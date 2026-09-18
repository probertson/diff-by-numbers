// Package shim is the stdio MCP front an agent launches. It ensures a shared dbn
// daemon is running, then serves a stdio MCP server that forwards every call to
// that daemon — so the review tools are present the moment a session starts,
// with nothing having to be running beforehand. The daemon it starts is detached
// and outlives the shim, so review state survives the agent window closing, and a
// second session's shim finds and shares the same daemon.
package shim

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/probertson/diff-by-numbers/internal/buildinfo"
)

// keepaliveInterval is how often the shim pings the daemon while the agent
// session is alive. It is comfortably shorter than the daemon's default idle
// grace, so a connected session never lets the daemon exit under it.
const keepaliveInterval = 20 * time.Second

// A daemon found listening can still be mid self-exit, closing its listener
// between the probe and the connect. These bound a short retry that re-spawns and
// reconnects rather than failing the whole session over that narrow race.
const (
	daemonConnectAttempts = 3
	daemonConnectBackoff  = 100 * time.Millisecond
)

// Config is what the shim needs to do its job.
type Config struct {
	// Port is where the daemon listens and the shim connects.
	Port int
	// Executable is the dbn binary to spawn as the daemon — normally the shim's
	// own path, so shim and daemon are always the same build.
	Executable string
	// LogPath, if set, is where a spawned daemon's stdout/stderr go. It must not be
	// the shim's stdout, which is the MCP channel.
	LogPath string
	// StartTimeout bounds how long Run waits for a just-spawned daemon to listen.
	StartTimeout time.Duration
}

// Run ensures a daemon, then serves the stdio proxy until the agent disconnects.
func Run(ctx context.Context, cfg Config) error {
	if cfg.StartTimeout <= 0 {
		cfg.StartTimeout = 5 * time.Second
	}

	daemonSession, err := connectDaemon(ctx, cfg)
	if err != nil {
		return err
	}
	defer daemonSession.Close()

	// Hold the daemon open for as long as this session lives. The daemon's own
	// idle clock is what lets an auto-started daemon exit once nothing needs it,
	// so a session that connects but does not post for a while must keep that
	// clock fresh — otherwise the daemon could exit before the first post.
	stopKeepalive := make(chan struct{})
	defer close(stopKeepalive)
	go keepalive(cfg.Port, stopKeepalive)

	// Mirror the daemon's tools onto a stdio server, each handler forwarding to the
	// daemon. The shim carries no knowledge of any individual tool, so it never
	// drifts from the daemon's contract as tools change.
	server := mcp.NewServer(&mcp.Implementation{Name: "dbn", Version: buildinfo.Version()}, nil)
	tools, err := daemonSession.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("could not list the daemon's tools: %w", err)
	}
	for _, tool := range tools.Tools {
		server.AddTool(tool, forward(daemonSession, tool.Name))
	}

	return server.Run(ctx, &mcp.StdioTransport{})
}

// forward relays one tool call to the daemon and returns its result verbatim.
func forward(session *mcp.ClientSession, name string) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return session.CallTool(ctx, &mcp.CallToolParams{
			Name:      name,
			Arguments: req.Params.Arguments,
		})
	}
}

func addr(port int) string { return fmt.Sprintf("127.0.0.1:%d", port) }

// connectDaemon ensures a daemon is up and returns a session to it, retrying a
// few times so a daemon caught mid self-exit (listener closing between probe and
// connect) causes a re-spawn rather than failing the whole shim — which would
// leave the agent's dbn tools unavailable for the session.
func connectDaemon(ctx context.Context, cfg Config) (*mcp.ClientSession, error) {
	endpoint := fmt.Sprintf("http://%s/mcp", addr(cfg.Port))
	client := mcp.NewClient(&mcp.Implementation{Name: "dbn-shim", Version: buildinfo.Version()}, nil)

	var lastErr error
	for attempt := 0; attempt < daemonConnectAttempts; attempt++ {
		if err := ensureDaemon(cfg); err != nil {
			lastErr = err
		} else if session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil); err == nil {
			return session, nil
		} else {
			lastErr = err
		}
		time.Sleep(daemonConnectBackoff)
	}
	return nil, fmt.Errorf("could not reach the dbn daemon on port %d: %w", cfg.Port, lastErr)
}

// keepalive pings the daemon on a timer until told to stop, refreshing the
// daemon's idle clock so it stays up while this session is connected.
func keepalive(port int, stop <-chan struct{}) {
	ticker := time.NewTicker(keepaliveInterval)
	defer ticker.Stop()
	httpClient := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://%s/ping", addr(port))
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if resp, err := httpClient.Get(url); err == nil {
				resp.Body.Close()
			}
		}
	}
}

// ensureDaemon guarantees a daemon is listening on the port: it connects to one
// already there, or spawns a detached daemon and waits for it to come up. The
// port bind is the mutex — if two shims race, one spawned daemon wins the bind
// and the other exits, and both shims connect to the winner.
func ensureDaemon(cfg Config) error {
	if listening(cfg.Port) {
		return nil
	}
	if err := spawnDaemon(cfg); err != nil {
		return err
	}
	return waitForDaemon(cfg.Port, cfg.StartTimeout)
}

func listening(port int) bool {
	conn, err := net.DialTimeout("tcp", addr(port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func waitForDaemon(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if listening(port) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("the dbn daemon did not start listening on port %d within %s", port, timeout)
}

// spawnDaemon starts a detached `dbn serve --self-exit`. Detached — its own
// process group, no shared stdio — so it survives this shim exiting (the agent
// session ending) and never writes onto the shim's stdout, which is the MCP
// channel. --self-exit is what makes it let go once no review needs it.
func spawnDaemon(cfg Config) error {
	cmd := exec.Command(cfg.Executable, "serve", "--self-exit", "--port", strconv.Itoa(cfg.Port))
	cmd.Stdin = nil

	if cfg.LogPath != "" {
		if logFile, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			cmd.Stdout = logFile
			cmd.Stderr = logFile
			defer logFile.Close()
		}
	}
	cmd.SysProcAttr = detachAttr()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start the dbn daemon: %w", err)
	}
	// Reap the child when it exits, rather than Release()-ing it, so a daemon that
	// loses the bind-or-connect race (binds, fails, exits at once) does not linger
	// as a zombie for the life of the shim. The winner is reaped only when it
	// eventually self-exits or the shim itself dies.
	go func() { _ = cmd.Wait() }()
	return nil
}

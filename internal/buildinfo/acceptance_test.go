package buildinfo_test

// An acceptance test for the build stamp: it builds the real binary the way the
// release pipeline does — the same -X path .goreleaser.yaml passes — and then
// asks every surface that reports a version what it thinks it is. The ldflags
// path is a string in a YAML file; nothing but a build can tell us it still
// points at the variable. Prior art: internal/installer, internal/shim.

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stampedVersion is deliberately nothing like the default dev stamp, so a
// surface that ignored the ldflags fails loudly rather than coincidentally.
const stampedVersion = "9.9.9"

// ldflagsPath must stay in step with .goreleaser.yaml's -X target.
const ldflagsPath = "github.com/probertson/diff-by-numbers/internal/buildinfo.version"

type statusPayload struct {
	Version      string `json:"version"`
	Executable   string `json:"executable"`
	ActiveReview bool   `json:"active_review"`
}

func TestAReleaseBuildReportsItsVersionFromEverySurface(t *testing.T) {
	bin := buildStamped(t)
	port := freePort(t)

	out, err := exec.Command(bin, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("dbn version: %v\n%s", err, out)
	}

	if got, want := string(out), "dbn "+stampedVersion+"\n"; got != want {
		t.Errorf("dbn version printed %q, want %q", got, want)
	}

	serve(t, bin, port)
	ctx := context.Background()

	status := getStatus(t, port)
	if status.Version != stampedVersion {
		t.Errorf("the status endpoint reports version %q, want %q", status.Version, stampedVersion)
	}
	if status.Executable != bin {
		t.Errorf("the status endpoint reports executable %q, want %q", status.Executable, bin)
	}
	if status.ActiveReview {
		t.Error("the status endpoint reports a review active on a daemon that has none")
	}

	direct := connect(t, ctx, &mcp.StreamableClientTransport{Endpoint: endpoint(port)})
	defer direct.Close()
	if got := direct.InitializeResult().ServerInfo.Version; got != stampedVersion {
		t.Errorf("the daemon's MCP serverInfo reports %q, want %q", got, stampedVersion)
	}

	shimCmd := exec.Command(bin, "mcp")
	shimCmd.Env = append(os.Environ(), "DBN_PORT="+strconv.Itoa(port))
	shimmed := connect(t, ctx, &mcp.CommandTransport{Command: shimCmd})
	defer shimmed.Close()
	if got := shimmed.InitializeResult().ServerInfo.Version; got != stampedVersion {
		t.Errorf("the shim's MCP serverInfo reports %q, want %q", got, stampedVersion)
	}
}

func TestTheShutdownEndpointStopsTheDaemon(t *testing.T) {
	bin := buildStamped(t)
	port := freePort(t)

	serve(t, bin, port)

	response, err := http.Post(endpointBase(port)+"/shutdown", "text/plain", nil)
	if err != nil {
		t.Fatalf("POST /shutdown: %v", err)
	}
	response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST /shutdown answered %s", response.Status)
	}
	if !gone(port, 5*time.Second) {
		t.Error("the daemon is still listening after /shutdown")
	}
}

// --- helpers ----------------------------------------------------------------

// buildStamped builds dbn with the release pipeline's ldflags, so the binary
// under test is stamped exactly as a published one is.
func buildStamped(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "dbn")
	cmd := exec.Command("go", "build", "-ldflags", "-X "+ldflagsPath+"="+stampedVersion, "-o", bin, "./cmd/dbn")
	cmd.Dir = filepath.Join("..", "..") // repo root, relative to internal/buildinfo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("could not build a stamped dbn: %v\n%s", err, out)
	}
	return bin
}

// serve starts a hand-run daemon on the port and waits for it to listen.
func serve(t *testing.T, bin string, port int) {
	t.Helper()
	cmd := exec.Command(bin, "serve", "--port", strconv.Itoa(port))
	if err := cmd.Start(); err != nil {
		t.Fatalf("could not start the daemon: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if listening(port) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the daemon did not start listening on port %d", port)
}

func getStatus(t *testing.T, port int) statusPayload {
	t.Helper()
	response, err := http.Get(endpointBase(port) + "/status")
	if err != nil {
		t.Fatalf("GET /status: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /status answered %s", response.Status)
	}
	var status statusPayload
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatalf("could not decode the status: %v", err)
	}
	return status
}

func connect(t *testing.T, ctx context.Context, transport mcp.Transport) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("could not connect over MCP: %v", err)
	}
	return session
}

func endpointBase(port int) string { return "http://127.0.0.1:" + strconv.Itoa(port) }
func endpoint(port int) string     { return endpointBase(port) + "/mcp" }

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not find a free port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func listening(port int) bool {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func gone(port int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !listening(port) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

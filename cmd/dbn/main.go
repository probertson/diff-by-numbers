// Command dbn is the review surface an agent drives.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/buildinfo"
	"github.com/probertson/diff-by-numbers/internal/daemon"
	"github.com/probertson/diff-by-numbers/internal/selfupdate"
	"github.com/probertson/diff-by-numbers/internal/shim"
	"github.com/probertson/diff-by-numbers/internal/skillcheck"
	"github.com/probertson/diff-by-numbers/internal/tui"
	"github.com/probertson/diff-by-numbers/internal/updatecheck"
	"github.com/probertson/diff-by-numbers/skills"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "dbn: %v\n", err)
		os.Exit(1)
	}
}

// defaultPort is the port dbn listens on and the TUI attaches to, overridable
// with DBN_PORT so several daemons can run side by side (tests, or two repos).
func defaultPort() int {
	if raw := os.Getenv("DBN_PORT"); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil {
			return p
		}
	}
	return daemon.DefaultPort
}

// daemonURL is where a daemon on this port answers. Loopback, always: the daemon
// listens nowhere else.
func daemonURL(port int) string { return fmt.Sprintf("http://127.0.0.1:%d", port) }

// parseTUIArgs reads the flags of the bare `dbn` form, which opens the review
// TUI. Help goes to usage and comes back as flag.ErrHelp.
func parseTUIArgs(args []string, usage io.Writer) (int, error) {
	flags := flag.NewFlagSet("dbn", flag.ContinueOnError)
	flags.SetOutput(usage)
	port := flags.Int("port", defaultPort(), "daemon port to attach to")
	// Written out rather than PrintDefaults, which would show the default as
	// whatever DBN_PORT resolved to and not say where it came from.
	flags.Usage = func() {
		fmt.Fprintf(usage, `usage: dbn [-port N]                 open the review TUI
       dbn <command> [flags]
commands: serve, mcp, dump, abandon, update, version
run `+"`dbn <command> -h`"+` for a command's flags

  -port int
    	daemon port to attach to (default %d, or $DBN_PORT)
`, daemon.DefaultPort)
	}
	if err := flags.Parse(args); err != nil {
		return 0, err
	}
	if flags.NArg() > 0 {
		return 0, fmt.Errorf("unexpected argument %q; run `dbn -h` for usage", flags.Arg(0))
	}
	return *port, nil
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 || (strings.HasPrefix(args[0], "-") && !isVersionFlag(args[0])) {
		port, err := parseTUIArgs(args, out)
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		if err != nil {
			return err
		}
		return tui.Run(port)
	}

	switch args[0] {
	case "version", "--version", "-v":
		// A build stamp so a Reviewer can tell which dbn a daemon is running,
		// which matters once several people share the review workflow.
		fmt.Fprintln(out, "dbn "+buildinfo.Version())
		reportUpdate(out)
		return nil
	case "serve":
		flags := flag.NewFlagSet("serve", flag.ContinueOnError)
		port := flags.Int("port", defaultPort(), "port to listen on")
		selfExit := flags.Bool("self-exit", false, "exit automatically once no review needs the daemon (used when auto-started)")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		var opts []daemon.Option
		if *selfExit {
			opts = append(opts, daemon.WithSelfExit())
		}
		return daemon.New(opts...).Serve(*port)

	case "mcp":
		// The stdio MCP shim an agent registers and launches: it starts the daemon
		// if none is running, then proxies to it. Registered with, e.g.,
		// `claude mcp add dbn -- dbn mcp`.
		flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
		port := flags.Int("port", defaultPort(), "daemon port to connect to or start")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("could not find the dbn executable to start the daemon: %w", err)
		}
		return shim.Run(context.Background(), shim.Config{
			Port:       *port,
			Executable: exe,
			LogPath:    filepath.Join(os.TempDir(), "dbn-daemon.log"),
		})

	case "update":
		flags := flag.NewFlagSet("update", flag.ContinueOnError)
		port := flags.Int("port", defaultPort(), "port a running daemon is listening on")
		force := flags.Bool("force", false, "restart the daemon even if a review is in progress, losing it")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		cfg, err := selfupdate.DefaultConfig(daemonURL(*port))
		if err != nil {
			return err
		}
		cfg.Force = *force
		return selfupdate.Update(context.Background(), cfg, out)

	case "skill-check":
		// Deliberately absent from the usage list: this is the second hop of
		// `dbn update`, which re-execs the freshly installed binary so the
		// comparison is made against the skill that binary shipped with, not the
		// one the outgoing binary embedded. Nothing stops a Reviewer running it,
		// but it is not a command they need to know.
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("could not find your home directory to check the dbn-review skill: %w", err)
		}
		skillcheck.Report(skillcheck.Config{Home: home, Embedded: skills.DbnReview}, out)
		return nil

	case "dump":
		flags := flag.NewFlagSet("dump", flag.ContinueOnError)
		port := flags.Int("port", defaultPort(), "port the daemon is listening on")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return dump(*port, out)

	case "abandon":
		flags := flag.NewFlagSet("abandon", flag.ContinueOnError)
		port := flags.Int("port", defaultPort(), "port the daemon is listening on")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return abandon(*port, out)

	default:
		return fmt.Errorf("unknown command %q; run `dbn -h` for usage", args[0])
	}
}

func isVersionFlag(arg string) bool { return arg == "--version" || arg == "-v" }

// reportUpdate adds what the update check found under the version line. Unlike
// the TUI, `dbn version` says when the check itself failed: someone asking a
// binary what it is deserves to know the answer is incomplete, and this is where
// an opted-out or offline machine finds out why it hears nothing elsewhere.
func reportUpdate(out io.Writer) {
	result, err := updatecheck.Check(context.Background(), updatecheck.DefaultConfig())
	switch {
	case err != nil:
		fmt.Fprintf(out, "(couldn't check for updates: %v)\n", err)
	case result.Available:
		fmt.Fprintln(out, selfupdate.Notice(result.Latest))
	}
}

func dump(port int, out io.Writer) error {
	response, err := http.Get(daemonURL(port) + "/dump")
	if err != nil {
		return fmt.Errorf("no dbn daemon on port %d — start one with `dbn serve`: %w", port, err)
	}
	defer response.Body.Close()

	if err := expectOK(response, port); err != nil {
		return err
	}
	if _, err := io.Copy(out, response.Body); err != nil {
		return fmt.Errorf("could not read the daemon's response: %w", err)
	}
	return nil
}

func abandon(port int, out io.Writer) error {
	response, err := http.Post(daemonURL(port)+"/abandon", "text/plain", nil)
	if err != nil {
		return fmt.Errorf("no dbn daemon on port %d — start one with `dbn serve`: %w", port, err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusConflict {
		body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
		return fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}
	if err := expectOK(response, port); err != nil {
		return err
	}
	_, err = io.Copy(out, response.Body)
	return err
}

const maxErrorBody = 4 << 10

// expectOK guards against something other than dbn answering on the port. A
// transport error is caught by the caller; a wrong-but-willing server is not,
// and would otherwise have its response printed as though it were a Walkthrough.
func expectOK(response *http.Response, port int) error {
	if response.StatusCode == http.StatusOK {
		return nil
	}
	return fmt.Errorf("the server on port %d answered %s — is that dbn?", port, response.Status)
}

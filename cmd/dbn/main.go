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
	"strings"

	"github.com/probertson/diff-by-numbers/internal/buildinfo"
	"github.com/probertson/diff-by-numbers/internal/config"
	"github.com/probertson/diff-by-numbers/internal/daemon"
	"github.com/probertson/diff-by-numbers/internal/selfupdate"
	"github.com/probertson/diff-by-numbers/internal/shim"
	"github.com/probertson/diff-by-numbers/internal/skillcheck"
	"github.com/probertson/diff-by-numbers/internal/tui"
	"github.com/probertson/diff-by-numbers/internal/updatecheck"
	"github.com/probertson/diff-by-numbers/skills"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "dbn: %v\n", err)
		os.Exit(1)
	}
}

// portSource gives the port a command uses, once its flags are parsed and dbn's
// settings are read.
type portSource func(config.Settings) (config.Setting[int], error)

// portFlag adds -port to a command's flags. It has no default of its own — the
// configured port is the default, and the settings are read only once the flags
// are parsed, so `-h` works even with a broken config file. The function it
// returns gives the port the command uses: the flag's when it was given, which
// outranks every other source, and the configured one otherwise.
func portFlag(flags *flag.FlagSet, usage string) portSource {
	port := flags.Int("port", 0, usage+" (default: dbn config shows it)")
	return func(settings config.Settings) (config.Setting[int], error) {
		given := false
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "port" {
				given = true
			}
		})
		if !given {
			return settings.Port, nil
		}
		if err := config.CheckPort(*port); err != nil {
			return config.Setting[int]{}, fmt.Errorf("-port %d: %w", *port, err)
		}
		return config.Setting[int]{Value: *port, Source: config.Flag}, nil
	}
}

// loadSettings reads dbn's settings once a command's flags are parsed, warns on
// errOut about config file keys dbn ignored, and resolves the port. An invalid
// setting anywhere stops the command: a typo must never quietly send one dbn
// process to a different port from another.
func loadSettings(port portSource, errOut io.Writer) (config.Settings, int, error) {
	settings, err := config.Load(os.LookupEnv)
	if err != nil {
		return config.Settings{}, 0, err
	}
	warn(settings, errOut)
	resolved, err := port(settings)
	if err != nil {
		return config.Settings{}, 0, err
	}
	return settings, resolved.Value, nil
}

// warn prints the config file warning, if there is one.
func warn(settings config.Settings, errOut io.Writer) {
	if warning := settings.Warning(); warning != "" {
		fmt.Fprintf(errOut, "dbn: warning: %s\n", warning)
	}
}

// daemonURL is where a daemon on this port answers. Loopback, always: the daemon
// listens nowhere else.
func daemonURL(port int) string { return fmt.Sprintf("http://127.0.0.1:%d", port) }

// parseTUIArgs reads the flags of the bare `dbn` form, which opens the review
// TUI. Help goes to usage and comes back as flag.ErrHelp.
func parseTUIArgs(args []string, usage io.Writer) (portSource, error) {
	flags := flag.NewFlagSet("dbn", flag.ContinueOnError)
	flags.SetOutput(usage)
	port := portFlag(flags, "daemon port to attach to")
	// Written out rather than PrintDefaults, which cannot say where the default
	// comes from.
	flags.Usage = func() {
		fmt.Fprintf(usage, `usage: dbn [-port N]                 open the review TUI
       dbn <command> [flags]
commands: serve, mcp, wait, dump, update, version, config
run `+"`dbn <command> -h`"+` for a command's flags

  -port int
    	daemon port to attach to (default %d, unless set in the config file or $DBN_PORT)
`, daemon.DefaultPort)
	}
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	if flags.NArg() > 0 {
		return nil, fmt.Errorf("unexpected argument %q; run `dbn -h` for usage", flags.Arg(0))
	}
	return port, nil
}

func run(args []string, out, errOut io.Writer) error {
	if len(args) == 0 || (strings.HasPrefix(args[0], "-") && !isVersionFlag(args[0])) {
		portFor, err := parseTUIArgs(args, out)
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		if err != nil {
			return err
		}
		// Not loadSettings: the warning goes in the TUI's notice slot, since anything
		// printed now is hidden the moment the TUI takes the screen.
		settings, err := config.Load(os.LookupEnv)
		if err != nil {
			return err
		}
		resolved, err := portFor(settings)
		if err != nil {
			return err
		}
		return tui.Run(tui.Options{
			Port:          resolved.Value,
			UpdateCheck:   settings.UpdateCheck.Value,
			ConfigWarning: settings.Warning(),
		})
	}

	switch args[0] {
	case "version", "--version", "-v":
		// A build stamp so a Reviewer can tell which dbn a daemon is running,
		// which matters once several people share the review workflow.
		settings, err := config.Load(os.LookupEnv)
		if err != nil {
			return err
		}
		warn(settings, errOut)
		fmt.Fprintln(out, "dbn "+buildinfo.Version())
		reportUpdate(out, settings.UpdateCheck.Value)
		return nil
	case "serve":
		flags := flag.NewFlagSet("serve", flag.ContinueOnError)
		portFor := portFlag(flags, "port to listen on")
		selfExit := flags.Bool("self-exit", false, "exit automatically once no review needs the daemon (used when auto-started)")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		_, port, err := loadSettings(portFor, errOut)
		if err != nil {
			return err
		}
		var opts []daemon.Option
		if *selfExit {
			opts = append(opts, daemon.WithSelfExit())
		}
		return daemon.New(opts...).Serve(port)

	case "mcp":
		// The stdio MCP shim an agent registers and launches: it starts the daemon
		// if none is running, then proxies to it. The Claude Code plugin registers
		// it; other agents register it by hand, e.g. `claude mcp add dbn -- dbn mcp`.
		flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
		portFor := portFlag(flags, "daemon port to connect to or start")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("could not find the dbn executable to start the daemon: %w", err)
		}
		cfg := shim.Config{
			Executable: exe,
			LogPath:    filepath.Join(os.TempDir(), "dbn-daemon.log"),
		}
		// No warning here: the shim's stderr goes where nobody reads it.
		resolve := func() (int, error) {
			settings, err := config.Load(os.LookupEnv)
			if err != nil {
				return 0, err
			}
			port, err := portFor(settings)
			return port.Value, err
		}
		port, err := resolve()
		if err != nil {
			// The agent launched this, so an error on stderr would reach no one.
			// Serve the tools anyway and let each call carry the error to the
			// agent, which tells the Reviewer.
			return shim.RunUntilConfigured(context.Background(), cfg, resolve)
		}
		cfg.Port = port
		return shim.Run(context.Background(), cfg)

	case "update":
		flags := flag.NewFlagSet("update", flag.ContinueOnError)
		portFor := portFlag(flags, "port a running daemon is listening on")
		force := flags.Bool("force", false, "restart the daemon even if a review is in progress, losing it")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		_, port, err := loadSettings(portFor, errOut)
		if err != nil {
			return err
		}
		cfg, err := selfupdate.DefaultConfig(daemonURL(port))
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

	case "wait":
		return runWait(args[1:], out, errOut)

	case "dump":
		flags := flag.NewFlagSet("dump", flag.ContinueOnError)
		portFor := portFlag(flags, "port the daemon is listening on")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		_, port, err := loadSettings(portFor, errOut)
		if err != nil {
			return err
		}
		return dump(port, out)

	case "config":
		return runConfig(args[1:], out, errOut)

	default:
		return fmt.Errorf("unknown command %q; run `dbn -h` for usage", args[0])
	}
}

func isVersionFlag(arg string) bool { return arg == "--version" || arg == "-v" }

// reportUpdate adds what the update check found under the version line. Unlike
// the TUI, `dbn version` says when the check itself failed: someone asking a
// binary what it is deserves to know the answer is incomplete, and this is where
// an opted-out or offline machine finds out why it hears nothing elsewhere.
func reportUpdate(out io.Writer, enabled bool) {
	result, err := updatecheck.Check(context.Background(), updatecheck.DefaultConfig(enabled))
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

// expectOK guards against something other than dbn answering on the port. A
// transport error is caught by the caller; a wrong-but-willing server is not,
// and would otherwise have its response printed as though it were a Round.
func expectOK(response *http.Response, port int) error {
	if response.StatusCode == http.StatusOK {
		return nil
	}
	return fmt.Errorf("the server on port %d answered %s — is that dbn?", port, response.Status)
}

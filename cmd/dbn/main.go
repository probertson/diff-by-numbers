// Command dbn is the review surface an agent drives.
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "dbn: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: dbn <serve|dump|abandon> [flags]")
	}

	switch args[0] {
	case "serve":
		flags := flag.NewFlagSet("serve", flag.ContinueOnError)
		port := flags.Int("port", daemon.DefaultPort, "port to listen on")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return daemon.New().Serve(*port)

	case "dump":
		flags := flag.NewFlagSet("dump", flag.ContinueOnError)
		port := flags.Int("port", daemon.DefaultPort, "port the daemon is listening on")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return dump(*port, out)

	case "abandon":
		flags := flag.NewFlagSet("abandon", flag.ContinueOnError)
		port := flags.Int("port", daemon.DefaultPort, "port the daemon is listening on")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return abandon(*port, out)

	default:
		return fmt.Errorf("unknown command %q; usage: dbn <serve|dump|abandon> [flags]", args[0])
	}
}

func dump(port int, out io.Writer) error {
	response, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/dump", port))
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
	response, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/abandon", port), "text/plain", nil)
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

package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

func TestParseTUIArgsReadsThePortFlag(t *testing.T) {
	t.Setenv("DBN_PORT", "")

	port, err := parseTUIArgs([]string{"-port", "7374"}, io.Discard)

	if err != nil {
		t.Fatalf("parseTUIArgs: %v", err)
	}
	if port != 7374 {
		t.Errorf("port = %d, want 7374", port)
	}
}

func TestParseTUIArgsDefaultsToTheDefaultPort(t *testing.T) {
	t.Setenv("DBN_PORT", "")

	port, err := parseTUIArgs(nil, io.Discard)

	if err != nil {
		t.Fatalf("parseTUIArgs: %v", err)
	}
	if port != daemon.DefaultPort {
		t.Errorf("port = %d, want %d", port, daemon.DefaultPort)
	}
}

func TestParseTUIArgsRespectsDBNPortWhenNoFlagIsGiven(t *testing.T) {
	t.Setenv("DBN_PORT", "7380")

	port, err := parseTUIArgs(nil, io.Discard)

	if err != nil {
		t.Fatalf("parseTUIArgs: %v", err)
	}
	if port != 7380 {
		t.Errorf("port = %d, want 7380", port)
	}
}

func TestParseTUIArgsPortFlagOverridesDBNPort(t *testing.T) {
	t.Setenv("DBN_PORT", "7380")

	port, err := parseTUIArgs([]string{"-port", "7374"}, io.Discard)

	if err != nil {
		t.Fatalf("parseTUIArgs: %v", err)
	}
	if port != 7374 {
		t.Errorf("port = %d, want 7374", port)
	}
}

func TestParseTUIArgsRejectsATrailingPositionalArgument(t *testing.T) {
	t.Setenv("DBN_PORT", "")

	_, err := parseTUIArgs([]string{"-port", "7374", "foo"}, io.Discard)

	if err == nil {
		t.Fatal("parseTUIArgs accepted a trailing positional argument")
	}
	if !strings.Contains(err.Error(), "foo") {
		t.Errorf("error %q does not name the unexpected argument", err)
	}
}

func TestRunHelpPrintsUsageAndReturnsCleanly(t *testing.T) {
	var out bytes.Buffer

	err := run([]string{"-h"}, &out)

	if err != nil {
		t.Fatalf("run -h returned %v, want nil", err)
	}
	usage := out.String()
	for _, want := range []string{
		"usage: dbn [-port N]",
		"commands: serve, mcp, dump, abandon, update, version",
		"-port int",
	} {
		if !strings.Contains(usage, want) {
			t.Errorf("usage is missing %q:\n%s", want, usage)
		}
	}
}

func TestRunUnknownCommandIsAOneLineError(t *testing.T) {
	err := run([]string{"foo"}, io.Discard)

	if err == nil {
		t.Fatal("run foo succeeded, want an unknown-command error")
	}
	msg := err.Error()
	if strings.Contains(msg, "\n") {
		t.Errorf("error spans several lines: %q", msg)
	}
	if !strings.Contains(msg, `unknown command "foo"`) || !strings.HasSuffix(msg, "run `dbn -h` for usage") {
		t.Errorf("error = %q, want it to name the command and end with the usage hint", msg)
	}
}

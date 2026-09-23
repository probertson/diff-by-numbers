package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
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
		"commands: serve, mcp, wait, dump, update, version",
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

func TestSkillCheckReportsOnAStaleUserSkill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".claude", "skills", "dbn-review", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("nothing like the shipped skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer

	if err := run([]string{"skill-check"}, &out); err != nil {
		t.Fatalf("dbn skill-check: %v", err)
	}

	if !strings.Contains(out.String(), "is out of date: run npx skills add") {
		t.Errorf("skill-check did not report the stale skill:\n%s", out.String())
	}
}

func TestSkillCheckIsNotAdvertisedInUsage(t *testing.T) {
	var usage bytes.Buffer

	_, _ = parseTUIArgs([]string{"-h"}, &usage)

	if strings.Contains(usage.String(), "skill-check") {
		t.Errorf("skill-check is an internal hop for `dbn update`, not a command to advertise:\n%s", usage.String())
	}
}

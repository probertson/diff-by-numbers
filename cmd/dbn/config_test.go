package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain points every test in this package — and every dbn process a test
// starts — at an empty config directory, so the developer's own config file
// can't change what a test sees.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "dbn-config")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// withConfig gives this test its own config file holding contents, and returns
// its path.
func withConfig(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "dbn", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// fields is each line of dbn config's output split on whitespace, so a test
// can look for a setting's row without caring how the columns are padded.
func fields(output string) [][]string {
	var rows [][]string
	for _, line := range strings.Split(output, "\n") {
		rows = append(rows, strings.Fields(line))
	}
	return rows
}

func hasRow(output string, want ...string) bool {
	for _, row := range fields(output) {
		if strings.Join(row, " ") == strings.Join(want, " ") {
			return true
		}
	}
	return false
}

func TestConfigListsEachSettingWithItsSource(t *testing.T) {
	path := withConfig(t, "port = 7400\n")
	t.Setenv("DBN_PORT", "")
	t.Setenv("DBN_NO_UPDATE_CHECK", "1")
	var out bytes.Buffer

	err := run([]string{"config"}, &out, &bytes.Buffer{})

	if err != nil {
		t.Fatalf("dbn config: %v", err)
	}
	if !strings.Contains(out.String(), path) {
		t.Errorf("dbn config does not print the file's path %s:\n%s", path, out.String())
	}
	if !hasRow(out.String(), "port", "7400", "config", "file") {
		t.Errorf("dbn config does not show port 7400 from the config file:\n%s", out.String())
	}
	if !hasRow(out.String(), "update-check", "false", "$DBN_NO_UPDATE_CHECK") {
		t.Errorf("dbn config does not show the update check off from DBN_NO_UPDATE_CHECK:\n%s", out.String())
	}
}

func TestConfigSaysWhenThereIsNoFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("DBN_PORT", "")
	t.Setenv("DBN_NO_UPDATE_CHECK", "")
	var out bytes.Buffer

	err := run([]string{"config"}, &out, &bytes.Buffer{})

	if err != nil {
		t.Fatalf("dbn config: %v", err)
	}
	if !strings.Contains(out.String(), "(no file there; using defaults)") {
		t.Errorf("dbn config does not say the file is missing:\n%s", out.String())
	}
	if !hasRow(out.String(), "port", "7373", "default") || !hasRow(out.String(), "update-check", "true", "default") {
		t.Errorf("dbn config does not show the defaults:\n%s", out.String())
	}
}

func TestAPortFlagOutranksTheEnvironmentAndTheFile(t *testing.T) {
	withConfig(t, "port = 7400\n")
	t.Setenv("DBN_PORT", "7402")
	var out bytes.Buffer

	err := run([]string{"config", "-port", "7500"}, &out, &bytes.Buffer{})

	if err != nil {
		t.Fatalf("dbn config: %v", err)
	}
	if !hasRow(out.String(), "port", "7500", "-port", "flag") {
		t.Errorf("dbn config does not show port 7500 from the flag:\n%s", out.String())
	}
}

func TestAPortFlagOutOfRangeIsAnError(t *testing.T) {
	err := run([]string{"dump", "-port", "70000"}, &bytes.Buffer{}, &bytes.Buffer{})

	if err == nil || !strings.Contains(err.Error(), "-port") || !strings.Contains(err.Error(), "between 1 and 65535") {
		t.Errorf("dbn dump -port 70000 returned %v, want an error naming the flag and the range", err)
	}
}

func TestAnInvalidConfigFileStopsACommand(t *testing.T) {
	path := withConfig(t, "port = 70000\n")

	err := run([]string{"dump"}, &bytes.Buffer{}, &bytes.Buffer{})

	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "line 1") {
		t.Errorf("dbn dump returned %v, want an error naming %s and the line", err, path)
	}
}

func TestConfigReportsAnInvalidFileAndFails(t *testing.T) {
	path := withConfig(t, "port = 70000\n")
	var out bytes.Buffer

	err := run([]string{"config"}, &out, &bytes.Buffer{})

	if err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Errorf("dbn config returned %v, want the file's error", err)
	}
	if !strings.Contains(out.String(), path) {
		t.Errorf("dbn config does not print the path of the file it is reporting on:\n%s", out.String())
	}
}

func TestAnUnknownKeyWarnsOnStderrAndTheCommandCarriesOn(t *testing.T) {
	withConfig(t, "colour-preset = \"strong\"\n")
	var out, errOut bytes.Buffer

	err := run([]string{"version"}, &out, &errOut)

	if err != nil {
		t.Fatalf("dbn version: %v", err)
	}
	if !strings.HasPrefix(out.String(), "dbn ") {
		t.Errorf("dbn version did not print its version:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), `unknown key "colour-preset"`) {
		t.Errorf("stderr does not warn about the unknown key:\n%s", errOut.String())
	}
}

func TestConfigWarnsAboutAnUnknownKeyAsOtherCommandsDo(t *testing.T) {
	withConfig(t, "colour-preset = \"strong\"\n")
	var errOut bytes.Buffer

	err := run([]string{"config"}, &bytes.Buffer{}, &errOut)

	if err != nil {
		t.Fatalf("dbn config: %v", err)
	}
	if !strings.Contains(errOut.String(), `dbn: warning: unknown key "colour-preset"`) {
		t.Errorf("dbn config does not warn about the unknown key on stderr:\n%s", errOut.String())
	}
}

func TestConfigListsTheBackgroundAndTheDiffColours(t *testing.T) {
	withConfig(t, "background = \"light\"\n[diff]\npreset = \"strong\"\n[diff.dark]\nremoved-change = \"#8a3535\"\n")
	var out bytes.Buffer

	err := run([]string{"config"}, &out, &bytes.Buffer{})

	if err != nil {
		t.Fatalf("dbn config: %v", err)
	}
	for _, row := range [][]string{
		{"background", "light", "config", "file"},
		{"diff.preset", "strong", "config", "file"},
		{"diff.dark.added-line", "#1d4a1d", "strong", "preset"},
		{"diff.dark.removed-change", "#8a3535", "config", "file"},
		{"diff.light.removed-change", "#e88a8a", "strong", "preset"},
	} {
		if !hasRow(out.String(), row...) {
			t.Errorf("dbn config has no row %v:\n%s", row, out.String())
		}
	}
}

func TestConfigShowsTheDefaultColours(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var out bytes.Buffer

	err := run([]string{"config"}, &out, &bytes.Buffer{})

	if err != nil {
		t.Fatalf("dbn config: %v", err)
	}
	if !hasRow(out.String(), "background", "auto", "default") || !hasRow(out.String(), "diff.preset", "medium", "default") ||
		!hasRow(out.String(), "diff.light.added-line", "#e3f5e3", "medium", "preset") {
		t.Errorf("dbn config does not show the default background and colours:\n%s", out.String())
	}
}

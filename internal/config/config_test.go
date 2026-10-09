package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// environment is a stand-in for the process environment, so a test controls
// exactly which variables are set — including HOME, which is where the file is
// looked for.
type environment map[string]string

func (e environment) lookup(name string) (string, bool) {
	value, ok := e[name]
	return value, ok
}

// homeWith returns an environment whose HOME holds the given config file at
// ~/.config/dbn/config.toml.
func homeWith(t *testing.T, contents string) environment {
	t.Helper()
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".config", "dbn", "config.toml"), contents)
	return environment{"HOME": home}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWithNoFileEverySettingIsItsDefault(t *testing.T) {
	env := environment{"HOME": t.TempDir()}

	settings, err := Load(env.lookup)

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Port != (Setting[int]{Value: daemon.DefaultPort, Source: Default}) {
		t.Errorf("Port = %+v, want the default port from the default", settings.Port)
	}
	if settings.UpdateCheck != (Setting[bool]{Value: true, Source: Default}) {
		t.Errorf("UpdateCheck = %+v, want on from the default", settings.UpdateCheck)
	}
}

func TestTheFileSetsThePortAndTheUpdateCheck(t *testing.T) {
	env := homeWith(t, "port = 7400\nupdate-check = false\n")

	settings, err := Load(env.lookup)

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Port != (Setting[int]{Value: 7400, Source: File}) {
		t.Errorf("Port = %+v, want 7400 from the file", settings.Port)
	}
	if settings.UpdateCheck != (Setting[bool]{Value: false, Source: File}) {
		t.Errorf("UpdateCheck = %+v, want off from the file", settings.UpdateCheck)
	}
}

func TestXDGConfigHomeIsWhereTheFileIsLookedFor(t *testing.T) {
	xdg := t.TempDir()
	writeFile(t, filepath.Join(xdg, "dbn", "config.toml"), "port = 7401\n")
	env := homeWith(t, "port = 7400\n")
	env["XDG_CONFIG_HOME"] = xdg

	settings, err := Load(env.lookup)

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Path != filepath.Join(xdg, "dbn", "config.toml") {
		t.Errorf("Path = %q, want it under XDG_CONFIG_HOME", settings.Path)
	}
	if settings.Port.Value != 7401 {
		t.Errorf("Port = %d, want 7401 from the file under XDG_CONFIG_HOME", settings.Port.Value)
	}
}

func TestARelativeXDGConfigHomeIsIgnored(t *testing.T) {
	env := homeWith(t, "port = 7400\n")
	env["XDG_CONFIG_HOME"] = "relative/config"

	settings, err := Load(env.lookup)

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Port.Value != 7400 {
		t.Errorf("Port = %d, want 7400 from ~/.config, since the XDG spec says a relative path is ignored", settings.Port.Value)
	}
}

func TestEnvironmentVariablesOutrankTheFile(t *testing.T) {
	env := homeWith(t, "port = 7400\nupdate-check = true\n")
	env["DBN_PORT"] = "7402"
	env["DBN_NO_UPDATE_CHECK"] = "1"

	settings, err := Load(env.lookup)

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Port != (Setting[int]{Value: 7402, Source: Env}) {
		t.Errorf("Port = %+v, want 7402 from DBN_PORT", settings.Port)
	}
	if settings.UpdateCheck != (Setting[bool]{Value: false, Source: Env}) {
		t.Errorf("UpdateCheck = %+v, want off from DBN_NO_UPDATE_CHECK", settings.UpdateCheck)
	}
}

func TestAnEmptyEnvironmentVariableCountsAsUnset(t *testing.T) {
	env := homeWith(t, "port = 7400\nupdate-check = false\n")
	env["DBN_PORT"] = ""
	env["DBN_NO_UPDATE_CHECK"] = ""

	settings, err := Load(env.lookup)

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Port.Source != File || settings.UpdateCheck.Source != File {
		t.Errorf("Port from %s, UpdateCheck from %s; want both from the file", settings.Port.Source, settings.UpdateCheck.Source)
	}
}

func TestDBNNoUpdateCheckCanOnlyTurnTheCheckOff(t *testing.T) {
	// "0", "false" and "no" have always meant "don't opt out" (ADR-0012). A
	// variable named "no update check" must not turn the check back on, so they
	// count as unset and the file decides.
	for _, value := range []string{"0", "false", "no"} {
		env := homeWith(t, "update-check = false\n")
		env["DBN_NO_UPDATE_CHECK"] = value

		settings, err := Load(env.lookup)

		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if settings.UpdateCheck != (Setting[bool]{Value: false, Source: File}) {
			t.Errorf("DBN_NO_UPDATE_CHECK=%s: UpdateCheck = %+v, want off from the file", value, settings.UpdateCheck)
		}
	}
}

func TestAnInvalidFileIsAnErrorNamingTheFileTheLineAndTheProblem(t *testing.T) {
	cases := []struct {
		name     string
		contents string
		line     string
		problem  string
	}{
		{"not TOML", "port = 7400\nupdate-check = \n", "line 2", "expected value"},
		{"a port that is not a number", "update-check = true\nport = \"seven\"\n", "line 2", "port"},
		{"a port out of range", "port = 70000\n", "line 1", "between 1 and 65535"},
		{"a port of zero", "\n\nport = 0\n", "line 3", "between 1 and 65535"},
		{"an update check that is not a boolean", "update-check = \"off\"\n", "line 1", "update-check"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := homeWith(t, c.contents)

			_, err := Load(env.lookup)

			if err == nil {
				t.Fatal("Load accepted an invalid file")
			}
			for _, want := range []string{filepath.Join(env["HOME"], ".config", "dbn", "config.toml"), c.line, c.problem} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

func TestAnInvalidDBNPortIsAnErrorNamingTheVariable(t *testing.T) {
	for _, value := range []string{"74o4", "70000", "-1"} {
		env := environment{"HOME": t.TempDir(), "DBN_PORT": value}

		_, err := Load(env.lookup)

		if err == nil {
			t.Fatalf("DBN_PORT=%s: Load accepted it", value)
		}
		if !strings.Contains(err.Error(), "DBN_PORT") || !strings.Contains(err.Error(), value) {
			t.Errorf("DBN_PORT=%s: error %q does not name the variable and its value", value, err)
		}
	}
}

func TestAnInvalidSettingFailsEvenWhenSomethingOutranksIt(t *testing.T) {
	env := homeWith(t, "port = 70000\n")
	env["DBN_PORT"] = "7402"

	_, err := Load(env.lookup)

	if err == nil {
		t.Error("Load accepted an invalid file because DBN_PORT outranked the bad value")
	}
}

func TestAnUnknownKeyIsAWarningNotAnError(t *testing.T) {
	env := homeWith(t, "port = 7400\ncolour-preset = \"strong\"\n")

	settings, err := Load(env.lookup)

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Port.Value != 7400 {
		t.Errorf("Port = %d, want the known key still read", settings.Port.Value)
	}
	want := `unknown key "colour-preset" in ` + settings.Path
	if settings.Warning() != want {
		t.Errorf("Warning() = %q, want %q", settings.Warning(), want)
	}
}

func TestSeveralUnknownKeysShareOneWarning(t *testing.T) {
	env := homeWith(t, "colour = 1\n[diff]\npreset = \"strong\"\n")

	settings, err := Load(env.lookup)

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// A whole unknown table is named once, not once per key inside it.
	want := `unknown keys "colour", "diff" in ` + settings.Path
	if settings.Warning() != want {
		t.Errorf("Warning() = %q, want %q", settings.Warning(), want)
	}
}

func TestAFileOfKnownKeysHasNoWarning(t *testing.T) {
	env := homeWith(t, "port = 7400\n")

	settings, err := Load(env.lookup)

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Warning() != "" {
		t.Errorf("Warning() = %q, want none", settings.Warning())
	}
}

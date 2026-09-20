package skillcheck_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/skillcheck"
)

// The skill this release shipped. Every case compares an on-disk copy against it.
const shipped = "# dbn-review\n\nRequires dbn v0.2.2 or later.\n"

const stale = "# dbn-review\n\nan older copy\n"

// generic is the line every run ends with, whatever it found.
const generic = "Installed the dbn-review skill for another agent or in a project?"

func TestAnEmptyHomeGetsOnlyTheGenericReminder(t *testing.T) {
	home := t.TempDir()

	out := report(t, home)

	if !strings.Contains(out, generic) {
		t.Errorf("output does not end with the generic reminder:\n%s", out)
	}
	if strings.Contains(out, "out of date") {
		t.Errorf("nothing is installed, so nothing is out of date:\n%s", out)
	}
}

func TestAStaleUserSkillSaysHowToUpdateIt(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, filepath.Join(home, ".claude", "skills"), stale)

	out := report(t, home)

	// Asserted as one string: the generic reminder also names the npx command,
	// so a loose check on the command alone would pass without the finding.
	if !strings.Contains(out, "is out of date: run npx skills add probertson/diff-by-numbers/skills/dbn-review") {
		t.Errorf("a stale user skill should say it is out of date and name the npx command:\n%s", out)
	}
}

func TestACurrentUserSkillIsNotMentioned(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, filepath.Join(home, ".claude", "skills"), shipped)

	out := report(t, home)

	if strings.Contains(out, "out of date") {
		t.Errorf("a current skill should print nothing about itself:\n%s", out)
	}
}

func TestAStalePluginSaysHowToUpdateIt(t *testing.T) {
	home := t.TempDir()
	install := t.TempDir()
	writeSkill(t, filepath.Join(install, "skills"), stale)
	installPlugin(t, home, install)

	out := report(t, home)

	if !strings.Contains(out, "is out of date: run /plugin update dbn@diff-by-numbers") {
		t.Errorf("a stale plugin should name the /plugin update command:\n%s", out)
	}
	if !strings.Contains(out, "Claude Code plugin") {
		t.Errorf("a stale plugin should say which install it means:\n%s", out)
	}
}

func TestACurrentPluginIsNotMentioned(t *testing.T) {
	home := t.TempDir()
	install := t.TempDir()
	writeSkill(t, filepath.Join(install, "skills"), shipped)
	installPlugin(t, home, install)

	out := report(t, home)

	if strings.Contains(out, "out of date") {
		t.Errorf("a current plugin should print nothing about itself:\n%s", out)
	}
}

func TestAMalformedInstalledPluginsFileIsSkippedSilently(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "plugins", "installed_plugins.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := report(t, home)

	if !strings.Contains(out, generic) {
		t.Errorf("a malformed file should not stop the generic reminder:\n%s", out)
	}
	if strings.Contains(out, "out of date") {
		t.Errorf("a malformed file should be skipped, not guessed at:\n%s", out)
	}
}

func TestAnUnexpectedShapeIsSkippedSilently(t *testing.T) {
	home := t.TempDir()
	// Valid JSON, but "plugins" is an array where the real file has an object.
	writeJSON(t, filepath.Join(home, ".claude", "plugins", "installed_plugins.json"),
		map[string]any{"version": 1, "plugins": []any{"dbn@diff-by-numbers"}})

	out := report(t, home)

	if !strings.Contains(out, generic) {
		t.Errorf("an unexpected shape should not stop the generic reminder:\n%s", out)
	}
	if strings.Contains(out, "out of date") {
		t.Errorf("an unexpected shape should be skipped:\n%s", out)
	}
}

func TestAStalePluginAndAStaleUserSkillAreBothReported(t *testing.T) {
	home := t.TempDir()
	install := t.TempDir()
	writeSkill(t, filepath.Join(install, "skills"), stale)
	installPlugin(t, home, install)
	writeSkill(t, filepath.Join(home, ".claude", "skills"), stale)

	out := report(t, home)

	if !strings.Contains(out, "/plugin update dbn@diff-by-numbers") {
		t.Errorf("the stale plugin should be reported:\n%s", out)
	}
	if !strings.Contains(out, "is out of date: run npx skills add") {
		t.Errorf("the stale user skill should be reported:\n%s", out)
	}
	if !strings.Contains(out, generic) {
		t.Errorf("the generic reminder should still close the report:\n%s", out)
	}
}

// report runs the check over a fake home and returns everything it printed.
func report(t *testing.T, home string) string {
	t.Helper()
	var buf strings.Builder
	skillcheck.Report(skillcheck.Config{Home: home, Embedded: shipped}, &buf)
	return buf.String()
}

// writeSkill puts a dbn-review SKILL.md at the given directory's conventional path.
func writeSkill(t *testing.T, dir, content string) {
	t.Helper()
	path := filepath.Join(dir, "dbn-review", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// installPlugin writes an installed_plugins.json naming installPath for dbn.
func installPlugin(t *testing.T, home, installPath string) {
	t.Helper()
	doc := map[string]any{
		"version": 1,
		"plugins": map[string]any{
			"dbn@diff-by-numbers": []any{
				map[string]any{"scope": "user", "installPath": installPath, "version": "0.1.0"},
			},
		},
	}
	writeJSON(t, filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), doc)
}

func writeJSON(t *testing.T, path string, doc any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

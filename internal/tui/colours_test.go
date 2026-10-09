package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/probertson/diff-by-numbers/internal/config"
)

// configured applies colour settings exactly as Run does, restoring the
// package's tints when the test ends. Call it after inColour, whose cleanup
// then restores the background detection this may override.
func configured(t *testing.T, background config.TerminalBackground, diff config.DiffColours) {
	t.Helper()
	add, addEmph, del, delEmph := addTint, addEmphasis, delTint, delEmphasis
	t.Cleanup(func() { addTint, addEmphasis, delTint, delEmphasis = add, addEmph, del, delEmph })
	useColours(background, diff)
}

// settingsFrom loads diff settings from a config file holding contents, so a
// test states its colours the way a Reviewer would.
func settingsFrom(t *testing.T, contents string) config.Settings {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, ".config", "dbn", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	settings, err := config.Load(func(name string) (string, bool) {
		if name == "HOME" {
			return home, true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return settings
}

func TestTheDefaultColoursAreTheOnesDbnAlwaysDrew(t *testing.T) {
	inColour(t)
	settings := settingsFrom(t, "")
	configured(t, settings.TerminalBackground.Value, settings.Diff)

	added := drawn(t, 1, false, nil)
	removed := drawn(t, 0, false, nil)

	if !sgrBefore(added, "retry", addTintSGR) || !sgrBefore(added, "5\x1b", addEmphSGR) || !sgrBefore(removed, "retry", delTintSGR) {
		t.Errorf("expected the medium preset with no colour settings: added %q, removed %q", added, removed)
	}
}

func TestAPresetAndAnOverrideChangeTheDrawnTints(t *testing.T) {
	inColour(t)
	settings := settingsFrom(t, "[diff]\npreset = \"strong\"\n[diff.dark]\nremoved-line = \"#8a3535\"\n")
	configured(t, settings.TerminalBackground.Value, settings.Diff)

	added := drawn(t, 1, false, nil)
	removed := drawn(t, 0, false, nil)

	// As lipgloss rounds them: #1d4a1d and #8a3535 each lose one on a channel.
	if !sgrBefore(added, "retry", "48;2;29;73;29") {
		t.Errorf("expected the added row in strong's #1d4a1d: %q", added)
	}
	if !sgrBefore(removed, "retry", "48;2;138;52;52") {
		t.Errorf("expected the removed row in the overriding #8a3535: %q", removed)
	}
}

func TestBackgroundLightDrawsTheLightPaletteOnATerminalDetectedAsDark(t *testing.T) {
	inColour(t)
	settings := settingsFrom(t, "background = \"light\"\n")
	configured(t, settings.TerminalBackground.Value, settings.Diff)

	added := drawn(t, 1, false, nil)

	if !sgrBefore(added, "retry", "48;2;227;245;227") {
		t.Errorf("expected the added row in medium's light #e3f5e3: %q", added)
	}
	// Not only the diff: the + sign's colour is adaptive too, and goes light.
	if !strings.Contains(added, "38;2;32;117;32") {
		t.Errorf("expected the + sign in the light theme's #207520: %q", added)
	}
}

func TestBackgroundDarkDrawsTheDarkPaletteOnATerminalDetectedAsLight(t *testing.T) {
	inColour(t)
	lipgloss.SetHasDarkBackground(false) // inColour's cleanup restores it
	settings := settingsFrom(t, "background = \"dark\"\n")
	configured(t, settings.TerminalBackground.Value, settings.Diff)

	added := drawn(t, 1, false, nil)

	if !sgrBefore(added, "retry", addTintSGR) {
		t.Errorf("expected the added row in medium's dark tint: %q", added)
	}
}

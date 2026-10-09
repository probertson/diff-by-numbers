package config

import (
	"strings"
	"testing"
)

func TestWithNoDiffSettingsTheColoursAreTheMediumPresetOnBothBackgrounds(t *testing.T) {
	env := homeWith(t, "")

	settings, err := Load(env.lookup)

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.TerminalBackground != (Setting[TerminalBackground]{Value: BackgroundAuto, Source: Default}) {
		t.Errorf("TerminalBackground = %+v, want auto from the default", settings.TerminalBackground)
	}
	if settings.Diff.Preset != (Setting[string]{Value: "medium", Source: Default}) {
		t.Errorf("Preset = %+v, want medium from the default", settings.Diff.Preset)
	}
	wantDark := Colours{
		AddedLine:     Setting[string]{Value: "#213a21", Source: Preset},
		AddedChange:   Setting[string]{Value: "#2f6b2f", Source: Preset},
		RemovedLine:   Setting[string]{Value: "#3f2222", Source: Preset},
		RemovedChange: Setting[string]{Value: "#7a2f2f", Source: Preset},
	}
	wantLight := Colours{
		AddedLine:     Setting[string]{Value: "#e3f5e3", Source: Preset},
		AddedChange:   Setting[string]{Value: "#abe2ab", Source: Preset},
		RemovedLine:   Setting[string]{Value: "#fae3e3", Source: Preset},
		RemovedChange: Setting[string]{Value: "#f0a8a8", Source: Preset},
	}
	if settings.Diff.Dark != wantDark {
		t.Errorf("Dark = %+v, want medium's dark %+v", settings.Diff.Dark, wantDark)
	}
	if settings.Diff.Light != wantLight {
		t.Errorf("Light = %+v, want medium's light %+v", settings.Diff.Light, wantLight)
	}
}

// values is a Colours' four hex values in role order, for tests that care only
// about which palette is in force.
func values(c Colours) [4]string {
	return [4]string{c.AddedLine.Value, c.AddedChange.Value, c.RemovedLine.Value, c.RemovedChange.Value}
}

func TestAPresetSetsBothBackgroundsPalettes(t *testing.T) {
	cases := []struct {
		preset      string
		dark, light [4]string
	}{
		{"subtle", [4]string{"#1f2b1f", "#2f4a2f", "#2e1f1f", "#4f2d2d"}, [4]string{"#f0f9f0", "#c9ecc9", "#fbf0f0", "#f3c9c9"}},
		{"strong", [4]string{"#1d4a1d", "#2e8b2e", "#5a1f1f", "#a33a3a"}, [4]string{"#cdeecd", "#8fd68f", "#f6cccc", "#e88a8a"}},
	}
	for _, c := range cases {
		env := homeWith(t, "[diff]\npreset = \""+c.preset+"\"\n")

		settings, err := Load(env.lookup)

		if err != nil {
			t.Fatalf("%s: Load: %v", c.preset, err)
		}
		if settings.Diff.Preset != (Setting[string]{Value: c.preset, Source: File}) {
			t.Errorf("%s: Preset = %+v, want it from the file", c.preset, settings.Diff.Preset)
		}
		if values(settings.Diff.Dark) != c.dark || values(settings.Diff.Light) != c.light {
			t.Errorf("%s: dark %v light %v, want dark %v light %v", c.preset, values(settings.Diff.Dark), values(settings.Diff.Light), c.dark, c.light)
		}
	}
}

func TestAnOverrideReplacesOnlyItsOwnColourOnItsOwnBackground(t *testing.T) {
	env := homeWith(t, "[diff]\npreset = \"strong\"\n\n[diff.dark]\nremoved-change = \"#8a3535\"\n\n[diff.light]\nadded-line = \"#E8F7E8\"\n")

	settings, err := Load(env.lookup)

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Diff.Dark.RemovedChange != (Setting[string]{Value: "#8a3535", Source: File}) {
		t.Errorf("Dark.RemovedChange = %+v, want #8a3535 from the file", settings.Diff.Dark.RemovedChange)
	}
	if settings.Diff.Light.AddedLine != (Setting[string]{Value: "#E8F7E8", Source: File}) {
		t.Errorf("Light.AddedLine = %+v, want #E8F7E8 from the file", settings.Diff.Light.AddedLine)
	}
	if values(settings.Diff.Dark) != [4]string{"#1d4a1d", "#2e8b2e", "#5a1f1f", "#8a3535"} {
		t.Errorf("dark = %v, want strong with only removed-change replaced", values(settings.Diff.Dark))
	}
	if values(settings.Diff.Light) != [4]string{"#E8F7E8", "#8fd68f", "#f6cccc", "#e88a8a"} {
		t.Errorf("light = %v, want strong with only added-line replaced", values(settings.Diff.Light))
	}
}

func TestBackgroundCanBeForcedEitherWay(t *testing.T) {
	for _, value := range []string{"auto", "dark", "light"} {
		env := homeWith(t, "background = \""+value+"\"\n")

		settings, err := Load(env.lookup)

		if err != nil {
			t.Fatalf("%s: Load: %v", value, err)
		}
		if settings.TerminalBackground != (Setting[TerminalBackground]{Value: TerminalBackground(value), Source: File}) {
			t.Errorf("TerminalBackground = %+v, want %s from the file", settings.TerminalBackground, value)
		}
	}
}

func TestAnInvalidColourSettingIsAnErrorNamingTheLineAndTheKey(t *testing.T) {
	cases := []struct {
		name     string
		contents string
		line     string
		key      string
	}{
		{"an unknown preset", "[diff]\npreset = \"loud\"\n", "line 2", "diff.preset"},
		{"an unknown background", "background = \"dim\"\n", "line 1", "background"},
		{"a colour without its #", "[diff.dark]\nadded-line = \"213a21\"\n", "line 2", "diff.dark.added-line"},
		{"a short colour", "[diff.light]\n\nremoved-line = \"#fee\"\n", "line 3", "diff.light.removed-line"},
		{"a colour that is not hex", "[diff.dark]\nadded-change = \"#2f6b2g\"\n", "line 2", "diff.dark.added-change"},
		{"a colour that is not a string", "[diff.dark]\nremoved-change = 7\n", "line 2", "diff.dark.removed-change"},
		{"the reserved table form", "[diff.dark]\nremoved-change = { fg = \"#e05555\" }\n", "line 2", "diff.dark.removed-change"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := homeWith(t, c.contents)

			_, err := Load(env.lookup)

			if err == nil {
				t.Fatal("Load accepted an invalid colour setting")
			}
			if !strings.Contains(err.Error(), c.line) || !strings.Contains(err.Error(), c.key) {
				t.Errorf("error %q does not name %s and %s", err, c.line, c.key)
			}
		})
	}
}

func TestAnUnknownDiffKeyIsAWarning(t *testing.T) {
	env := homeWith(t, "[diff]\nstrength = 2\n\n[diff.dark]\nadded-emph = \"#2f6b2f\"\n")

	settings, err := Load(env.lookup)

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(settings.Warning(), `"diff.strength", "diff.dark.added-emph"`) {
		t.Errorf("Warning() = %q, want both unknown keys named", settings.Warning())
	}
}

package config

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Colours is the four colours a diff is drawn in on one background: a tint
// across a whole changed line, and a stronger one on just the characters that
// changed within it, for added and removed lines. A value is a #rrggbb
// background colour.
type Colours struct {
	AddedLine     Setting[string]
	AddedChange   Setting[string]
	RemovedLine   Setting[string]
	RemovedChange Setting[string]
}

// Role is one of Colours' four, named by its config key.
type Role struct {
	Key    string
	Colour Setting[string]
}

// Roles is the four colours in a fixed order, each with its config key — for
// whatever lists them, so the order and the keys live in one place.
func (c Colours) Roles() []Role {
	return []Role{
		{"added-line", c.AddedLine},
		{"added-change", c.AddedChange},
		{"removed-line", c.RemovedLine},
		{"removed-change", c.RemovedChange},
	}
}

// DiffColours is the diff's colours: the preset they start from, and the
// colours in force on each background once any overrides are applied.
type DiffColours struct {
	Preset Setting[string]
	Dark   Colours
	Light  Colours
}

// palette is one preset's colours on one background, in role order.
type palette struct{ addedLine, addedChange, removedLine, removedChange string }

func (p palette) colours() Colours {
	from := func(hex string) Setting[string] { return Setting[string]{Value: hex, Source: Preset} }
	return Colours{
		AddedLine:     from(p.addedLine),
		AddedChange:   from(p.addedChange),
		RemovedLine:   from(p.removedLine),
		RemovedChange: from(p.removedChange),
	}
}

// presets are the palettes #81's visual check found readable in several
// terminals and monitors, with Comment, cursor and selection states. medium is
// the default, and is what dbn drew before the colours could be set.
var presets = map[string]struct{ dark, light palette }{
	"subtle": {
		dark:  palette{"#1f2b1f", "#2f4a2f", "#2e1f1f", "#4f2d2d"},
		light: palette{"#f0f9f0", "#c9ecc9", "#fbf0f0", "#f3c9c9"},
	},
	"medium": {
		dark:  palette{"#213a21", "#2f6b2f", "#3f2222", "#7a2f2f"},
		light: palette{"#e3f5e3", "#abe2ab", "#fae3e3", "#f0a8a8"},
	},
	"strong": {
		dark:  palette{"#1d4a1d", "#2e8b2e", "#5a1f1f", "#a33a3a"},
		light: palette{"#cdeecd", "#8fd68f", "#f6cccc", "#e88a8a"},
	},
}

const defaultPreset = "medium"

func defaultDiffColours() DiffColours {
	return presetColours(Setting[string]{Value: defaultPreset, Source: Default})
}

func presetColours(preset Setting[string]) DiffColours {
	chosen := presets[preset.Value]
	return DiffColours{Preset: preset, Dark: chosen.dark.colours(), Light: chosen.light.colours()}
}

// diffShape is the file's [diff] table.
type diffShape struct {
	Preset *presetName   `toml:"preset"`
	Dark   *coloursShape `toml:"dark"`
	Light  *coloursShape `toml:"light"`
}

// coloursShape is a [diff.dark] or [diff.light] table: overrides of the preset,
// each optional.
type coloursShape struct {
	AddedLine     *hexColour `toml:"added-line"`
	AddedChange   *hexColour `toml:"added-change"`
	RemovedLine   *hexColour `toml:"removed-line"`
	RemovedChange *hexColour `toml:"removed-change"`
}

// apply lays the file's [diff] table over the default colours.
func (d *diffShape) apply() DiffColours {
	colours := defaultDiffColours()
	if d == nil {
		return colours
	}
	if d.Preset != nil {
		colours = presetColours(Setting[string]{Value: string(*d.Preset), Source: File})
	}
	d.Dark.override(&colours.Dark)
	d.Light.override(&colours.Light)
	return colours
}

func (c *coloursShape) override(colours *Colours) {
	if c == nil {
		return
	}
	for _, role := range []struct {
		set *hexColour
		to  *Setting[string]
	}{
		{c.AddedLine, &colours.AddedLine},
		{c.AddedChange, &colours.AddedChange},
		{c.RemovedLine, &colours.RemovedLine},
		{c.RemovedChange, &colours.RemovedChange},
	} {
		if role.set != nil {
			*role.to = Setting[string]{Value: string(*role.set), Source: File}
		}
	}
}

// presetName is a preset's name, checked as it is decoded so the error carries
// its line.
type presetName string

func (p *presetName) UnmarshalTOML(data any) error {
	name, ok := data.(string)
	if _, known := presets[name]; !ok || !known {
		return fmt.Errorf("must be one of %s, got %v", strings.Join(presetNames(), ", "), data)
	}
	*p = presetName(name)
	return nil
}

func presetNames() []string {
	names := make([]string, 0, len(presets))
	for name := range presets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// hexPattern is the only form a colour takes for now. A bare string means the
// background colour; an inline table ({ fg = …, strikethrough = true }) is
// reserved for richer styles (ADR-0019), so it is refused rather than read as
// anything else.
var hexPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// hexColour is a #rrggbb colour, checked as it is decoded.
type hexColour string

func (h *hexColour) UnmarshalTOML(data any) error {
	value, ok := data.(string)
	if !ok || !hexPattern.MatchString(value) {
		return fmt.Errorf("must be a colour written #rrggbb, got %v", data)
	}
	*h = hexColour(value)
	return nil
}

// TerminalBackground is the background setting: whether the terminal's
// background is detected, or forced when detection guesses wrong. (The config
// key is `background`; the Go name keeps clear of a Brief's Background.)
type TerminalBackground string

const (
	BackgroundAuto  TerminalBackground = "auto"
	BackgroundDark  TerminalBackground = "dark"
	BackgroundLight TerminalBackground = "light"
)

var backgrounds = []TerminalBackground{BackgroundAuto, BackgroundDark, BackgroundLight}

func (b *TerminalBackground) UnmarshalTOML(data any) error {
	value, ok := data.(string)
	if ok {
		for _, known := range backgrounds {
			if TerminalBackground(value) == known {
				*b = known
				return nil
			}
		}
	}
	names := make([]string, len(backgrounds))
	for i, known := range backgrounds {
		names[i] = string(known)
	}
	return fmt.Errorf("must be one of %s, got %v", strings.Join(names, ", "), data)
}

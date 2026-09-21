package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// Added and removed rows are tinted, and between a removed line and the added
// line matched with it the words that changed carry a stronger tint (#81). Each signal
// keeps its own channel: tint for the change, foreground for a Comment, bold
// for the cursor, blue background for a selection, dim for context.

// Palette B, dark, as the SGR parameters a truecolour terminal receives —
// after lipgloss's colour conversion, which can round a channel by one.
const (
	addTintSGR  = "48;2;32;58;32"    // #213a21, as lipgloss rounds it
	addEmphSGR  = "48;2;47;107;47"   // #2f6b2f
	delTintSGR  = "48;2;63;34;34"    // #3f2222
	selectedSGR = "48;2;10;52;80"    // #0a3550, as lipgloss rounds it
	commentSGR  = "38;2;255;215;135" // #ffd787
)

// inColour forces a dark truecolour terminal for the length of a test: lipgloss
// strips styling when stdout is not a terminal, and its profile is global.
func inColour(t *testing.T) {
	t.Helper()
	profile, dark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(profile)
		lipgloss.SetHasDarkBackground(dark)
	})
}

// tintStep is one Excerpt: a removed line, the added line in its place, and a line of
// context, the first two matched, with the number that changed emphasised.
func tintStep() *daemon.StepWire {
	return &daemon.StepWire{Name: "s", Explanation: "e", Excerpts: []daemon.ExcerptWire{{
		File: "f.go", Side: "new", FirstLine: 1, LastLine: 2,
		Lines: []daemon.LineWire{
			{Number: 1, Side: "old", Text: "retry(transport, 3)", Changed: true, Emphasis: [][2]int{{17, 18}}},
			{Number: 1, Side: "new", Text: "retry(transport, 5)", Changed: true, Emphasis: [][2]int{{17, 18}}},
			{Number: 2, Side: "new", Text: "return err", Changed: false},
		},
	}}}
}

// drawn renders row i of tintStep with the cursor parked on the context row, so
// the row under test is not also the cursor unless a test puts it there.
func drawn(t *testing.T, i int, commented bool, adjust func(*stepCursor)) string {
	t.Helper()
	cur := newStepCursor(tintStep(), nil)
	cur.cursor = 2
	if adjust != nil {
		adjust(&cur)
	}
	return strings.Join(codeRows(cur, i, commented, 60, 4, false), "\n")
}

// sgrBefore reports whether the text is drawn under a style carrying sgr: the
// last escape sequence before it names that parameter.
func sgrBefore(row, text, sgr string) bool {
	at := strings.Index(row, text)
	if at < 0 {
		return false
	}
	escape := strings.LastIndex(row[:at], "\x1b[")
	return escape >= 0 && strings.Contains(row[escape:at], sgr)
}

func TestAChangedRowIsTintedAndOnlyItsChangedWordsAreEmphasised(t *testing.T) {
	inColour(t)

	added := drawn(t, 1, false, nil)
	removed := drawn(t, 0, false, nil)

	if !sgrBefore(added, "retry", addTintSGR) || !sgrBefore(added, "5\x1b", addEmphSGR) {
		t.Errorf("expected the added row tinted and its 5 emphasised: %q", added)
	}
	if sgrBefore(added, "retry", addEmphSGR) {
		t.Errorf("expected the unchanged words not emphasised: %q", added)
	}
	if !sgrBefore(removed, "retry", delTintSGR) {
		t.Errorf("expected the removed row tinted red: %q", removed)
	}
}

func TestContextHasNoTint(t *testing.T) {
	inColour(t)

	context := drawn(t, 2, false, func(c *stepCursor) { c.cursor = 0 })

	if strings.Contains(context, "48;2;") {
		t.Errorf("expected context with no background at all: %q", context)
	}
}

func TestACommentAndTheCursorKeepTheirOwnChannelsOnATintedRow(t *testing.T) {
	inColour(t)

	commented := drawn(t, 1, true, nil)
	underCursor := drawn(t, 1, false, func(c *stepCursor) { c.cursor = 1 })

	if !sgrBefore(commented, "retry", commentSGR) || !sgrBefore(commented, "retry", addTintSGR) {
		t.Errorf("expected a Comment's foreground over the tint: %q", commented)
	}
	if !sgrBefore(underCursor, "retry", "1;") || !sgrBefore(underCursor, "retry", addTintSGR) {
		t.Errorf("expected the cursor's bold over the tint: %q", underCursor)
	}
}

func TestASelectionOverridesTheTint(t *testing.T) {
	inColour(t)

	selected := drawn(t, 1, false, func(c *stepCursor) { c.cursor, c.sel = 1, 1 })

	if !sgrBefore(selected, "retry", selectedSGR) || strings.Contains(selected, addTintSGR) || strings.Contains(selected, addEmphSGR) {
		t.Errorf("expected the selection's blue in place of every tint: %q", selected)
	}
}

func TestEmphasisSurvivesWrapping(t *testing.T) {
	inColour(t)
	long := strings.Repeat("word ", 14) + "CHANGED tail"
	step := &daemon.StepWire{Name: "s", Explanation: "e", Excerpts: []daemon.ExcerptWire{{
		File: "f.go", Side: "new", FirstLine: 1, LastLine: 1,
		Lines: []daemon.LineWire{{Number: 1, Side: "new", Text: long, Changed: true, Emphasis: [][2]int{{70, 77}}}},
	}}}
	cur := newStepCursor(step, nil)

	rows := codeRows(cur, 0, false, 50, 4, true)

	if len(rows) < 2 {
		t.Fatalf("expected the line wrapped, got %q", rows)
	}
	last := rows[len(rows)-1]
	if !sgrBefore(last, "CHANGED", addEmphSGR) || !sgrBefore(last, "tail", addTintSGR) {
		t.Errorf("expected the emphasis carried onto the continuation row: %q", last)
	}
}

func TestATabBeforeTheChangeDoesNotShiftTheEmphasis(t *testing.T) {
	inColour(t)
	step := &daemon.StepWire{Name: "s", Explanation: "e", Excerpts: []daemon.ExcerptWire{{
		File: "f.go", Side: "new", FirstLine: 1, LastLine: 1,
		Lines: []daemon.LineWire{{Number: 1, Side: "new", Text: "\treturn 5", Changed: true, Emphasis: [][2]int{{8, 9}}}},
	}}}
	cur := newStepCursor(step, nil)

	row := strings.Join(codeRows(cur, 0, false, 60, 4, false), "")

	if !sgrBefore(row, "5\x1b", addEmphSGR) || sgrBefore(row, "return", addEmphSGR) {
		t.Errorf("expected only the 5 emphasised after the tab is expanded: %q", row)
	}
}

func TestAChangedRowTooNarrowForItsCodeIsTintedPlain(t *testing.T) {
	inColour(t)
	// Emphasis at the very start of the code, where offsets into the code
	// would land on the gutter instead if the two were run together.
	step := &daemon.StepWire{Name: "s", Explanation: "e", Excerpts: []daemon.ExcerptWire{{
		File: "f.go", Side: "new", FirstLine: 1, LastLine: 1,
		Lines: []daemon.LineWire{{Number: 1, Side: "new", Text: "retry(transport, 5)", Changed: true, Emphasis: [][2]int{{0, 5}}}},
	}}}
	cur := newStepCursor(step, nil)
	cur.cursor = -1

	row := strings.Join(codeRows(cur, 0, false, 12, 4, false), "")

	if strings.Contains(row, addEmphSGR) || !strings.Contains(row, addTintSGR) {
		t.Errorf("expected the clipped row tinted, with nothing emphasised: %q", row)
	}
}

func TestTheEllipsisOfAClippedRowIsNeverEmphasised(t *testing.T) {
	inColour(t)
	step := &daemon.StepWire{Name: "s", Explanation: "e", Excerpts: []daemon.ExcerptWire{{
		File: "f.go", Side: "new", FirstLine: 1, LastLine: 1,
		// The emphasised word starts exactly where a 30-cell row is clipped.
		Lines: []daemon.LineWire{{Number: 1, Side: "new", Text: "abcdefghijklmnopq CHANGED", Changed: true, Emphasis: [][2]int{{18, 25}}}},
	}}}
	cur := newStepCursor(step, nil)
	cur.cursor = -1

	row := strings.Join(codeRows(cur, 0, false, 32, 4, false), "")

	if !strings.Contains(row, "…") || sgrBefore(row, "…", addEmphSGR) {
		t.Errorf("expected the ellipsis on the row's own tint, not the clipped word's: %q", row)
	}
}

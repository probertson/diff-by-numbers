package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// twoLongLines is the case #77 exists for: a long removal and the addition that
// replaced it, neither readable in one row. Each tail is distinctive so a test
// can say which of them reached the screen.
func twoLongLines() *daemon.StepWire {
	return &daemon.StepWire{
		Name: "Rework the call", Explanation: "x",
		Excerpts: []daemon.ExcerptWire{{File: "a.go", Side: "new", FirstLine: 1, LastLine: 2, Lines: []daemon.LineWire{
			{Number: 1, Text: "was := " + strings.Repeat("o", 30) + " OLDTAIL", Changed: true, Side: "old"},
			{Number: 1, Text: "now := " + strings.Repeat("n", 30) + " NEWTAIL", Changed: true, Side: "new"},
		}}},
	}
}

func TestWTogglesWrappingEveryLine(t *testing.T) {
	m := stepModel(t, twoLongLines())

	on := press(m, "w")

	if !on.wrapAll {
		t.Fatal("w should turn wrapping on for every line")
	}
	if press(on, "w").wrapAll {
		t.Error("a second w should turn it back off")
	}
}

func TestWrappingEveryLineSurvivesAStepChange(t *testing.T) {
	m := press(memoryModel(t, twoLongLines()), "w")

	moved := arrive(m, 1, 2, otherStep())

	if !moved.wrapAll {
		t.Error("wrapping every line is a setting for the review, not for one Step")
	}
}

func TestWrappingEveryLineShowsTwoLongLinesAtOnce(t *testing.T) {
	step := twoLongLines()
	cur := newStepCursor(step, nil) // on the removal, not the addition
	const width, height = 50, 30

	out := renderStep(step, cur, map[string]bool{}, nil, width, height, false, true)

	if !strings.Contains(out, "OLDTAIL") || !strings.Contains(out, "NEWTAIL") {
		t.Errorf("both long lines should be readable in full at once, got:\n%s", out)
	}
	if w := widestLine(out); w > width {
		t.Errorf("a wrapped row is %d cells wide, over the %d given:\n%s", w, width, out)
	}
}

func TestTogglingBackRestoresTheCursorLineOnlyWrap(t *testing.T) {
	m := stepModel(t, twoLongLines())
	m.width, m.height = 50, 30

	on := press(m, "w")
	off := press(on, "w")

	if !strings.Contains(on.View(), "NEWTAIL") {
		t.Errorf("wrapping every line should bring the other tail into view, got:\n%s", on.View())
	}
	if strings.Contains(off.View(), "NEWTAIL") {
		t.Errorf("toggling back should restore the cursor-line-only wrap, got:\n%s", off.View())
	}
	if !strings.Contains(off.View(), "OLDTAIL") {
		t.Errorf("the cursor line should still wrap on its own, got:\n%s", off.View())
	}
}

func TestWrappingEveryLineKeepsTheCursorLineOnScreen(t *testing.T) {
	step := tallStep() // 40 lines, more than fits
	cur := newStepCursor(step, nil)
	cur.cursor = 30

	out := renderStep(step, cur, map[string]bool{}, nil, 80, 20, false, true)

	if !strings.Contains(out, "code line 31") {
		t.Errorf("the cursor's own line must stay visible, got:\n%s", out)
	}
	if h := lipgloss.Height(out); h > 20 {
		t.Errorf("the pane is %d rows, over the 20 given:\n%s", h, out)
	}
}

func TestWrappingEveryLineClipsALineTallerThanThePane(t *testing.T) {
	// One line wraps to far more rows than the pane has. It is pinned to its first
	// row and the rest is clipped behind the downward marker, rather than growing
	// the pane past its height.
	huge := strings.Repeat("word ", 400)
	step := oneLineStep(huge)
	const width, height = 40, 16
	cur := newStepCursor(step, nil)

	out := renderStep(step, cur, map[string]bool{}, nil, width, height, false, true)

	if h := lipgloss.Height(out); h > height {
		t.Errorf("an oversized wrapped line blew the height: %d rows over the %d given:\n%s", h, height, out)
	}
	if w := widestLine(out); w > width {
		t.Errorf("a wrapped row is %d cells wide, over the %d given", w, width)
	}
	if !strings.Contains(out, "more below") {
		t.Errorf("the clipped remainder should be announced by the downward marker, got:\n%s", out)
	}
}

func TestTheStepPaneSurvivesAPaneTooShortForItsHeaders(t *testing.T) {
	// A few rows of pane, a cursor inside an expanded Acknowledgement (so its row
	// carries both the file header and the Acknowledgement's), and code that
	// overflows. The headers can want more rows than the window has, which used to
	// leave the window inverted.
	step := ackStep()
	long := strings.Repeat("generated ", 12)
	expansion := []daemon.ExcerptWire{{File: "gen.go", Side: "new", FirstLine: 2, LastLine: 4, Lines: []daemon.LineWire{
		{Number: 2, Text: long, Side: "old", Changed: true},
		{Number: 2, Text: long, Side: "new", Changed: true},
		{Number: 3, Text: "short", Side: "new", Changed: true},
	}}}
	cur := newStepCursor(step, map[int][]daemon.ExcerptWire{0: expansion})

	for _, wrapAll := range []bool{false, true} {
		for width := 24; width <= 60; width += 4 {
			for height := 5; height <= 14; height++ {
				for i := range cur.lines {
					cur.cursor = i

					out := renderStep(step, cur, map[string]bool{}, nil, width, height, false, wrapAll)

					if out == "" {
						t.Errorf("wrapAll=%v width=%d height=%d cursor=%d rendered nothing", wrapAll, width, height, i)
					}
				}
			}
		}
	}
}

func TestWrappingEveryLineStillMovesTheCursorBySourceLine(t *testing.T) {
	m := press(stepModel(t, twoLongLines()), "w")

	moved := press(m, "down")

	if moved.cursor.cursor != 1 {
		t.Errorf("↑/↓ move by source line, not by wrapped row, got index %d", moved.cursor.cursor)
	}
}

func TestSelectingAcrossWrappedLinesAnchorsTheSourceLines(t *testing.T) {
	m := press(press(stepModel(t, twoLongLines()), "w"), " ") // start selecting on the removal

	extended, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftDown})
	run, ok := extended.(model).cursor.selection()

	if !ok {
		t.Fatal("a selection over two wrapped lines should still anchor")
	}
	if run.rows != 2 {
		t.Errorf("a selection is counted in source lines, not wrapped rows, got %d", run.rows)
	}
	if run.start.side != "old" || run.end.side != "new" {
		t.Errorf("the selection should span the removal and its replacement, got %+v", run)
	}
}

func TestTheKeybarNamesWhatWWillDo(t *testing.T) {
	m := stepModel(t, twoLongLines())

	if !strings.Contains(m.modeKeys(), keybar("w wrap all lines")) {
		t.Errorf("expected the keybar to offer wrapping every line, got:\n%s", m.modeKeys())
	}

	on := press(m, "w")

	if !strings.Contains(on.modeKeys(), keybar("w wrap cursor line only")) {
		t.Errorf("expected the keybar to offer the way back, got:\n%s", on.modeKeys())
	}
}

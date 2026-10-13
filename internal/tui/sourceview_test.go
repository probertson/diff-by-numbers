package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// redTextSGR is the terminal palette's red, as an SGR parameter.
const redTextSGR = "31"

// s switches every Document between its formatted view and its source: the
// source lines as they stand, signed + and -, with the changed text itself
// coloured rather than banded. Code is drawn as it always was.

func TestSShowsADocumentsSourceAndSAgainShowsItFormatted(t *testing.T) {
	m := stepModel(t, documentStep(retries()))

	source := press(m, "s")
	formatted := press(source, "s")

	out := ansi.Strip(drawnStep(source))
	if !strings.Contains(out, "A request is **retried** when") || !strings.Contains(lineWith(t, out, "attempts."), "6") {
		t.Errorf("expected the paragraph's source lines one to a row, numbered:\n%s", out)
	}
	if !strings.Contains(ansi.Strip(drawnStep(formatted)), "3–6") {
		t.Errorf("expected a second s to show the Document formatted again:\n%s", drawnStep(formatted))
	}
}

func TestANewLineOfSourceIsGreenInFullWithNoBand(t *testing.T) {
	inColour(t)
	m := press(stepModel(t, documentStep(retries())), "s")

	out := drawnStep(m)

	for _, text := range []string{"# Retries", "the _transport_ fails, and", "- the first attempt"} {
		row := lineWith(t, out, text)
		if !strings.HasPrefix(strings.TrimLeft(ansi.Strip(row), "▸ "), "+") {
			t.Errorf("expected the new line signed +: %q", row)
		}
		if !drawnWith(t, row, text, greenSGR) {
			t.Errorf("expected the new line's text green: %q", row)
		}
		if strings.Contains(row, "48;") {
			t.Errorf("expected no background band: %q", row)
		}
	}
}

func TestInARewriteOnlyTheChangedWordsAreColoured(t *testing.T) {
	inColour(t)
	// Line 5 of the paragraph was rewritten: "waited" became "waits".
	excerpt := retries()
	was := daemon.LineWire{Number: 5, Text: "`retry()` waited between", Side: "old", Changed: true, Emphasis: [][2]int{{10, 16}}, Block: 2}
	excerpt.Lines = slices.Concat(excerpt.Lines[:4], []daemon.LineWire{was}, excerpt.Lines[4:])
	excerpt.Lines[5].Emphasis = [][2]int{{10, 15}}
	m := press(stepModel(t, documentStep(excerpt)), "s")

	out := drawnStep(m)

	removed, added := lineWith(t, out, "waited"), lineWith(t, out, "waits")
	if !strings.Contains(ansi.Strip(removed), "-") || !drawnWith(t, removed, "waited", redTextSGR) {
		t.Errorf("expected the removed word red on a - line: %q", removed)
	}
	if !drawnWith(t, added, "waits", greenSGR) {
		t.Errorf("expected the added word green: %q", added)
	}
	if drawnWith(t, added, "between", greenSGR) || drawnWith(t, removed, "between", redTextSGR) {
		t.Errorf("expected the unchanged words of a rewrite uncoloured:\n%s\n%s", removed, added)
	}
	if strings.Contains(out, "\x1b[9m") || strings.Contains(out, ";9m") || strings.Contains(out, ";9;") || strings.Contains(out, "48;") {
		t.Errorf("expected no strikethrough and no band in the source view:\n%q", out)
	}
}

func TestSourceLinesUseThePanesFullWidth(t *testing.T) {
	long := "this source line runs far past the prose width but well within the pane"
	m := stepModel(t, documentStep(documentExcerpt("docs/retries.md", para(long))))
	m.width, m.proseWidth = 200, 30

	out := ansi.Strip(drawnStep(press(m, "s")))

	if !strings.Contains(out, long) {
		t.Errorf("expected the source line on one row, unbroken by the prose width:\n%s", out)
	}
}

func TestInTheSourceACommentIsMarkedOnEveryLineItCovers(t *testing.T) {
	inColour(t)
	m := press(stepModel(t, documentStep(retries())), "s")
	m.view.Comments = []daemon.CommentWire{{ID: 1, Step: 1, File: "docs/retries.md", Segments: []daemon.SegmentWire{
		{Side: "new", FirstLine: 4, LastLine: 5},
	}}}

	out := drawnStep(m)

	if strings.Count(out, "●") != 2 {
		t.Fatalf("expected a mark on each of the two lines the Comment covers:\n%s", out)
	}
	for _, text := range []string{"the _transport_ fails, and", "`retry()` waits between"} {
		if row := lineWith(t, out, text); !drawnWith(t, row, "●", commentSGR) {
			t.Errorf("expected the line marked in the Comment colour: %q", row)
		}
	}
	if strings.Contains(out, "✎") {
		t.Errorf("expected a Document's source to use the gutter mark, not the code's:\n%s", out)
	}
}

func TestShowingTheSourceLeavesCodeAsItWas(t *testing.T) {
	inColour(t)
	code := daemon.ExcerptWire{File: "retry.go", Side: "new", FirstLine: 1, LastLine: 2, Lines: []daemon.LineWire{
		{Number: 1, Text: "func retry() {", Side: "new", Changed: true},
		{Number: 2, Text: "}", Side: "new", Changed: true},
	}}
	m := stepModel(t, documentStep(retries(), code))

	source := press(m, "s")

	for _, text := range []string{"func retry() {", "}"} {
		if before, after := lineWith(t, drawnStep(m), text), lineWith(t, drawnStep(source), text); before != after {
			t.Errorf("expected code drawn the same in both views:\n%q\n%q", before, after)
		}
	}
}

func TestTheNextStepsDocumentsOpenInTheSameView(t *testing.T) {
	m := press(memoryModel(t, documentStep(retries())), "s")
	m.width, m.height = 80, 40
	next := documentStep(documentExcerpt("docs/backoff.md", para("Backoff doubles", "each time.")))

	m = arrive(m, 1, 2, next)

	if out := ansi.Strip(drawnStep(m)); !strings.Contains(out, "Backoff doubles") || strings.Contains(out, "Backoff doubles each") {
		t.Errorf("expected the next Step's Document shown as source too:\n%s", out)
	}
	if !strings.Contains(m.modeKeys(), keybar("s show formatted")) {
		t.Errorf("expected the keybar to still offer the way back, got:\n%s", m.modeKeys())
	}
}

func TestTheKeybarNamesWhatSWillDo(t *testing.T) {
	m := stepModel(t, documentStep(retries()))

	source := press(m, "s")

	if !strings.Contains(m.modeKeys(), keybar("s show source")) {
		t.Errorf("expected the keybar to offer the source, got:\n%s", m.modeKeys())
	}
	if !strings.Contains(source.modeKeys(), keybar("s show formatted")) {
		t.Errorf("expected the keybar to offer the way back, got:\n%s", source.modeKeys())
	}
}

// cursorText is the text of the row the cursor is on, as drawn.
func cursorText(t *testing.T, m model) string {
	t.Helper()
	return ansi.Strip(lineWith(t, drawnStep(m), "▸"))
}

func TestShowingTheSourcePutsTheCursorOnItsBlocksFirstLine(t *testing.T) {
	m := press(stepModel(t, documentStep(retries())), "down") // the paragraph, lines 3-6

	m = press(m, "s")

	if got := cursorText(t, m); !strings.Contains(got, "3 │ A request is") {
		t.Errorf("expected the cursor on the paragraph's first line, got %q", got)
	}
}

func TestShowingItFormattedPutsTheCursorOnTheBlockHoldingItsLine(t *testing.T) {
	m := press(stepModel(t, documentStep(retries())), "s")
	for range 4 {
		m = press(m, "down") // line 5, inside the paragraph
	}

	m = press(m, "s")

	if got := cursorText(t, m); !strings.Contains(got, "3–6") {
		t.Errorf("expected the cursor on the paragraph, got %q", got)
	}
}

func TestABlankLineBetweenBlocksReturnsToTheBlockAboveIt(t *testing.T) {
	m := press(stepModel(t, documentStep(retries())), "s")
	for range 6 {
		m = press(m, "down") // line 7, between the paragraph and the list item
	}

	m = press(m, "s")

	if got := cursorText(t, m); !strings.Contains(got, "3–6") {
		t.Errorf("expected the cursor on the paragraph above the blank line, got %q", got)
	}
}

func TestReturningToAStepAfterSwitchingViewsFindsTheSameText(t *testing.T) {
	step, other := documentStep(retries()), documentStep(documentExcerpt("docs/backoff.md", para("Backoff doubles.")))
	m := memoryModel(t, step)
	m.width, m.height = 80, 40
	m = press(m, "down") // the paragraph

	m = arrive(m, 1, 2, other)
	m = press(m, "s")
	m = arrive(m, 1, 1, step)

	if got := cursorText(t, m); !strings.Contains(got, "3 │ A request is") {
		t.Errorf("expected the cursor back on the paragraph's first line, got %q", got)
	}

	m = press(m, "down") // line 4
	m = arrive(m, 1, 2, other)
	m = press(m, "s")
	m = arrive(m, 1, 1, step)

	if got := cursorText(t, m); !strings.Contains(got, "3–6") {
		t.Errorf("expected the cursor back on the paragraph, got %q", got)
	}
}

// cursorRow is the screen row the cursor is drawn on, counted from the top of
// the window.
func cursorRow(t *testing.T, m model) int {
	t.Helper()
	for i, row := range strings.Split(m.View(), "\n") {
		if strings.Contains(row, "▸") {
			return i
		}
	}
	t.Fatalf("expected the cursor on screen:\n%s", m.View())
	return -1
}

// longDocument is twenty three-line paragraphs, far more than one screen.
func longDocument() *daemon.StepWire {
	var blocks []docBlock
	for i := range 20 {
		blocks = append(blocks, para(
			fmt.Sprintf("paragraph %d opens", i+1),
			fmt.Sprintf("paragraph %d goes on", i+1),
			fmt.Sprintf("paragraph %d closes", i+1),
		))
	}
	return documentStep(documentExcerpt("docs/retries.md", blocks...))
}

func TestShowingTheSourceKeepsTheCursorOnTheSameScreenRow(t *testing.T) {
	for _, width := range []int{50, 80} {
		for block := 1; block <= 20; block++ {
			m := stepModel(t, longDocument())
			m.width, m.height = width, 30
			for range block - 1 {
				m = press(m, "down")
			}
			formattedRow := cursorRow(t, m)

			source := press(m, "s")

			want := fmt.Sprintf("paragraph %d opens", block)
			if got := cursorText(t, source); !strings.Contains(got, want) {
				t.Fatalf("width %d: expected the cursor on %q, got %q", width, want, got)
			}
			if got := cursorRow(t, source); got != formattedRow {
				t.Errorf("width %d, paragraph %d: expected the cursor to stay on screen row %d, got %d:\n%s", width, block, formattedRow, got, source.View())
			}
		}
	}
}

// Formatted, the Document takes fewer rows than its source, so within a screen
// of either end there is not enough of it to put above or below the cursor; the
// pane does not scroll past its ends. Every line between is checked.
func TestShowingItFormattedKeepsTheCursorOnTheSameScreenRow(t *testing.T) {
	for _, width := range []int{50, 80} {
		for line := 13; line <= 60; line++ {
			m := press(stepModel(t, longDocument()), "s")
			m.width, m.height = width, 30
			for range line - 1 {
				m = press(m, "down")
			}
			sourceRow := cursorRow(t, m)
			block := (line-1)/4 + 1 // four rows a paragraph, with the blank line after it

			formatted := press(m, "s")

			want := fmt.Sprintf("paragraph %d opens", block)
			if got := cursorText(t, formatted); !strings.Contains(got, want) {
				t.Fatalf("width %d, line %d: expected the cursor on %q, got %q", width, line, want, got)
			}
			if got := cursorRow(t, formatted); got != sourceRow {
				t.Errorf("width %d, line %d: expected the cursor to stay on screen row %d, got %d:\n%s", width, line, sourceRow, got, formatted.View())
			}
		}
	}
}

func TestSwitchingViewsDropsASelection(t *testing.T) {
	m := press(press(stepModel(t, documentStep(retries())), "v"), "down")

	m = press(m, "s")

	if m.cursor.sel >= 0 {
		t.Error("expected the selection dropped: its stops are not the new view's")
	}
}

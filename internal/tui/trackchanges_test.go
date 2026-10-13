package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// An edit to a Document is drawn in place, as track changes: one merged flow
// in the new structure, removed words red and struck through, added words green
// and underlined, with a yellow ± on its rows. dbn says what the edit kept,
// struck and added; the TUI only lays the runs out and styles them.

const (
	strikeSGR    = "9"
	underlineSGR = "4"
)

// rowReading is the one row of out that reads text once its styling is
// stripped: lipgloss draws struck and underlined text a rune at a time.
func rowReading(t *testing.T, out, text string) string {
	t.Helper()
	var found []string
	for _, row := range strings.Split(out, "\n") {
		if strings.Contains(ansi.Strip(row), text) {
			found = append(found, row)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one row reading %q, found %d in:\n%s", text, len(found), out)
	}
	return found[0]
}

// trackedWith reports whether the first rune of word, as row reads once
// stripped, is drawn with every one of params.
func trackedWith(t *testing.T, row, word string, params ...string) bool {
	t.Helper()
	at := strings.Index(ansi.Strip(row), word)
	if at < 0 {
		t.Fatalf("expected %q in %q", word, ansi.Strip(row))
	}
	skipEscapes := func(raw int) int {
		for strings.HasPrefix(row[raw:], "\x1b[") {
			raw += strings.IndexByte(row[raw:], 'm') + 1
		}
		return raw
	}
	raw := 0
	for plain := 0; plain < at; plain++ {
		raw = skipEscapes(raw) + 1
	}
	const probe = "\x00"
	return drawnWith(t, row[:skipEscapes(raw)]+probe, probe, params...)
}

// editedRetries is a heading, then a paragraph whose edit struck "retried" and
// added "resent": dbn's view of it carries the removed line, tagged with the
// paragraph, above the line that replaced it.
func editedRetries() daemon.ExcerptWire {
	return daemon.ExcerptWire{File: "docs/retries.md", Side: "new", FirstLine: 1, LastLine: 3,
		Lines: []daemon.LineWire{
			{Number: 1, Text: "# Retries", Side: "new", Block: 1},
			{Number: 2, Text: "", Side: "new"},
			{Number: 3, Text: "A request is retried when the transport fails.", Side: "old", Changed: true, Block: 2},
			{Number: 3, Text: "A request is resent when the transport fails.", Side: "new", Changed: true, Block: 2, Runs: []daemon.RunWire{
				{Text: "A request is ", Change: "kept"},
				{Text: "retried", Change: "removed"},
				{Text: "resent", Change: "added"},
				{Text: " when the transport fails.", Change: "kept"},
			}},
		},
		Blocks: []daemon.BlockWire{
			{Kind: "heading", Level: 1, Side: "new", FirstLine: 1, LastLine: 1, Treatment: "context"},
			{Kind: "paragraph", Side: "new", FirstLine: 3, LastLine: 3, Treatment: "edited"},
		},
	}
}

func TestAnEditedParagraphIsDrawnAsOneMergedFlowOfTrackChanges(t *testing.T) {
	inColour(t)
	m := stepModel(t, documentStep(editedRetries()))

	out := drawnStep(m)

	row := rowReading(t, out, "retried") // drawn once: the removed line is not drawn on its own
	if !strings.Contains(ansi.Strip(row), "A request is retried resent when the transport fails.") {
		t.Errorf("expected the struck and added words in one flow: %q", ansi.Strip(row))
	}
	if !trackedWith(t, row, "retried", redSGR, strikeSGR) {
		t.Errorf("expected the removed word red and struck through: %q", row)
	}
	if !trackedWith(t, row, "resent", greenSGR, underlineSGR) {
		t.Errorf("expected the added word green and underlined: %q", row)
	}
	if trackedWith(t, row, "request", strikeSGR) || trackedWith(t, row, "request", underlineSGR) {
		t.Errorf("expected the kept words unmarked: %q", row)
	}
	if !drawnWith(t, row, "±", yellowSGR) || !strings.Contains(ansi.Strip(row), "3 ") {
		t.Errorf("expected a yellow ± and the paragraph's line in the gutter: %q", row)
	}
}

func TestEveryRowOfAnEditedBlockIsSignedAndTheCursorsSignIsBlue(t *testing.T) {
	inColour(t)
	m := stepModel(t, documentStep(editedRetries()))
	m.width = 30 // narrow enough that the paragraph takes several rows
	m = press(m, "down")

	out := drawnStep(m)

	var rows []string
	for _, row := range strings.Split(out, "\n") {
		if strings.Contains(row, "request") || strings.Contains(row, "transport") || strings.Contains(row, "fails.") {
			rows = append(rows, row)
		}
	}
	if len(rows) < 2 {
		t.Fatalf("expected the paragraph over several rows:\n%s", out)
	}
	for _, row := range rows {
		if !drawnWith(t, row, "±", blueSGR) {
			t.Errorf("expected every row of the cursor's edited block signed with a blue ±: %q", row)
		}
	}
}

func TestARemovedBlockIsSignedWithARedMinusAndANewOneStaysQuiet(t *testing.T) {
	inColour(t)
	excerpt := daemon.ExcerptWire{File: "docs/retries.md", Side: "new", FirstLine: 3, LastLine: 3,
		Lines: []daemon.LineWire{
			{Number: 3, Text: "Old limits applied once.", Side: "old", Changed: true, Block: 1},
			{Number: 3, Text: "Brand new caps hold.", Side: "new", Changed: true, Block: 2},
		},
		Blocks: []daemon.BlockWire{
			{Kind: "paragraph", Side: "old", FirstLine: 3, LastLine: 3, Treatment: "removed"},
			{Kind: "paragraph", Side: "new", FirstLine: 3, LastLine: 3, Treatment: "new"},
		},
	}
	m := stepModel(t, documentStep(excerpt))
	m = press(m, "down") // onto the new block, so the removed one keeps its own colour

	out := drawnStep(m)

	removed, added := rowReading(t, out, "Old limits"), rowReading(t, out, "Brand new")
	if !drawnWith(t, removed, "-", redSGR) {
		t.Errorf("expected the removed block signed with a red -: %q", removed)
	}
	if !trackedWith(t, removed, "Old", redSGR, strikeSGR) {
		t.Errorf("expected the removed block's words red and struck through: %q", removed)
	}
	if !strings.Contains(ansi.Strip(added), "+") || drawnWith(t, added, "Brand", underlineSGR) || drawnWith(t, added, "Brand", greenSGR) {
		t.Errorf("expected the new block quiet, marked only by its +: %q", added)
	}
}

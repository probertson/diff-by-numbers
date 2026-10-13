package tui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// A Document Excerpt is drawn formatted: block by block, reflowed to the prose
// width, its Markdown markers muted and its spans styled, with the cursor
// moving a block at a time and every block but the cursor's dimmed. dbn says
// what the blocks are; the TUI only lays them out and styles them.

// The terminal's own palette, as SGR parameters.
const (
	faintSGR       = "2"
	boldSGR        = "1"
	italicSGR      = "3"
	greenSGR       = "32"
	blueSGR        = "34"
	cyanSGR        = "36"
	yellowSGR      = "33"
	brightBlackSGR = "90"
)

// paramsBefore is the SGR parameters in force where text first appears in row:
// those of the last escape sequence before it, which is the style it is drawn in.
// A 24-bit or 256-colour parameter is kept whole ("38;2;r;g;b"), so its parts
// are never mistaken for attributes of their own.
func paramsBefore(t *testing.T, row, text string) []string {
	t.Helper()
	at := strings.Index(row, text)
	if at < 0 {
		t.Fatalf("expected %q in %q", text, row)
	}
	escape := strings.LastIndex(row[:at], "\x1b[")
	if escape < 0 {
		return nil
	}
	end := strings.IndexByte(row[escape:at], 'm')
	if end < 0 {
		return nil
	}
	parts := strings.Split(row[escape+2:escape+end], ";")
	var params []string
	for i := 0; i < len(parts); i++ {
		n := 1
		if (parts[i] == "38" || parts[i] == "48") && i+1 < len(parts) {
			n = map[string]int{"2": 5, "5": 3}[parts[i+1]]
		}
		n = max(1, min(n, len(parts)-i))
		params = append(params, strings.Join(parts[i:i+n], ";"))
		i += n - 1
	}
	return params
}

func drawnWith(t *testing.T, row, text string, params ...string) bool {
	t.Helper()
	in := paramsBefore(t, row, text)
	for _, p := range params {
		if !slices.Contains(in, p) {
			return false
		}
	}
	return true
}

// docBlock is one block of a Document under test: its kind and its source lines.
type docBlock struct {
	kind  string
	level int
	text  []string
}

func heading(level int, text string) docBlock {
	return docBlock{kind: "heading", level: level, text: []string{strings.Repeat("#", level) + " " + text}}
}

func para(lines ...string) docBlock { return docBlock{kind: "paragraph", text: lines} }

func item(lines ...string) docBlock { return docBlock{kind: "list-item", text: lines} }

// documentExcerpt lays the blocks out as a new Document from line 1, a blank
// line between each, as dbn's Step view gives one: every row naming its block.
func documentExcerpt(file string, blocks ...docBlock) daemon.ExcerptWire {
	excerpt := daemon.ExcerptWire{File: file, Side: "new", FirstLine: 1}
	n := 1
	for bi, b := range blocks {
		if bi > 0 {
			excerpt.Lines = append(excerpt.Lines, daemon.LineWire{Number: n, Side: "new", Changed: true})
			n++
		}
		first := n
		for _, text := range b.text {
			excerpt.Lines = append(excerpt.Lines, daemon.LineWire{Number: n, Text: text, Side: "new", Changed: true, Block: bi + 1})
			n++
		}
		excerpt.Blocks = append(excerpt.Blocks, daemon.BlockWire{
			Kind: b.kind, Level: b.level, Side: "new", FirstLine: first, LastLine: n - 1, Treatment: "new",
		})
	}
	excerpt.LastLine = n - 1
	return excerpt
}

func documentStep(excerpts ...daemon.ExcerptWire) *daemon.StepWire {
	return &daemon.StepWire{Number: 1, Name: "Retry policy", Explanation: "how retries work", Excerpts: excerpts}
}

// retries is a new Document of a heading, a hard-wrapped paragraph with every
// kind of inline span, and a list item: lines 1, 3-6 and 8.
func retries() daemon.ExcerptWire {
	return documentExcerpt("docs/retries.md",
		heading(1, "Retries"),
		para("A request is **retried** when", "the _transport_ fails, and", "`retry()` waits between", "attempts."),
		item("- the first attempt"),
	)
}

// drawnStep renders the Step pane as the model draws it, at the given width.
func drawnStep(m model) string {
	return renderStep(m.view.Step, m.cursor, m.commentedLines(), m.ackComments(), m.width, m.bodyHeight(), false, false, m.proseWidth)
}

// textColumn is where a Document row's text begins: past the caret, the sign, the
// Comment mark and the line range.
func textColumn(row, firstWord string) string {
	plain := ansi.Strip(row)
	return plain[strings.Index(plain, firstWord):]
}

func TestAHardWrappedParagraphIsReflowedToTheProseWidth(t *testing.T) {
	var source []string
	for i := range 12 {
		source = append(source, fmt.Sprintf("w%da w%db w%dc w%dd", i, i, i, i))
	}
	m := stepModel(t, documentStep(documentExcerpt("docs/retries.md", para(source...))))
	m.width, m.proseWidth = 200, 40

	out := drawnStep(m)

	first := lineWith(t, out, "w0a")
	if !strings.Contains(first, "w1a") {
		t.Errorf("expected the paragraph's source lines joined into one flow:\n%s", out)
	}
	for _, row := range strings.Split(ansi.Strip(out), "\n") {
		word := proseWord.FindString(row)
		if word == "" {
			continue
		}
		if text := textColumn(row, word); ansi.StringWidth(text) > 40 {
			t.Errorf("expected every row of prose within 40 cells, got %d: %q", ansi.StringWidth(text), text)
		}
	}
}

// proseWord is a word of the reflowed paragraph.
var proseWord = regexp.MustCompile(`w\d+[abcd]`)

func TestMarkersAreMutedAndSpansStyled(t *testing.T) {
	inColour(t)
	m := stepModel(t, documentStep(retries()))
	m.cursor.move(1) // onto the paragraph, so it is drawn bright

	out := drawnStep(m)

	paragraph := lineWith(t, out, "request")
	if !drawnWith(t, paragraph, "**", brightBlackSGR) || !drawnWith(t, paragraph, "_", brightBlackSGR) || !drawnWith(t, paragraph, "`", brightBlackSGR) {
		t.Errorf("expected the markers drawn in bright black: %q", paragraph)
	}
	if !drawnWith(t, paragraph, "retried", boldSGR) {
		t.Errorf("expected the bold span bold: %q", paragraph)
	}
	if !drawnWith(t, paragraph, "transport", italicSGR) {
		t.Errorf("expected the italic span italic: %q", paragraph)
	}
	if !drawnWith(t, paragraph, "retry()", yellowSGR) {
		t.Errorf("expected the code span styled: %q", paragraph)
	}
	if !drawnWith(t, lineWith(t, out, "first"), "-", brightBlackSGR) {
		t.Errorf("expected the list bullet muted:\n%s", out)
	}
	if !drawnWith(t, lineWith(t, out, "Retries"), "#", brightBlackSGR) {
		t.Errorf("expected the heading's # muted:\n%s", out)
	}
}

func TestAHeadingKeepsItsAccentWhenDimmed(t *testing.T) {
	inColour(t)
	m := stepModel(t, documentStep(retries()))
	m.cursor.move(1) // away from the heading

	heading := lineWith(t, drawnStep(m), "Retries")

	if !drawnWith(t, heading, "Retries", cyanSGR, faintSGR) {
		t.Errorf("expected the dimmed heading still in its accent: %q", heading)
	}
}

func TestTheCursorMovesBlockByBlockAndDimsEveryOtherBlock(t *testing.T) {
	inColour(t)
	m := stepModel(t, documentStep(retries()))

	m = press(m, "down")
	out := drawnStep(m)

	paragraph, heading, listItem := lineWith(t, out, "request"), lineWith(t, out, "Retries"), lineWith(t, out, "first")
	if !strings.Contains(ansi.Strip(paragraph), "▸") {
		t.Errorf("expected one press to move the cursor from the heading to the paragraph:\n%s", out)
	}
	if drawnWith(t, paragraph, "request", faintSGR) {
		t.Errorf("expected the cursor's block bright: %q", paragraph)
	}
	if !drawnWith(t, listItem, "first", faintSGR) || !drawnWith(t, heading, "Retries", faintSGR) {
		t.Errorf("expected every other block dimmed:\n%s", out)
	}
	if !drawnWith(t, paragraph, "+", blueSGR) || !drawnWith(t, listItem, "+", greenSGR) {
		t.Errorf("expected the cursor's gutter blue and the others green:\n%s", out)
	}
}

func TestEveryRowOfANewBlockIsSignedWithNoTint(t *testing.T) {
	inColour(t)
	m := stepModel(t, documentStep(retries()))
	m.width = 40 // narrow enough that the paragraph takes several rows

	out := drawnStep(m)

	var rows []string
	for _, row := range strings.Split(out, "\n") {
		if strings.Contains(row, "request") || strings.Contains(row, "fails") || strings.Contains(row, "attempts.") {
			rows = append(rows, row)
		}
	}
	if len(rows) < 2 {
		t.Fatalf("expected the paragraph over several rows:\n%s", out)
	}
	if !strings.Contains(ansi.Strip(rows[0]), "3–6") {
		t.Errorf("expected the block's first row to carry its line range: %q", rows[0])
	}
	for _, row := range rows {
		if !strings.Contains(ansi.Strip(row), "+") {
			t.Errorf("expected every row of the new block signed +: %q", row)
		}
		if strings.Contains(row, "48;") {
			t.Errorf("expected no tint on new text: %q", row)
		}
	}
}

func TestAMixedStepMovesByBlockInItsDocumentAndByLineInItsCode(t *testing.T) {
	code := daemon.ExcerptWire{File: "retry.go", Side: "new", FirstLine: 1, LastLine: 2, Lines: []daemon.LineWire{
		{Number: 1, Text: "func retry() {", Side: "new", Changed: true},
		{Number: 2, Text: "}", Side: "new", Changed: true},
	}}
	m := stepModel(t, documentStep(retries(), code))

	var visited []string
	for range 5 {
		visited = append(visited, ansi.Strip(lineWith(t, drawnStep(m), "▸")))
		m = press(m, "down")
	}

	for i, want := range []string{"# Retries", "A request", "- the first attempt", "func retry() {", "2 │ }"} {
		if !strings.Contains(visited[i], want) {
			t.Errorf("stop %d: expected the cursor on %q, got %q", i+1, want, visited[i])
		}
	}
}

func TestACommentInADocumentIsMarkedOnceOnItsBlocksFirstRow(t *testing.T) {
	inColour(t)
	step := documentStep(retries())
	m := stepModel(t, step)
	m.view.Comments = []daemon.CommentWire{{ID: 1, Step: 1, File: "docs/retries.md", Segments: []daemon.SegmentWire{
		{Side: "new", FirstLine: 5, LastLine: 5}, // inside the paragraph, not its first line
	}}}
	m.width = 40

	out := drawnStep(m)

	if strings.Count(out, "●") != 1 {
		t.Fatalf("expected a single Comment mark:\n%s", out)
	}
	marked := lineWith(t, out, "●")
	if !strings.Contains(marked, "3–6") {
		t.Errorf("expected the mark on the paragraph's first row: %q", marked)
	}
	if !drawnWith(t, marked, "●", commentSGR) || drawnWith(t, marked, "●", faintSGR) {
		t.Errorf("expected the mark in the Comment colour at full brightness on a dimmed block: %q", marked)
	}
	if strings.Contains(out, "✎") {
		t.Errorf("expected a Document to use the gutter mark, not the code's: \n%s", out)
	}
}

func TestCodeKeepsItsCommentStyling(t *testing.T) {
	code := daemon.ExcerptWire{File: "retry.go", Side: "new", FirstLine: 1, LastLine: 1, Lines: []daemon.LineWire{
		{Number: 1, Text: "func retry() {}", Side: "new", Changed: true},
	}}
	m := stepModel(t, documentStep(retries(), code))
	m.view.Comments = []daemon.CommentWire{{ID: 1, Step: 1, File: "retry.go", Segments: []daemon.SegmentWire{
		{Side: "new", FirstLine: 1, LastLine: 1},
	}}}

	out := drawnStep(m)

	if !strings.Contains(lineWith(t, out, "func retry"), "✎") || strings.Contains(out, "●") {
		t.Errorf("expected code's Comment marked as it always was:\n%s", out)
	}
}

func TestSelectingABlockSelectsItsSourceLines(t *testing.T) {
	m := stepModel(t, documentStep(retries()))
	m = press(m, "down")

	run, ok := m.cursor.selection()

	if !ok || run.start != (rowRef{"new", 3}) || run.end != (rowRef{"new", 6}) || run.rows != 4 {
		t.Errorf("expected the paragraph's lines 3-6 selected, got %+v (%v)", run, ok)
	}
}

// tallDocument is a paragraph far taller than a short pane, then a heading.
func tallDocument() *daemon.StepWire {
	words := []string{"opening"}
	for range 300 {
		words = append(words, "prose")
	}
	words = append(words, "closing")
	var source []string
	for len(words) > 0 {
		n := min(10, len(words))
		source = append(source, strings.Join(words[:n], " "))
		words = words[n:]
	}
	return documentStep(documentExcerpt("docs/retries.md", para(source...), heading(2, "Afterwards")))
}

func TestDownPagesThroughABlockTallerThanThePaneBeforeMovingOn(t *testing.T) {
	m := stepModel(t, tallDocument())
	m.height = 20

	// Every page drawn while the cursor stays on the tall paragraph.
	pages := []string{drawnStep(m)}
	for len(pages) < 20 {
		m = press(m, "down")
		if m.cursor.cursor != 0 {
			break
		}
		pages = append(pages, drawnStep(m))
	}

	if !strings.Contains(pages[0], "opening") || strings.Contains(pages[0], "closing") {
		t.Fatalf("expected the tall block to open at its top:\n%s", pages[0])
	}
	if len(pages) < 3 {
		t.Fatalf("expected ↓ to page through the block before moving on, moved after %d presses", len(pages))
	}
	if !strings.Contains(pages[1], "prose") || strings.Contains(pages[1], "opening") {
		t.Errorf("expected the first ↓ to show the next page of the block:\n%s", pages[1])
	}
	if end := pages[len(pages)-1]; !strings.Contains(end, "closing") {
		t.Errorf("expected the block's end on screen before the cursor moved on:\n%s", end)
	}
	if m.cursor.cursor != 1 {
		t.Errorf("expected ↓ at the block's end to move to the next block")
	}
}

func TestUpPagesBackThroughATallBlockFromItsEnd(t *testing.T) {
	m := stepModel(t, tallDocument())
	m.height = 20
	m.cursor.move(1) // onto the heading after the tall paragraph

	m = press(m, "up")
	atEnd := drawnStep(m)
	m = press(m, "up")
	pagedBack := drawnStep(m)

	if !strings.Contains(atEnd, "closing") || strings.Contains(atEnd, "opening") {
		t.Errorf("expected ↑ into a tall block to show its end:\n%s", atEnd)
	}
	if strings.Contains(pagedBack, "closing") {
		t.Errorf("expected the next ↑ to page back through the block:\n%s", pagedBack)
	}
	if m.cursor.cursor != 0 {
		t.Errorf("expected the cursor to stay on the tall block while paging")
	}
}

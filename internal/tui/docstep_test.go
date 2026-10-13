package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	redSGR         = "31"
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
	return renderStep(m.paneInputs())
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

// rowsEndingWith is every row of out whose text, its styling stripped, ends in
// suffix.
func rowsEndingWith(out, suffix string) []string {
	var rows []string
	for _, row := range strings.Split(out, "\n") {
		if strings.HasSuffix(strings.TrimRight(ansi.Strip(row), " "), suffix) {
			rows = append(rows, row)
		}
	}
	return rows
}

func TestFrontmatterAndCodeFencesAreMutedAndWhatTheyHoldIsNot(t *testing.T) {
	inColour(t)
	m := stepModel(t, documentStep(documentExcerpt("docs/retries.md",
		docBlock{kind: "frontmatter", text: []string{"---", "title: Retries", "---"}},
		docBlock{kind: "code", text: []string{"```go", "retry()", "```"}},
	)))

	out := drawnStep(m)

	for fence, want := range map[string]int{"---": 2, "```go": 1, "```": 1} {
		rows := rowsEndingWith(out, fence)
		if len(rows) != want {
			t.Fatalf("expected %d rows ending in %q, got %d:\n%s", want, fence, len(rows), out)
		}
		for _, row := range rows {
			if !drawnWith(t, row, fence, brightBlackSGR) {
				t.Errorf("expected the fence %q drawn in bright black: %q", fence, row)
			}
		}
	}
	for _, text := range []string{"title: Retries", "retry()"} {
		if drawnWith(t, lineWith(t, out, text), text, brightBlackSGR) {
			t.Errorf("expected %q, inside its fences, not muted:\n%s", text, out)
		}
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

// anchorServer stands in for the daemon's /anchor: it answers every request
// with the same Anchor text and records what each one asked for.
func anchorServer(t *testing.T, m *model, text string) *[]map[string]any {
	t.Helper()
	var asked []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/anchor") {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		asked = append(asked, body)
		io.WriteString(w, text)
	}))
	t.Cleanup(server.Close)
	m.client.base = server.URL
	m.note = newNote(m.width)
	return &asked
}

// twoParagraphs is a new Document of a heading on line 1, paragraphs on lines
// 3-4 and 6-7, and a list item on 9.
func twoParagraphs() *daemon.StepWire {
	return documentStep(documentExcerpt("docs/retries.md",
		heading(1, "Retries"),
		para("A request is retried", "when the transport fails."),
		para("It waits between", "attempts."),
		item("- the first attempt"),
	))
}

// selectingTwoParagraphs selects the Document's two paragraphs, block by block.
func selectingTwoParagraphs(m model) model {
	return press(press(press(m, "down"), "v"), "down")
}

func TestCommentingOnTwoParagraphsAsksForThoseBlocksAnchor(t *testing.T) {
	m := stepModel(t, twoParagraphs())
	asked := anchorServer(t, &m, "Re: docs/retries.md — after 3-7")
	m = selectingTwoParagraphs(m)

	m = press(m, "c")

	want := []map[string]any{{
		"excerpt_index": float64(0), "blocks": true,
		"start": map[string]any{"side": "new", "line": float64(3)},
		"end":   map[string]any{"side": "new", "line": float64(6)},
	}}
	if !reflect.DeepEqual(*asked, want) {
		t.Errorf("expected the Anchor asked for by the blocks at each end\n%v\ngot\n%v", want, *asked)
	}
	if m.mode != modeNote || m.pendingCode != "Re: docs/retries.md — after 3-7" {
		t.Errorf("expected the Comment editor open on the Anchor, got mode %v and %q", m.mode, m.pendingCode)
	}
}

func TestYCopiesTheSameAnchorACommentWouldCarry(t *testing.T) {
	var copied []string
	was := copyText
	copyText = func(text string) bool { copied = append(copied, text); return true }
	t.Cleanup(func() { copyText = was })
	m := stepModel(t, twoParagraphs())
	asked := anchorServer(t, &m, "Re: docs/retries.md — after 3-7")
	commented := press(selectingTwoParagraphs(m), "c")

	m = press(selectingTwoParagraphs(m), "y")

	if len(*asked) != 2 || !reflect.DeepEqual((*asked)[0], (*asked)[1]) {
		t.Errorf("expected y to ask for the same Anchor as c, got %v", *asked)
	}
	if !slices.Equal(copied, []string{commented.pendingCode}) {
		t.Errorf("expected the Anchor text copied, got %q", copied)
	}
	if m.status != "copied Anchor for 2 blocks — paste it into your agent chat" {
		t.Errorf("expected the status to count the blocks selected, got %q", m.status)
	}
}

func TestTheSourceViewSelectsAndAnchorsLinesAsBefore(t *testing.T) {
	m := press(stepModel(t, twoParagraphs()), "s")
	asked := anchorServer(t, &m, "Re: docs/retries.md — after 3-4")
	m = press(press(press(press(m, "down"), "down"), "v"), "down") // lines 3 and 4

	press(m, "c")

	want := []map[string]any{{
		"excerpt_index": float64(0),
		"start":         map[string]any{"side": "new", "line": float64(3)},
		"end":           map[string]any{"side": "new", "line": float64(4)},
	}}
	if !reflect.DeepEqual(*asked, want) {
		t.Errorf("expected the source view to anchor the lines selected\n%v\ngot\n%v", want, *asked)
	}
}

func TestEOnABlockEditsACommentOnAnyOfItsLines(t *testing.T) {
	m := press(stepModel(t, twoParagraphs()), "down")
	m.note = newNote(m.width)
	m.view.Comments = []daemon.CommentWire{{ID: 4, Step: 1, File: "docs/retries.md", Note: "say how long",
		Segments: []daemon.SegmentWire{{Side: "new", FirstLine: 4, LastLine: 4}}}}

	m = press(m, "e")

	if m.mode != modeNote || m.editingID != 4 {
		t.Errorf("expected e to open the Comment on the paragraph's second line, got mode %v, editing %d (%s)", m.mode, m.editingID, m.status)
	}
}

// widenedParagraph is a six-line paragraph of which the Excerpt names only the
// middle two lines; dbn widened it to the rest, which it marks as context. Each
// source line is exactly eleven cells, so at a prose width of eleven each is a
// row of its own.
func widenedParagraph() daemon.ExcerptWire {
	excerpt := documentExcerpt("docs/retries.md", para(
		"ctx1a ctx1b", "ctx2a ctx2b", "new3a new3b", "new4a new4b", "ctx5a ctx5b", "ctx6a ctx6b",
	))
	for i := range excerpt.Lines {
		if strings.HasPrefix(excerpt.Lines[i].Text, "ctx") {
			excerpt.Lines[i].Changed, excerpt.Lines[i].Widened = false, true
		}
	}
	return excerpt
}

// signOf is the gutter sign drawn before a Document row's text.
func signOf(row, firstWord string) string {
	plain := ansi.Strip(row)
	return strings.TrimSpace(strings.Trim(plain[:strings.Index(plain, firstWord)], "▸ 0123456789–●"))
}

func TestAWidenedParagraphsExtraLinesAreDimmedAndUnsignedOnTheCursorsBlock(t *testing.T) {
	inColour(t)
	m := stepModel(t, documentStep(widenedParagraph()))
	m.proseWidth = 11

	out := drawnStep(m)

	for _, word := range []string{"ctx1a", "ctx2a", "ctx5a", "ctx6a"} {
		row := lineWith(t, out, word)
		if !drawnWith(t, row, word, faintSGR) {
			t.Errorf("expected the widened line dimmed though its block is the cursor's: %q", row)
		}
		if sign := signOf(row, word); sign != "" {
			t.Errorf("expected the widened line unsigned, got %q: %q", sign, row)
		}
	}
	for _, word := range []string{"new3a", "new4a"} {
		row := lineWith(t, out, word)
		if drawnWith(t, row, word, faintSGR) {
			t.Errorf("expected the Excerpt's own line bright: %q", row)
		}
		if sign := signOf(row, word); sign != "+" {
			t.Errorf("expected the Excerpt's own line signed +, got %q: %q", sign, row)
		}
	}
}

// tableRowExcerpt is one row of a table, line 7, as dbn gives an Excerpt that
// cuts into the table: with the header row and divider, lines 3-4, widened in
// as context. The row is far wider than the pane.
func tableRowExcerpt() daemon.ExcerptWire {
	wide := "| 3 | " + strings.Repeat("backoff ", 15) + "| lastcell |"
	return daemon.ExcerptWire{File: "docs/retries.md", Side: "new", FirstLine: 7, LastLine: 7,
		Lines: []daemon.LineWire{
			{Number: 3, Text: "| Attempt | Delay | Note |", Side: "new", Block: 1, Widened: true},
			{Number: 4, Text: "| ------- | ----- | ---- |", Side: "new", Block: 1, Widened: true},
			{Number: 7, Text: wide, Side: "new", Changed: true, Block: 2},
		},
		Blocks: []daemon.BlockWire{
			{Kind: "table-row", Side: "new", FirstLine: 3, LastLine: 4, Treatment: "context"},
			{Kind: "table-row", Side: "new", FirstLine: 7, LastLine: 7, Treatment: "new"},
		},
	}
}

func TestATableRowIsDrawnAsItsSourceWrappedWhenWideWithItsPipesMuted(t *testing.T) {
	inColour(t)
	m := stepModel(t, documentStep(tableRowExcerpt()))
	m.width = 60
	m = press(m, "down") // onto the row

	out := drawnStep(m)

	if strings.Contains(ansi.Strip(out), "…") {
		t.Errorf("expected the wide row wrapped, not clipped:\n%s", ansi.Strip(out))
	}
	last := lineWith(t, out, "lastcell")
	if strings.Contains(ansi.Strip(last), "| 3 |") {
		t.Errorf("expected the row's end on a row of its own:\n%s", ansi.Strip(out))
	}
	if !drawnWith(t, last, "|", brightBlackSGR) {
		t.Errorf("expected the pipes on a wrapped row muted: %q", last)
	}
	if !drawnWith(t, lineWith(t, out, " 3 "), "|", brightBlackSGR) {
		t.Errorf("expected the row's pipes muted:\n%s", out)
	}
}

func TestATableRowsHeaderIsDrawnAboveItAsContext(t *testing.T) {
	inColour(t)
	m := stepModel(t, documentStep(tableRowExcerpt()))
	m.width = 60
	m = press(m, "down") // onto the row

	out := drawnStep(m)

	header, divider := lineWith(t, out, "Attempt"), lineWith(t, out, "-------")
	if plain := ansi.Strip(out); strings.Index(plain, "Attempt") > strings.Index(plain, "| 3 |") {
		t.Errorf("expected the header row above the row:\n%s", ansi.Strip(out))
	}
	if !drawnWith(t, header, "Attempt", faintSGR) || signOf(header, "|") != "" || signOf(divider, "|") != "" {
		t.Errorf("expected the header row and divider dimmed and unsigned:\n%s", out)
	}
}

func TestProseThatIsNotMarkdownIsDrawnWithoutMarkdownStyling(t *testing.T) {
	inColour(t)
	excerpt := documentExcerpt("docs/spec.txt",
		para("# not a heading, and", "**not bold** either"),
		para("- not a list item"),
	)
	excerpt.Plain = true
	m := stepModel(t, documentStep(excerpt))

	out := drawnStep(m)

	first := lineWith(t, out, "not a heading")
	if !strings.Contains(first, "**not bold**") || drawnWith(t, first, "#", brightBlackSGR) || drawnWith(t, first, "not bold", boldSGR) {
		t.Errorf("expected the paragraph reflowed with nothing taken for Markdown: %q", first)
	}
	if drawnWith(t, lineWith(t, out, "not a list"), "-", brightBlackSGR) {
		t.Errorf("expected no list bullet in plain prose:\n%s", out)
	}
	m = press(m, "down")
	if !strings.Contains(ansi.Strip(lineWith(t, drawnStep(m), "▸")), "not a list item") {
		t.Errorf("expected the cursor to move by paragraph:\n%s", ansi.Strip(drawnStep(m)))
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

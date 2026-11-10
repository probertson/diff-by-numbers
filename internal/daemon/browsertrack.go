package daemon

import (
	"html/template"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/present"
	"github.com/probertson/diff-by-numbers/internal/review"
)

// An edited or removed block of a Document is drawn as the TUI draws it: as
// track changes over its source, the words an edit struck struck through and
// the ones it added inserted, with its Markdown markers shown, muted, rather
// than rendered. Rendering an edit fully is left for later (#184).

// trackedLine is one source line of an edited or removed block: its block's
// kind, and its text as runs the edit kept, struck and added.
type trackedLine struct {
	kind string
	runs []RunWire
}

// trackedRow is a row of an edited or removed block, on the block's own side,
// as the page draws it: an edited row as its runs, a removed row struck whole,
// and a row the Excerpt was widened by as it stands.
func trackedRow(line LineWire, block BlockWire, commented bool) documentRow {
	runs := line.Runs
	switch {
	case block.Treatment == string(review.TreatmentRemoved):
		runs = []RunWire{{Text: line.Text, Change: string(review.WordsRemoved)}}
	case len(runs) == 0:
		runs = []RunWire{{Text: line.Text, Change: string(review.WordsKept)}}
	}
	return documentRow{
		text:      line.Text,
		block:     line.Block,
		commented: commented,
		treatment: block.Treatment,
		widened:   line.Widened,
		tracked:   &trackedLine{kind: block.Kind, runs: runs},
	}
}

// renderTracked draws one edited or removed block, its rows given in order,
// as track changes, followed by the notes of the Comments on it. Prose that is
// not Markdown (plain) has no markers.
func renderTracked(rows []documentRow, plain bool) template.HTML {
	return drawTracked(rows, plain) + notesOf(rows)
}

// drawTracked draws one edited or removed block, its rows given in order, as
// track changes.
func drawTracked(rows []documentRow, plain bool) template.HTML {
	first := rows[0]
	if first.diagram != nil {
		return renderDiagramChange(changedDiagram{
			sources: *first.diagram, treatment: first.treatment, block: first.block, place: first.place,
			commented: first.commented, cursor: first.cursor, edit: rows,
		})
	}
	var out strings.Builder
	out.WriteString(`<div class="tracked ` + template.HTMLEscapeString(first.tracked.kind))
	if flows(first.tracked.kind, plain) {
		out.WriteString(" flow")
	}
	if first.commented {
		out.WriteString(" commented")
	}
	if first.cursor {
		out.WriteString(" cursor")
	}
	out.WriteString(`" data-block="` + strconv.Itoa(first.block) + `" data-place="` +
		template.HTMLEscapeString(first.place) + `" data-treatment="` + template.HTMLEscapeString(first.treatment) + `">`)
	out.WriteString(string(trackedLines(rows, plain)))
	out.WriteString("</div>\n")
	return template.HTML(out.String())
}

// trackedLines draws an edited or removed block's rows as track changes, a
// line each, a line the Excerpt was widened by dimmed as context. The lines of
// a block that flows are marked where one ends in a hard break, the only place
// its flow breaks, and leave out the markers a block quote repeats on each.
func trackedLines(rows []documentRow, plain bool) template.HTML {
	return drawLines(rows, plain, false)
}

// trackedItemLines draws a list item's rows as track changes, as trackedLines
// does, for the item the list draws: its bullet or number is the list's, so the
// markers leading each line are left out.
func trackedItemLines(rows []documentRow) template.HTML {
	return drawLines(rows, false, true)
}

func drawLines(rows []documentRow, plain, item bool) template.HTML {
	var out strings.Builder
	var st present.Spans
	kind := rows[0].tracked.kind
	flowing := flows(kind, plain)
	for i, row := range rows {
		out.WriteString(`<div class="line`)
		if row.widened {
			out.WriteString(" widened")
		}
		if flowing && i < len(rows)-1 && endsInHardBreak(row.tracked.runs) {
			out.WriteString(" hard-break")
		}
		out.WriteString(`">`)
		line := blockLine{kind: kind, first: i == 0, plain: plain, continued: item || (flowing && i > 0)}
		line.write(&out, separated(row.tracked.runs), &st)
		out.WriteString("</div>")
	}
	return template.HTML(out.String())
}

// flows reports whether a block's lines flow as one paragraph, as Markdown
// reflows them (and the TUI draws them): a paragraph's or a list item's, and
// any block of prose that is not Markdown.
func flows(kind string, plain bool) bool {
	return plain || kind == string(review.BlockParagraph) || kind == string(review.BlockListItem)
}

// endsInHardBreak reports whether a line, as the file has it now, ends in a
// Markdown hard break: two spaces, or a backslash.
func endsInHardBreak(runs []RunWire) bool {
	var current strings.Builder
	for _, run := range runs {
		if run.Change != string(review.WordsRemoved) {
			current.WriteString(run.Text)
		}
	}
	text := current.String()
	return strings.HasSuffix(text, "  ") || strings.HasSuffix(text, "\\")
}

// separated sets struck runs apart from the text either side with a space:
// dbn gives struck words without the spaces around them, which belong to the
// text the edit kept.
func separated(runs []RunWire) []RunWire {
	return present.Separated(runs,
		func(run RunWire) bool { return run.Change == string(review.WordsRemoved) },
		func(run RunWire) string { return run.Text },
		RunWire{Text: " ", Change: string(review.WordsKept)})
}

// rowCells is the cells of an edited or removed table row, drawn from its
// rows: a body row's one line, or a header's first, when the edit left its
// divider as it was.
func rowCells(rows []documentRow, columns int) ([][]RunWire, bool) {
	for _, row := range rows[1:] {
		for _, run := range row.tracked.runs {
			if run.Change != string(review.WordsKept) {
				return nil, false
			}
		}
	}
	return trackedCells(rows[0].tracked.runs, columns)
}

// trackedCells cuts a table row's runs into its cells at the pipes between
// them, leaving out the pipes and the row's leading and trailing ones. It
// reports false when the edit added, struck or moved a pipe (a row struck or
// added throughout aside), whose cells may then not be its table's columns,
// and when the row has other than columns cells.
func trackedCells(runs []RunWire, columns int) ([][]RunWire, bool) {
	throughout := true
	for _, run := range runs {
		throughout = throughout && run.Change == runs[0].Change
	}
	now := pipesOf(runs, string(review.WordsRemoved))
	was := pipesOf(runs, string(review.WordsAdded))
	var cells [][]RunWire
	var cell []RunWire
	add := func(run RunWire, text string) {
		if text != "" {
			cell = append(cell, RunWire{Text: text, Change: run.Change})
		}
	}
	for i, run := range runs {
		cuts := now[i]
		switch {
		case run.Change == string(review.WordsRemoved):
			cuts = was[i]
		case run.Change == string(review.WordsKept) && !slices.Equal(now[i], was[i]):
			return nil, false
		}
		if len(cuts) > 0 && run.Change != string(review.WordsKept) && !throughout {
			return nil, false
		}
		at := 0
		for _, cut := range cuts {
			add(run, run.Text[at:cut])
			cells, cell = append(cells, cell), nil
			at = cut + 1
		}
		add(run, run.Text[at:])
	}
	cells = append(cells, cell)
	if len(cells) > 1 && blankRuns(cells[0]) {
		cells = cells[1:]
	}
	if len(cells) > 1 && blankRuns(cells[len(cells)-1]) {
		cells = cells[:len(cells)-1]
	}
	if len(cells) != columns {
		return nil, false
	}
	for i := range cells {
		cells[i] = trimmedRuns(cells[i])
	}
	return cells, true
}

// pipesOf is, for each run, where in it the pipes that divide a table row's
// cells are, read from the row's text without the runs whose change is
// leftOut (none for those): a pipe escaped is not one, and one in a code span
// still is, as GFM reads a row.
func pipesOf(runs []RunWire, leftOut string) [][]int {
	var text strings.Builder
	for _, run := range runs {
		if run.Change != leftOut {
			text.WriteString(run.Text)
		}
	}
	var pipes []int
	at := 0
	for _, token := range present.Inline(text.String(), &present.Spans{}, true) {
		if token.Kind == present.TokenMarker && token.Text == "|" {
			pipes = append(pipes, at)
		}
		at += len(token.Text)
	}
	out := make([][]int, len(runs))
	at = 0
	for i, run := range runs {
		if run.Change == leftOut {
			continue
		}
		for _, pipe := range pipes {
			if pipe >= at && pipe < at+len(run.Text) {
				out[i] = append(out[i], pipe-at)
			}
		}
		at += len(run.Text)
	}
	return out
}

// blankRuns reports whether runs hold nothing but white space.
func blankRuns(runs []RunWire) bool {
	for _, run := range runs {
		if strings.TrimSpace(run.Text) != "" {
			return false
		}
	}
	return true
}

// trimmedRuns is a cell's runs without the white space padding it.
func trimmedRuns(runs []RunWire) []RunWire {
	for len(runs) > 0 && strings.TrimLeft(runs[0].Text, " \t") == "" {
		runs = runs[1:]
	}
	for len(runs) > 0 && strings.TrimRight(runs[len(runs)-1].Text, " \t") == "" {
		runs = runs[:len(runs)-1]
	}
	if len(runs) == 0 {
		return nil
	}
	runs = slices.Clone(runs)
	runs[0].Text = strings.TrimLeft(runs[0].Text, " \t")
	runs[len(runs)-1].Text = strings.TrimRight(runs[len(runs)-1].Text, " \t")
	return runs
}

// trackedCell draws one cell of an edited or removed table row as track
// changes, its inline markers muted.
func trackedCell(runs []RunWire) template.HTML {
	var out strings.Builder
	blockLine{kind: string(review.BlockTableRow), cell: true}.write(&out, separated(runs), &present.Spans{})
	return template.HTML(out.String())
}

// blockLine is how one line of a block's source is read for its markers: the
// block's kind, whether it is the block's first line, and whether the
// Document is Markdown at all. A continued line of a block that flows leaves
// out the markers leading it, which would otherwise stand mid-paragraph. A
// table row's cell, drawn on its own, has no markers leading it.
type blockLine struct {
	kind      string
	first     bool
	plain     bool
	continued bool
	cell      bool
}

var (
	quotePrefix = regexp.MustCompile(`^\s{0,3}((?:>\s?)+)`)
	listMarker  = regexp.MustCompile(`^\s*([-*+]|\d{1,9}[.)])(\s+|$)`)
	headingHash = regexp.MustCompile(`^\s{0,3}#{1,6}(\s+|$)`)
	setextLine  = regexp.MustCompile(`^\s{0,3}(=+|-+)\s*$`)
	fenceLine   = regexp.MustCompile("^\\s*(```|~~~)")
	tableDivide = regexp.MustCompile(`^[\s|:\-]+$`)
)

// leading is how many bytes at the start of a line are Markdown markers that
// only stand there: a quote's >, a list item's bullet, a heading's #s, and the
// whole of a line that is nothing but markers (a setext underline, a fence, a
// table's divider).
func (l blockLine) leading(text string) int {
	if l.plain || l.cell {
		return 0
	}
	quote := len(quotePrefix.FindString(text))
	rest := text[quote:]
	switch {
	case l.kind == string(review.BlockCode) && fenceLine.MatchString(text):
		return len(text)
	case l.kind == string(review.BlockTableRow) && tableDivide.MatchString(text) && strings.Contains(text, "-"):
		return len(text)
	case l.kind == string(review.BlockHeading) && !l.first && setextLine.MatchString(rest):
		return len(text)
	case l.kind == string(review.BlockHeading) && l.first:
		return quote + len(headingHash.FindString(rest))
	case l.kind == string(review.BlockListItem) && l.first:
		return quote + len(listMarker.FindString(rest))
	}
	return quote
}

// inline reports whether a line's text is read for inline markers: emphasis,
// code spans and a table's pipes. Code and frontmatter are not.
func (l blockLine) inline() bool {
	switch {
	case l.plain:
		return false
	case l.kind == string(review.BlockCode), l.kind == string(review.BlockFrontmatter), l.kind == string(review.BlockOther):
		return false
	}
	return true
}

// write draws a line's runs: what the edit struck in <del>, what it added in
// <ins>, what it kept as it stands, each with its markers muted. The markers
// are read from the line as the file has it now; struck text is read with a
// copy of the open spans, so a marker the edit struck opens nothing in the
// text it kept.
func (l blockLine) write(out *strings.Builder, runs []RunWire, st *present.Spans) {
	var current strings.Builder
	for _, run := range runs {
		if run.Change != string(review.WordsRemoved) {
			current.WriteString(run.Text)
		}
	}
	lead := l.leading(current.String())
	at := 0 // where the current text has got to
	for i, run := range runs {
		switch run.Change {
		case string(review.WordsRemoved):
			scratch := *st
			struckLead := 0
			if i == 0 {
				struckLead = l.leading(run.Text)
			}
			out.WriteString("<del>")
			l.writeText(out, run.Text, struckLead, &scratch)
			out.WriteString("</del>")
		case string(review.WordsAdded):
			out.WriteString("<ins>")
			l.writeText(out, run.Text, max(min(lead-at, len(run.Text)), 0), st)
			out.WriteString("</ins>")
			at += len(run.Text)
		default:
			l.writeText(out, run.Text, max(min(lead-at, len(run.Text)), 0), st)
			at += len(run.Text)
		}
	}
}

// writeText draws a run of text whose first lead bytes are markers that stand
// at the start of the line, and the rest read for inline markers.
func (l blockLine) writeText(out *strings.Builder, text string, lead int, st *present.Spans) {
	if lead > 0 && !l.continued {
		writeSpan(out, "md", text[:lead])
	}
	if !l.inline() {
		writeSpan(out, "", text[lead:])
		return
	}
	for _, f := range inlineFragments(text[lead:], st, l.kind == string(review.BlockTableRow)) {
		writeSpan(out, f.class, f.text)
	}
}

func writeSpan(out *strings.Builder, class, text string) {
	if text == "" {
		return
	}
	if class == "" {
		out.WriteString(template.HTMLEscapeString(text))
		return
	}
	out.WriteString(`<span class="` + class + `">` + template.HTMLEscapeString(text) + "</span>")
}

// fragment is a run of a line's text drawn one way: a marker ("md"), or text
// in whatever inline spans are open ("bold italic", "code", or none).
type fragment struct{ text, class string }

// inlineFragments splits text into its inline markers and the text between
// them, opening and closing spans as it goes, as the TUI reads them
// (present.Inline). An escape's backslash is a marker. In a table row, a pipe
// is a marker.
func inlineFragments(text string, st *present.Spans, pipes bool) []fragment {
	var out []fragment
	for _, token := range present.Inline(text, st, pipes) {
		class := "md"
		if token.Kind == present.TokenText {
			class = spansClass(token.Spans)
		}
		out = append(out, fragment{text: token.Text, class: class})
	}
	return out
}

// spansClass is the style text is drawn in with these spans open.
func spansClass(st present.Spans) string {
	if st.Code {
		return "code"
	}
	var classes []string
	if st.Bold {
		classes = append(classes, "bold")
	}
	if st.Italic {
		classes = append(classes, "italic")
	}
	return strings.Join(classes, " ")
}

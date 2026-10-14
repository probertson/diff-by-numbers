package daemon

import (
	"html/template"
	"regexp"
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
// as track changes. Prose that is not Markdown (plain) has no markers.
func renderTracked(rows []documentRow, plain bool) template.HTML {
	first := rows[0]
	if first.change != nil {
		return renderDiagramChange(rows)
	}
	var out strings.Builder
	out.WriteString(`<div class="tracked ` + template.HTMLEscapeString(first.tracked.kind))
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
// line each, a line the Excerpt was widened by dimmed as context.
func trackedLines(rows []documentRow, plain bool) template.HTML {
	var out strings.Builder
	var st present.Spans
	for i, row := range rows {
		out.WriteString(`<div class="line`)
		if row.widened {
			out.WriteString(" widened")
		}
		out.WriteString(`">`)
		line := blockLine{kind: rows[0].tracked.kind, first: i == 0, plain: plain}
		line.write(&out, separated(row.tracked.runs), &st)
		out.WriteString("</div>")
	}
	return template.HTML(out.String())
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

// blockLine is how one line of a block's source is read for its markers: the
// block's kind, whether it is the block's first line, and whether the
// Document is Markdown at all.
type blockLine struct {
	kind  string
	first bool
	plain bool
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
	if l.plain {
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
	if lead > 0 {
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

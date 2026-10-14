package daemon

import (
	"html/template"
	"regexp"
	"strconv"
	"strings"
	"unicode"

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
func trackedRow(line LineWire, block BlockWire, commented, cursor bool) documentRow {
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
		cursor:    cursor,
		treatment: block.Treatment,
		widened:   line.Widened,
		tracked:   &trackedLine{kind: block.Kind, runs: runs},
	}
}

// renderTracked draws one edited or removed block, its rows given in order,
// as track changes. Prose that is not Markdown (plain) has no markers.
func renderTracked(rows []documentRow, plain bool) template.HTML {
	first := rows[0]
	var out strings.Builder
	out.WriteString(`<div class="tracked ` + template.HTMLEscapeString(first.tracked.kind))
	if first.commented {
		out.WriteString(" commented")
	}
	if first.cursor {
		out.WriteString(" cursor")
	}
	out.WriteString(`" data-block="` + strconv.Itoa(first.block) + `" data-treatment="` +
		template.HTMLEscapeString(first.treatment) + `">`)
	out.WriteString(string(trackedLines(rows, plain)))
	out.WriteString("</div>\n")
	return template.HTML(out.String())
}

// trackedLines draws an edited or removed block's rows as track changes, a
// line each, a line the Excerpt was widened by dimmed as context.
func trackedLines(rows []documentRow, plain bool) template.HTML {
	var out strings.Builder
	var st markdownState
	for i, row := range rows {
		out.WriteString(`<div class="line`)
		if row.widened {
			out.WriteString(" widened")
		}
		out.WriteString(`">`)
		line := sourceLine{kind: rows[0].tracked.kind, first: i == 0, plain: plain}
		line.write(&out, separated(row.tracked.runs), &st)
		out.WriteString("</div>")
	}
	return template.HTML(out.String())
}

// separated sets struck runs apart from the text either side with a space:
// dbn gives struck words without the spaces around them, which belong to the
// text the edit kept.
func separated(runs []RunWire) []RunWire {
	var out []RunWire
	space := RunWire{Text: " ", Change: string(review.WordsKept)}
	for i, run := range runs {
		if run.Change != string(review.WordsRemoved) {
			out = append(out, run)
			continue
		}
		if i > 0 && !strings.HasSuffix(runs[i-1].Text, " ") {
			out = append(out, space)
		}
		out = append(out, run)
		if i+1 < len(runs) && !strings.HasPrefix(runs[i+1].Text, " ") {
			out = append(out, space)
		}
	}
	return out
}

// sourceLine is how one line of a block's source is read for its markers: the
// block's kind, whether it is the block's first line, and whether the
// Document is Markdown at all.
type sourceLine struct {
	kind  string
	first bool
	plain bool
}

// markdownState is which inline spans are open, carried from line to line
// through a block.
type markdownState struct{ bold, italic, code bool }

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
func (l sourceLine) leading(text string) int {
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
func (l sourceLine) inline() bool {
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
func (l sourceLine) write(out *strings.Builder, runs []RunWire, st *markdownState) {
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
func (l sourceLine) writeText(out *strings.Builder, text string, lead int, st *markdownState) {
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
// them, opening and closing spans as it goes, as the TUI reads them. Inside a
// code span nothing but the closing backtick is a marker. An underscore inside
// a word opens nothing, as in CommonMark. In a table row, a pipe is a marker.
func inlineFragments(text string, st *markdownState, pipes bool) []fragment {
	var out []fragment
	var run []rune
	flush := func() {
		if len(run) > 0 {
			out = append(out, fragment{text: string(run), class: st.class()})
			run = nil
		}
	}
	marker := func(text string) {
		flush()
		out = append(out, fragment{text: text, class: "md"})
	}
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		pair := i+1 < len(rs) && rs[i+1] == r
		switch {
		case r == '|' && pipes:
			marker("|")
		case r == '`':
			marker("`")
			st.code = !st.code
		case st.code:
			run = append(run, r)
		case r == '\\' && i+1 < len(rs):
			marker("\\")
			run = append(run, rs[i+1])
			i++
		case (r == '*' || r == '_') && pair && delimiterAt(rs, i, 2):
			marker(string([]rune{r, r}))
			st.bold = !st.bold
			i++
		case (r == '*' || r == '_') && delimiterAt(rs, i, 1):
			marker(string(r))
			st.italic = !st.italic
		default:
			run = append(run, r)
		}
	}
	flush()
	return out
}

// class is the style text is drawn in with these spans open.
func (st markdownState) class() string {
	if st.code {
		return "code"
	}
	var classes []string
	if st.bold {
		classes = append(classes, "bold")
	}
	if st.italic {
		classes = append(classes, "italic")
	}
	return strings.Join(classes, " ")
}

// delimiterAt reports whether a run of n emphasis characters at i opens or
// closes a span: a * always may, an _ only at the edge of a word.
func delimiterAt(rs []rune, i, n int) bool {
	if rs[i] == '*' {
		return true
	}
	before := i == 0 || !isWordRune(rs[i-1])
	after := i+n >= len(rs) || !isWordRune(rs[i+n])
	return before || after
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

package tui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// A Document Excerpt is drawn formatted: each block dbn names is laid out by its
// kind, reflowed where Markdown would reflow it, with its markers kept but muted
// and its spans styled. The cursor's block is bright and every other block dim,
// so the bright block works as a reading bookmark. The colours are the
// terminal's own palette, so they suit whatever theme it has.
var (
	docGreen  = lipgloss.ANSIColor(2)
	docYellow = lipgloss.ANSIColor(3)
	docBlue   = lipgloss.ANSIColor(4)
	docCyan   = lipgloss.ANSIColor(6)
	docMarker = lipgloss.ANSIColor(8) // bright black
)

// span is a run of a block's text drawn in one style: a Markdown marker, or
// text inside whatever spans are open where it stands.
type span struct {
	text                       string
	marker, bold, italic, code bool
	// context is text of a line dbn widened the Excerpt by, so that no block is
	// read in part: dim even on the cursor's block, and never signed.
	context bool
}

// asContext marks spans as context, or leaves them as they are.
func asContext(spans []span, context bool) []span {
	if context {
		for i := range spans {
			spans[i].context = true
		}
	}
	return spans
}

// widenedOnly reports whether a row is nothing but text dbn widened the Excerpt
// by, which is context and so is not signed.
func (r docRow) widenedOnly() bool {
	widened := false
	for _, s := range r {
		switch {
		case s.context:
			widened = true
		case strings.TrimSpace(s.text) != "":
			return false
		}
	}
	return widened
}

// blockSource is a block's source lines as its Excerpt shows them.
type blockSource struct {
	lines []string
	// widened is, for each line, whether dbn widened the Excerpt by it.
	widened []bool
	// plain says the Document is not Markdown, so nothing in it is a marker.
	plain bool
}

// docRow is one laid-out row of a block, before the gutter.
type docRow []span

func (r docRow) width() int {
	w := 0
	for _, s := range r {
		w += ansi.StringWidth(s.text)
	}
	return w
}

func (r docRow) plain() string {
	var b strings.Builder
	for _, s := range r {
		b.WriteString(s.text)
	}
	return b.String()
}

// inline is which inline spans are open, carried from word to word through a
// block.
type inline struct{ bold, italic, code bool }

// inlineSpans splits one word of Markdown into its markers and the text between
// them, opening and closing spans as it goes. Inside a code span nothing but
// the closing backtick is a marker. An underscore inside a word (snake_case)
// opens nothing, as in CommonMark.
func inlineSpans(word string, st *inline) []span {
	var out []span
	var run []rune
	flush := func() {
		if len(run) > 0 {
			out = append(out, span{text: string(run), bold: st.bold, italic: st.italic, code: st.code})
			run = nil
		}
	}
	// toggle draws a marker, closing the text before it in the spans that were
	// open, then opens or closes the span it marks.
	toggle := func(text string, open *bool) {
		flush()
		out = append(out, span{text: text, marker: true})
		*open = !*open
	}
	rs := []rune(word)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		pair := i+1 < len(rs) && rs[i+1] == r
		switch {
		case r == '`':
			toggle("`", &st.code)
		case st.code:
			run = append(run, r)
		case r == '\\' && i+1 < len(rs):
			run = append(run, rs[i+1])
			i++
		case (r == '*' || r == '_') && pair && delimiterAt(rs, i, 2):
			toggle(string([]rune{r, r}), &st.bold)
			i++
		case (r == '*' || r == '_') && delimiterAt(rs, i, 1):
			toggle(string(r), &st.italic)
		default:
			run = append(run, r)
		}
	}
	flush()
	return out
}

// delimiterAt reports whether a run of n emphasis characters at i opens or
// closes a span: a `*` always may, an `_` only at the edge of a word.
func delimiterAt(rs []rune, i, n int) bool {
	if rs[i] == '*' {
		return true
	}
	before := i == 0 || !isWordRune(rs[i-1])
	after := i+n >= len(rs) || !isWordRune(rs[i+n])
	return before || after
}

func isWordRune(r rune) bool {
	return r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r > 127
}

// fill lays words, each already split into spans, into rows no wider than
// width, a space between each. A word wider than the row has a row to itself.
func fill(words [][]span, width int) []docRow {
	var rows []docRow
	var row docRow
	used := 0
	for _, word := range words {
		w := docRow(word).width()
		if used > 0 && used+1+w > width {
			rows = append(rows, row)
			row, used = nil, 0
		}
		if used > 0 {
			row = append(row, span{text: " "})
			used++
		}
		row = append(row, word...)
		used += w
	}
	if used > 0 || len(rows) == 0 {
		rows = append(rows, row)
	}
	return rows
}

var (
	quotePrefix = regexp.MustCompile(`^\s{0,3}((?:>\s?)+)`)
	listMarker  = regexp.MustCompile(`^(\s*)([-*+]|\d{1,9}[.)])(\s+|$)`)
	fence       = regexp.MustCompile("^\\s*(```|~~~)")
	setextLine  = regexp.MustCompile(`^\s{0,3}(=+|-+)\s*$`)
	headingHash = regexp.MustCompile(`^\s{0,3}#{1,6}(\s+|$)`)
	tableDivide = regexp.MustCompile(`^[\s|:\-]+$`)
)

// layoutBlock lays a block's source lines out width cells wide. A paragraph or
// list item is reflowed by Markdown's rules, its lines joined with spaces and
// broken only where a line ends in a hard break. Everything else keeps its
// source lines, each wrapped only where it is too wide for the row. Prose that
// is not Markdown is reflowed with nothing in it taken for a marker.
func layoutBlock(kind string, src blockSource, width int) []docRow {
	width = max(width, 1)
	source := src.lines
	switch {
	case src.plain:
		return reflowPlain(src, width)
	case kind == "paragraph" || kind == "list-item":
		return reflow(src, width)
	case kind == "heading":
		var rows []docRow
		st := &inline{}
		for i, line := range source {
			words := lineWords(kind, line, st)
			for _, word := range words {
				asContext(word, src.widened[i])
			}
			rows = append(rows, fill(words, width)...)
		}
		return rows
	case kind == "table-row":
		var rows []docRow
		st := &inline{}
		for i, line := range source {
			rows = append(rows, wrapRow(asContext(tableRow(line, st), src.widened[i]), width)...)
		}
		return rows
	}
	// Fenced code, frontmatter and anything else are drawn as they stand, their
	// fences muted.
	var rows []docRow
	for i, line := range source {
		isFence := (kind == "code" && fence.MatchString(line)) ||
			(kind == "frontmatter" && (i == 0 || i == len(source)-1) && (strings.TrimSpace(line) == "---" || strings.TrimSpace(line) == "..."))
		for _, chunk := range wrapCode(expandedTabs(line), width, width, len(line)+1) {
			rows = append(rows, docRow{{text: chunk, marker: isFence, context: src.widened[i]}})
		}
	}
	return rows
}

// wrapRow breaks a row too wide for width as a line of code is broken, at the
// last space that fits, keeping each piece of it in its style.
func wrapRow(row docRow, width int) []docRow {
	text := []rune(row.plain())
	var out []docRow
	at := 0
	for _, chunk := range wrapCode(string(text), width, width, len(text)+1) {
		end := at + len([]rune(chunk))
		out = append(out, sliceRow(row, at, end))
		at = skipSpaces(text, end)
	}
	return out
}

// sliceRow is the part of a row from rune from to rune to of its text.
func sliceRow(row docRow, from, to int) docRow {
	var out docRow
	at := 0
	for _, s := range row {
		rs := []rune(s.text)
		lo, hi := max(from-at, 0), min(to-at, len(rs))
		if lo < hi {
			piece := s
			piece.text = string(rs[lo:hi])
			out = append(out, piece)
		}
		at += len(rs)
	}
	return out
}

// reflowPlain lays out a paragraph of prose that is not Markdown: its lines
// joined with spaces, every character of it text.
func reflowPlain(src blockSource, width int) []docRow {
	var words [][]span
	for i, line := range src.lines {
		for _, word := range strings.Fields(expandedTabs(line)) {
			words = append(words, []span{{text: word, context: src.widened[i]}})
		}
	}
	return fill(words, width)
}

func expandedTabs(line string) string { return strings.ReplaceAll(line, "\t", "    ") }

// tableRow splits one line of a table into spans: its pipes and its divider are
// markers. A table row is drawn as it stands in the source, so a table whose
// columns were lined up stays lined up.
func tableRow(line string, st *inline) []span {
	if tableDivide.MatchString(line) {
		return []span{{text: strings.TrimSpace(line), marker: true}}
	}
	var spans []span
	for _, s := range inlineSpans(strings.TrimRight(expandedTabs(line), " "), st) {
		if !s.marker && strings.Contains(s.text, "|") {
			spans = append(spans, pipesMuted(s)...)
			continue
		}
		spans = append(spans, s)
	}
	return spans
}

// lineWords splits one heading line into words of spans: its #s and a setext
// underline are markers.
func lineWords(kind, line string, st *inline) [][]span {
	var words [][]span
	switch {
	case kind == "heading" && setextLine.MatchString(line):
		return [][]span{{{text: strings.TrimSpace(line), marker: true}}}
	case kind == "heading":
		if hashes := headingHash.FindString(line); hashes != "" {
			words = append(words, []span{{text: strings.TrimSpace(hashes), marker: true}})
			line = line[len(hashes):]
		}
	}
	for _, word := range strings.Fields(line) {
		words = append(words, inlineSpans(word, st))
	}
	return words
}

// pipesMuted splits a table cell's text at its pipes, muting them.
func pipesMuted(s span) []span {
	var out []span
	for i, part := range strings.Split(s.text, "|") {
		if i > 0 {
			out = append(out, span{text: "|", marker: true})
		}
		if part != "" {
			piece := s
			piece.text = part
			out = append(out, piece)
		}
	}
	return out
}

// reflow lays out a paragraph or list item: its block-quote markers and list
// bullet lead its first row, its continuation rows hang beneath the text, and
// its words flow between them.
func reflow(src blockSource, width int) []docRow {
	source := append([]string{}, src.lines...)
	var lead, hang docRow
	if quote := quotePrefix.FindString(source[0]); quote != "" {
		markers := strings.Repeat("> ", strings.Count(quote, ">"))
		lead = append(lead, span{text: markers, marker: true})
		hang = append(hang, span{text: markers, marker: true})
		for i, line := range source {
			source[i] = quotePrefix.ReplaceAllString(line, "")
		}
	}
	if m := listMarker.FindStringSubmatch(source[0]); m != nil {
		lead = append(lead, span{text: m[1]}, span{text: m[2], marker: true}, span{text: " "})
		hang = append(hang, span{text: strings.Repeat(" ", ansi.StringWidth(m[1]+m[2])+1)})
		source[0] = source[0][len(m[0]):]
	}

	st := &inline{}
	var segments [][][]span
	var words [][]span
	for i, line := range source {
		for _, word := range strings.Fields(line) {
			words = append(words, asContext(inlineSpans(word, st), src.widened[i]))
		}
		hardBreak := strings.HasSuffix(line, "  ") || strings.HasSuffix(line, "\\")
		if hardBreak && i < len(source)-1 {
			segments = append(segments, words)
			words = nil
		}
	}
	segments = append(segments, words)

	var rows []docRow
	for _, segment := range segments {
		rows = append(rows, fill(segment, width-lead.width())...)
	}
	for i := range rows {
		prefix := hang
		if i == 0 {
			prefix = lead
		}
		// A row of nothing but widened text is context, its markers included.
		prefix = asContext(append(docRow{}, prefix...), rows[i].widenedOnly())
		rows[i] = append(prefix, rows[i]...)
	}
	return rows
}

// spanStyle is how one span is drawn: a marker in the muted colour; text in its
// spans' styles, and a heading's in its accent whether dimmed or not; and all of
// it dim when the block is not the cursor's.
func spanStyle(s span, heading, dim bool) lipgloss.Style {
	style := lipgloss.NewStyle()
	if s.marker {
		style = style.Foreground(docMarker)
	} else {
		if heading {
			style = style.Foreground(docCyan).Bold(true)
		}
		if s.code {
			style = style.Foreground(docYellow)
		}
		if s.bold {
			style = style.Bold(true)
		}
		if s.italic {
			style = style.Italic(true)
		}
	}
	if dim {
		style = style.Faint(true)
	}
	return style
}

// renderDocRow draws a laid-out row, each run of text in one style drawn as one
// piece: the space between two words in the same style belongs to the run.
func renderDocRow(row docRow, heading, dim bool) string {
	type look struct{ marker, bold, italic, code, context bool }
	lookOf := func(s span) look { return look{s.marker, s.bold, s.italic, s.code, s.context} }
	isSpace := func(s span) bool { return strings.TrimSpace(s.text) == "" }

	var b strings.Builder
	for i := 0; i < len(row); {
		if isSpace(row[i]) {
			b.WriteString(row[i].text)
			i++
			continue
		}
		style := lookOf(row[i])
		text := row[i].text
		j := i + 1
		for j < len(row) {
			if !isSpace(row[j]) && lookOf(row[j]) == style {
				text += row[j].text
				j++
				continue
			}
			if isSpace(row[j]) && j+1 < len(row) && !isSpace(row[j+1]) && lookOf(row[j+1]) == style {
				text += row[j].text + row[j+1].text
				j += 2
				continue
			}
			break
		}
		b.WriteString(spanStyle(row[i], heading, dim || row[i].context).Render(text))
		i = j
	}
	return b.String()
}

// rangeLabel names a block's source lines.
func rangeLabel(first, last int) string {
	if first == last {
		return fmt.Sprint(first)
	}
	return fmt.Sprintf("%d–%d", first, last)
}

// blockLabel is the source line range a block stop's rows span.
func blockLabel(line codeLine) string { return rangeLabel(line.number, line.lastRow.line) }

// blockRows draws one block of a Document Excerpt. Every row carries the block's
// sign, green for new and blue on the cursor's block; its first row carries the
// caret, a Comment's mark and the block's line range. The text is laid out
// within the prose width and drawn bright on the cursor's block, dim elsewhere.
// A Comment is marked in the gutter rather than by colouring the text, which in
// a Document already says what is dim and what changed.
func blockRows(cur stepCursor, i int, excerpt daemon.ExcerptWire, hasComment bool, width, proseWidth, labelWidth int) []string {
	line := cur.lines[i]
	block := excerpt.Blocks[line.block-1]
	isCursor := i == cur.cursor
	selected := cur.sel >= 0 && cur.inSelection(i)

	sign := " "
	if block.Treatment == "new" {
		sign = "+"
	}
	signSt := lipgloss.NewStyle().Foreground(docGreen)
	if isCursor {
		signSt = lipgloss.NewStyle().Foreground(docBlue)
	}
	gutterWidth := 1 + 1 + 1 + 1 + labelWidth + 2
	room := width - 2 - gutterWidth
	src := blockSource{plain: excerpt.Plain}
	for _, r := range line.rows {
		src.lines = append(src.lines, excerpt.Lines[r].Text)
		src.widened = append(src.widened, excerpt.Lines[r].Widened)
	}
	rows := layoutBlock(block.Kind, src, narrationWidth(room, proseWidth))

	out := make([]string, 0, len(rows))
	for ri, row := range rows {
		caret, mark, label := "  ", " ", ""
		if ri == 0 {
			label = blockLabel(line)
			if isCursor {
				caret = caretSt.Render("▸ ")
			}
			if hasComment {
				mark = lipgloss.NewStyle().Foreground(commentColor).Render("●")
			}
		}
		rowSign := signSt.Render(sign)
		if row.widenedOnly() {
			// Widened in only so the block reads whole: context, so unsigned.
			rowSign = " "
		}
		gutter := rowSign + " " + mark + " " + dimSt.Render(fmt.Sprintf("%-*s", labelWidth, label)) + "  "
		text := renderDocRow(row, block.Kind == "heading", !isCursor)
		if selected {
			text = selSt.Render(row.plain())
		}
		out = append(out, caret+gutter+ansi.Truncate(text, max(room, 1), "…"))
	}
	return out
}

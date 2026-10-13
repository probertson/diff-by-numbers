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
	docRed    = lipgloss.ANSIColor(1)
	docGreen  = lipgloss.ANSIColor(2)
	docYellow = lipgloss.ANSIColor(3)
	docBlue   = lipgloss.ANSIColor(4)
	docCyan   = lipgloss.ANSIColor(6)
	docMarker = lipgloss.ANSIColor(8) // bright black
)

// span is a run of a block's text drawn in one style: a Markdown marker, or
// text inside whatever spans are open where it stands; and, in an edited block,
// whether an edit struck or added it.
type span struct {
	text                       string
	marker, bold, italic, code bool
	removed, added             bool
}

// piece is a run of a block's source text as an edit left it: kept, struck
// ("removed") or added, as dbn's runs say. Text with no edit to show is one
// kept piece a line.
type piece struct {
	text   string
	change string
}

func (p piece) removed() bool { return p.change == "removed" }

// sourceLine is one source line of a block, in pieces.
type sourceLine []piece

func plainLine(text string) sourceLine { return sourceLine{{text: text}} }

// text is the line as the file has it now: everything but what was struck.
func (l sourceLine) text() string {
	var b strings.Builder
	for _, p := range l {
		if !p.removed() {
			b.WriteString(p.text)
		}
	}
	return b.String()
}

// marked gives a word's spans the edit its piece shows.
func marked(spans []span, change string) []span {
	for i := range spans {
		spans[i].removed = change == "removed"
		spans[i].added = change == "added"
	}
	return spans
}

// wordsOf splits a line's pieces into words of spans. Struck words are read in
// spans of their own, so a marker the edit struck opens nothing in the text it
// kept.
func wordsOf(line sourceLine, st *inline) [][]span {
	var words [][]span
	for _, p := range line {
		state := st
		if p.removed() {
			scratch := *st
			state = &scratch
		}
		for _, word := range strings.Fields(p.text) {
			words = append(words, marked(inlineSpans(word, state), p.change))
		}
	}
	return words
}

// separated sets struck pieces apart from the text either side with a space,
// for a line drawn as it stands, where nothing else would.
func separated(line sourceLine) sourceLine {
	var out sourceLine
	for i, p := range line {
		if p.removed() {
			if i > 0 && !strings.HasSuffix(line[i-1].text, " ") {
				out = append(out, piece{text: " "})
			}
			out = append(out, p)
			if i+1 < len(line) && !strings.HasPrefix(line[i+1].text, " ") {
				out = append(out, piece{text: " "})
			}
			continue
		}
		out = append(out, p)
	}
	return out
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
// The space between two words an edit struck, or two it added, is marked as they
// are, so a struck or added phrase reads as one.
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
			before, after := row[len(row)-1], word[0]
			row = append(row, span{text: " ", removed: before.removed && after.removed, added: before.added && after.added})
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
// source lines, each wrapped only where it is too wide for the row. An edited
// block's lines carry what the edit struck and added, laid out in the flow.
func layoutBlock(kind string, source []sourceLine, width int) []docRow {
	width = max(width, 1)
	switch kind {
	case "paragraph", "list-item":
		return reflow(source, width)
	case "heading", "table-row":
		var rows []docRow
		st := &inline{}
		for _, line := range source {
			rows = append(rows, fill(lineWords(kind, line, st), width)...)
		}
		return rows
	}
	// Fenced code, frontmatter and anything else are drawn as they stand, their
	// fences muted.
	var rows []docRow
	for i, line := range source {
		text := line.text()
		isFence := (kind == "code" && fence.MatchString(text)) ||
			(kind == "frontmatter" && (i == 0 || i == len(source)-1) && (strings.TrimSpace(text) == "---" || strings.TrimSpace(text) == "..."))
		if len(line) == 1 && line[0].change == "" {
			for _, chunk := range wrapCode(expandedTabs(text), width, width, len(text)+1) {
				rows = append(rows, docRow{{text: chunk, marker: isFence}})
			}
			continue
		}
		var row docRow
		for _, p := range separated(line) {
			row = append(row, marked([]span{{text: expandedTabs(p.text), marker: isFence}}, p.change)...)
		}
		rows = append(rows, cut(row, width)...)
	}
	return rows
}

// cut breaks a row drawn as it stands into rows no wider than width.
func cut(row docRow, width int) []docRow {
	var rows []docRow
	var current docRow
	used := 0
	for _, s := range row {
		var run []rune
		flush := func() {
			if len(run) > 0 {
				piece := s
				piece.text = string(run)
				current = append(current, piece)
				run = nil
			}
		}
		for _, r := range s.text {
			w := ansi.StringWidth(string(r))
			if used > 0 && used+w > width {
				flush()
				rows = append(rows, current)
				current, used = nil, 0
			}
			run = append(run, r)
			used += w
		}
		flush()
	}
	return append(rows, current)
}

func expandedTabs(line string) string { return strings.ReplaceAll(line, "\t", "    ") }

// lineWords splits one heading or table line into words of spans: the heading's
// #s, a setext underline and a table's pipes and divider are markers.
func lineWords(kind string, line sourceLine, st *inline) [][]span {
	text := line.text()
	switch {
	case kind == "heading" && setextLine.MatchString(text):
		return [][]span{{{text: strings.TrimSpace(text), marker: true}}}
	case kind == "table-row" && tableDivide.MatchString(text):
		return [][]span{{{text: strings.TrimSpace(text), marker: true}}}
	case kind == "table-row":
		// A table row is drawn as it stands in the source, so a table whose
		// columns were lined up stays lined up.
		line = separated(line)
		var spans []span
		for i, p := range line {
			state := st
			if p.removed() {
				scratch := *st
				state = &scratch
			}
			cell := expandedTabs(p.text)
			if i == len(line)-1 {
				cell = strings.TrimRight(cell, " ")
			}
			for _, s := range marked(inlineSpans(cell, state), p.change) {
				if !s.marker && strings.Contains(s.text, "|") {
					spans = append(spans, pipesMuted(s)...)
					continue
				}
				spans = append(spans, s)
			}
		}
		return [][]span{spans}
	case kind == "heading":
		if hashes, rest, ok := leading(line, headingHash); ok {
			return append([][]span{{{text: strings.TrimSpace(hashes), marker: true}}}, wordsOf(rest, st)...)
		}
	}
	return wordsOf(line, st)
}

// leading takes what pattern matches at the start of a line off it, where the
// line starts with text the edit did not strike.
func leading(line sourceLine, pattern *regexp.Regexp) (string, sourceLine, bool) {
	if len(line) == 0 || line[0].removed() {
		return "", line, false
	}
	found := pattern.FindString(line[0].text)
	if found == "" {
		return "", line, false
	}
	rest := append(sourceLine{{text: line[0].text[len(found):], change: line[0].change}}, line[1:]...)
	return found, rest, true
}

// pipesMuted splits a table cell's text at its pipes, muting them.
func pipesMuted(s span) []span {
	var out []span
	for i, part := range strings.Split(s.text, "|") {
		if i > 0 {
			pipe := s
			pipe.text, pipe.marker = "|", true
			out = append(out, pipe)
		}
		if part != "" {
			cell := s
			cell.text = part
			out = append(out, cell)
		}
	}
	return out
}

// reflow lays out a paragraph or list item: its block-quote markers and list
// bullet lead its first row, its continuation rows hang beneath the text, and
// its words flow between them.
func reflow(source []sourceLine, width int) []docRow {
	source = append([]sourceLine{}, source...)
	var lead, hang docRow
	if quote, _, ok := leading(source[0], quotePrefix); ok {
		markers := strings.Repeat("> ", strings.Count(quote, ">"))
		lead = append(lead, span{text: markers, marker: true})
		hang = append(hang, span{text: markers, marker: true})
		for i, line := range source {
			if _, rest, ok := leading(line, quotePrefix); ok {
				source[i] = rest
			}
		}
	}
	if len(source[0]) > 0 && !source[0][0].removed() {
		if m := listMarker.FindStringSubmatch(source[0][0].text); m != nil {
			lead = append(lead, span{text: m[1]}, span{text: m[2], marker: true}, span{text: " "})
			hang = append(hang, span{text: strings.Repeat(" ", ansi.StringWidth(m[1]+m[2])+1)})
			_, source[0], _ = leading(source[0], listMarker)
		}
	}

	st := &inline{}
	var segments [][][]span
	var words [][]span
	for i, line := range source {
		words = append(words, wordsOf(line, st)...)
		text := line.text()
		hardBreak := strings.HasSuffix(text, "  ") || strings.HasSuffix(text, "\\")
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
		rows[i] = append(append(docRow{}, prefix...), rows[i]...)
	}
	return rows
}

// spanStyle is how one span is drawn: a marker in the muted colour; text in its
// spans' styles, and a heading's in its accent whether dimmed or not; what an
// edit struck red and struck through, and what it added green and underlined,
// markers too, so an edit reads even where a terminal draws no strikethrough;
// and all of it dim when the block is not the cursor's.
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
	switch {
	case s.removed:
		style = style.Foreground(docRed).Strikethrough(true)
	case s.added:
		style = style.Foreground(docGreen).Underline(true)
	}
	if dim {
		style = style.Faint(true)
	}
	return style
}

// renderDocRow draws a laid-out row, each run of text in one style drawn as one
// piece: the space between two words in the same style belongs to the run.
func renderDocRow(row docRow, heading, dim bool) string {
	type look struct{ marker, bold, italic, code, removed, added bool }
	lookOf := func(s span) look { return look{s.marker, s.bold, s.italic, s.code, s.removed, s.added} }
	// A space an edit struck or added is drawn struck or underlined with the
	// words either side of it, so it is not plain space.
	isSpace := func(s span) bool { return strings.TrimSpace(s.text) == "" && !s.removed && !s.added }

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
		b.WriteString(spanStyle(row[i], heading, dim).Render(text))
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

// blockLabel is the source line range a block stop's rows span on the block's
// own side: an edited block's rows include the lines its edit replaced, whose
// struck words its own rows carry.
func blockLabel(line codeLine, excerpt daemon.ExcerptWire) string {
	block := excerpt.Blocks[line.block-1]
	first, last := 0, 0
	for _, r := range line.rows {
		row := excerpt.Lines[r]
		if lineSide(row, excerpt) != block.Side {
			continue
		}
		if first == 0 {
			first = row.Number
		}
		last = row.Number
	}
	return rangeLabel(first, last)
}

// blockSource is a block's own source lines, in pieces: an edited block's as its
// edit kept, struck and added them; a removed block's struck whole; any other's
// as they stand.
func blockSource(line codeLine, excerpt daemon.ExcerptWire) []sourceLine {
	block := excerpt.Blocks[line.block-1]
	var source []sourceLine
	for _, r := range line.rows {
		row := excerpt.Lines[r]
		if lineSide(row, excerpt) != block.Side {
			continue
		}
		switch {
		case block.Treatment == "removed":
			source = append(source, sourceLine{{text: row.Text, change: "removed"}})
		case len(row.Runs) > 0:
			var pieces sourceLine
			for _, run := range row.Runs {
				change := run.Change
				if change == "kept" {
					change = ""
				}
				pieces = append(pieces, piece{text: run.Text, change: change})
			}
			source = append(source, pieces)
		default:
			source = append(source, plainLine(row.Text))
		}
	}
	return source
}

// treatmentSigns is the gutter sign for each treatment, and its colour away from
// the cursor: green for new, yellow for edited, red for removed. Context has none.
var treatmentSigns = map[string]struct {
	sign   string
	colour lipgloss.TerminalColor
}{
	"new":     {"+", docGreen},
	"edited":  {"±", docYellow},
	"removed": {"-", docRed},
}

// blockRows draws one block of a Document Excerpt. Every row carries the block's
// sign, + for new, ± for edited and - for removed, blue on the cursor's block;
// its first row carries the caret, a Comment's mark and the block's line range.
// The text is laid out within the prose width and drawn bright on the cursor's
// block, dim elsewhere, an edit as track changes. A Comment is marked in the
// gutter rather than by colouring the text, which in a Document already says
// what is dim and what changed.
func blockRows(cur stepCursor, i int, excerpt daemon.ExcerptWire, hasComment bool, width, proseWidth, labelWidth int) []string {
	line := cur.lines[i]
	block := excerpt.Blocks[line.block-1]
	isCursor := i == cur.cursor
	selected := cur.sel >= 0 && cur.inSelection(i)

	sign, signColour := " ", lipgloss.TerminalColor(docGreen)
	if s, ok := treatmentSigns[block.Treatment]; ok {
		sign, signColour = s.sign, s.colour
	}
	signSt := lipgloss.NewStyle().Foreground(signColour)
	if isCursor {
		signSt = lipgloss.NewStyle().Foreground(docBlue)
	}
	gutterWidth := 1 + 1 + 1 + 1 + labelWidth + 2
	room := width - 2 - gutterWidth
	rows := layoutBlock(block.Kind, blockSource(line, excerpt), narrationWidth(room, proseWidth))

	out := make([]string, 0, len(rows))
	for ri, row := range rows {
		caret, mark, label := "  ", " ", ""
		if ri == 0 {
			label = blockLabel(line, excerpt)
			if isCursor {
				caret = caretSt.Render("▸ ")
			}
			if hasComment {
				mark = lipgloss.NewStyle().Foreground(commentColor).Render("●")
			}
		}
		gutter := signSt.Render(sign) + " " + mark + " " + dimSt.Render(fmt.Sprintf("%-*s", labelWidth, label)) + "  "
		text := renderDocRow(row, block.Kind == "heading", !isCursor)
		if selected {
			text = selSt.Render(row.plain())
		}
		out = append(out, caret+gutter+ansi.Truncate(text, max(room, 1), "…"))
	}
	return out
}

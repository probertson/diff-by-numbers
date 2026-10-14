package tui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/probertson/diff-by-numbers/internal/daemon"
	"github.com/probertson/diff-by-numbers/internal/present"
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

// blockKind, blockTreatment and wordChange are the daemon's names, as they come
// over the wire, for what a block of a Document is, how it was changed, and what
// an edit did to a run of its words (review.BlockKind, review.Treatment and
// review.WordChange).
type (
	blockKind      string
	blockTreatment string
	wordChange     string
)

const (
	blockParagraph   blockKind = "paragraph"
	blockHeading     blockKind = "heading"
	blockListItem    blockKind = "list-item"
	blockTableRow    blockKind = "table-row"
	blockCode        blockKind = "code"
	blockFrontmatter blockKind = "frontmatter"

	treatmentNew     blockTreatment = "new"
	treatmentEdited  blockTreatment = "edited"
	treatmentRemoved blockTreatment = "removed"

	wordsKept    wordChange = "kept"
	wordsRemoved wordChange = "removed"
	wordsAdded   wordChange = "added"
)

// span is a run of a block's text drawn in one style: a Markdown marker, or
// text inside whatever spans are open where it stands; and, in an edited block,
// whether an edit struck or added it.
type span struct {
	text                       string
	marker, bold, italic, code bool
	removed, added             bool
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
	lines []sourceLine
	// widened is, for each line, whether dbn widened the Excerpt by it.
	widened []bool
	// plain says the Document is not Markdown, so nothing in it is a marker.
	plain bool
}

// piece is a run of a block's source text as an edit left it: kept, struck or
// added, as dbn's runs say. Text with no edit to show is one kept piece a line.
type piece struct {
	text   string
	change wordChange
}

func (p piece) removed() bool { return p.change == wordsRemoved }

// sourceLine is one source line of a block, in pieces.
type sourceLine []piece

// unedited reports whether a line is text no edit touched: one kept piece.
func (l sourceLine) unedited() bool { return len(l) == 1 && l[0].change == wordsKept }

func plainLine(text string) sourceLine { return sourceLine{{text: text, change: wordsKept}} }

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

// marked gives spans the edit their piece shows.
func marked(spans []span, change wordChange) []span {
	for i := range spans {
		spans[i].removed = change == wordsRemoved
		spans[i].added = change == wordsAdded
	}
	return spans
}

// stateFor is the inline state a piece's text is read in: the block's own, or,
// for text an edit struck, a copy of it, so a marker the edit struck opens
// nothing in the text it kept.
func stateFor(p piece, st *inline) *inline {
	if !p.removed() {
		return st
	}
	scratch := *st
	return &scratch
}

// wordsOf splits a line's pieces into words of spans, each split by spans as
// split says. Struck words are read in spans of their own (stateFor).
func wordsOf(line sourceLine, st *inline, split func(string, *inline) []span) [][]span {
	var words [][]span
	for _, p := range line {
		state := stateFor(p, st)
		for _, word := range strings.Fields(expandedTabs(p.text)) {
			words = append(words, marked(split(word, state), p.change))
		}
	}
	return words
}

// plainSpans is a word of prose that is not Markdown: all of it text.
func plainSpans(word string, _ *inline) []span { return []span{{text: word}} }

// separated sets struck pieces apart from the text either side with a space,
// for a line drawn as it stands, where nothing else would.
func separated(line sourceLine) sourceLine {
	return present.Separated(line, piece.removed, func(p piece) string { return p.text },
		piece{text: " ", change: wordsKept})
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
type inline = present.Spans

// inlineSpans splits one word of Markdown into its markers and the text between
// them, opening and closing spans as it goes (present.Inline). An escape's
// backslash is left out, and the character it escapes read as text.
func inlineSpans(word string, st *inline) []span {
	var out []span
	for _, token := range present.Inline(word, st, false) {
		switch {
		case token.Kind == present.TokenEscape:
		case token.Kind == present.TokenMarker:
			out = append(out, span{text: token.Text, marker: true})
		case len(out) > 0 && !out[len(out)-1].marker:
			// Only an escape stood between this text and the text before it.
			out[len(out)-1].text += token.Text
		default:
			out = append(out, span{text: token.Text, bold: token.Spans.Bold, italic: token.Spans.Italic, code: token.Spans.Code})
		}
	}
	return out
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
// source lines, each wrapped only where it is too wide for the row. Prose that
// is not Markdown is reflowed with nothing in it taken for a marker. An edited
// block's lines carry what the edit struck and added, laid out in the flow.
func layoutBlock(kind blockKind, src blockSource, width int) []docRow {
	width = max(width, 1)
	source := src.lines
	switch {
	case src.plain:
		return reflowPlain(src, width)
	case kind == blockParagraph || kind == blockListItem:
		return reflow(src, width)
	case kind == blockHeading:
		var rows []docRow
		st := &inline{}
		for i, line := range source {
			words := headingWords(line, st)
			for _, word := range words {
				asContext(word, src.widened[i])
			}
			rows = append(rows, fill(words, width)...)
		}
		return rows
	case kind == blockTableRow:
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
		text := line.text()
		isFence := (kind == blockCode && fence.MatchString(text)) ||
			(kind == blockFrontmatter && (i == 0 || i == len(source)-1) && (strings.TrimSpace(text) == "---" || strings.TrimSpace(text) == "..."))
		if line.unedited() {
			for _, chunk := range wrapCode(expandedTabs(text), width, width, len(text)+1) {
				rows = append(rows, docRow{{text: chunk, marker: isFence, context: src.widened[i]}})
			}
			continue
		}
		var row docRow
		for _, p := range separated(line) {
			row = append(row, marked([]span{{text: expandedTabs(p.text), marker: isFence, context: src.widened[i]}}, p.change)...)
		}
		rows = append(rows, wrapRow(row, width)...)
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
			part := s
			part.text = string(rs[lo:hi])
			out = append(out, part)
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
		for _, word := range wordsOf(line, &inline{}, plainSpans) {
			words = append(words, asContext(word, src.widened[i]))
		}
	}
	return fill(words, width)
}

func expandedTabs(line string) string { return strings.ReplaceAll(line, "\t", "    ") }

// tableRow splits one line of a table into spans: its pipes and its divider are
// markers. A table row is drawn as it stands in the source, so a table whose
// columns were lined up stays lined up.
func tableRow(line sourceLine, st *inline) []span {
	if text := line.text(); tableDivide.MatchString(text) {
		return []span{{text: strings.TrimSpace(text), marker: true}}
	}
	line = separated(line)
	var spans []span
	for i, p := range line {
		state := stateFor(p, st)
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
	return spans
}

// headingWords splits one heading line into words of spans: its #s and a
// setext underline are markers.
func headingWords(line sourceLine, st *inline) [][]span {
	if text := line.text(); setextLine.MatchString(text) {
		return [][]span{{{text: strings.TrimSpace(text), marker: true}}}
	}
	if hashes, rest, ok := leading(line, headingHash); ok {
		return append([][]span{{{text: strings.TrimSpace(hashes), marker: true}}}, wordsOf(rest, st, inlineSpans)...)
	}
	return wordsOf(line, st, inlineSpans)
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
func reflow(src blockSource, width int) []docRow {
	source := append([]sourceLine{}, src.lines...)
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
		for _, word := range wordsOf(line, st, inlineSpans) {
			words = append(words, asContext(word, src.widened[i]))
		}
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
		// A row of nothing but widened text is context, its markers included.
		prefix = asContext(append(docRow{}, prefix...), rows[i].widenedOnly())
		rows[i] = append(prefix, rows[i]...)
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
	type look struct{ marker, bold, italic, code, removed, added, context bool }
	lookOf := func(s span) look { return look{s.marker, s.bold, s.italic, s.code, s.removed, s.added, s.context} }
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

// blockLines is every row of a block stop, in order. An edited block's rows
// include the lines its edit replaced, on the other side.
func blockLines(line codeLine, excerpt daemon.ExcerptWire) []daemon.LineWire {
	rows := make([]daemon.LineWire, 0, len(line.rows))
	for _, r := range line.rows {
		rows = append(rows, excerpt.Lines[r])
	}
	return rows
}

// ownLines is the rows of a block stop on the block's own side, in order: not
// the lines an edit replaced, whose struck words its own rows carry.
func ownLines(line codeLine, excerpt daemon.ExcerptWire) []daemon.LineWire {
	side := excerpt.Blocks[line.block-1].Side
	var own []daemon.LineWire
	for _, row := range blockLines(line, excerpt) {
		if lineSide(row, excerpt) == side {
			own = append(own, row)
		}
	}
	return own
}

// blockRefs is where each row of a block stop is, on either side: what a
// Comment on the block may cover.
func blockRefs(line codeLine, excerpt daemon.ExcerptWire) []rowRef {
	var refs []rowRef
	for _, row := range blockLines(line, excerpt) {
		refs = append(refs, rowRef{side: lineSide(row, excerpt), line: row.Number})
	}
	return refs
}

// blockLabel is the source line range a block stop's rows span on the block's
// own side.
func blockLabel(line codeLine, excerpt daemon.ExcerptWire) string {
	own := ownLines(line, excerpt)
	if len(own) == 0 {
		return rangeLabel(0, 0)
	}
	return rangeLabel(own[0].Number, own[len(own)-1].Number)
}

// sourceOf is a block's own source lines as its Excerpt shows them, in pieces:
// an edited block's as its edit kept, struck and added them; a removed block's
// struck whole; any other's, and any line the Excerpt was widened by, as they
// stand.
func sourceOf(line codeLine, excerpt daemon.ExcerptWire) blockSource {
	block := excerpt.Blocks[line.block-1]
	src := blockSource{plain: excerpt.Plain}
	for _, row := range ownLines(line, excerpt) {
		pieces := plainLine(row.Text)
		switch {
		case blockTreatment(block.Treatment) == treatmentRemoved:
			pieces = sourceLine{{text: row.Text, change: wordsRemoved}}
		case len(row.Runs) > 0:
			pieces = nil
			for _, run := range row.Runs {
				pieces = append(pieces, piece{text: run.Text, change: wordChange(run.Change)})
			}
		}
		src.lines = append(src.lines, pieces)
		src.widened = append(src.widened, row.Widened)
	}
	return src
}

// treatmentSigns is the gutter sign for each treatment, and its colour away from
// the cursor: green for new, yellow for edited, red for removed. Context has none.
var treatmentSigns = map[blockTreatment]struct {
	sign   string
	colour lipgloss.TerminalColor
}{
	treatmentNew:     {"+", docGreen},
	treatmentEdited:  {"±", docYellow},
	treatmentRemoved: {"-", docRed},
}

// blockRows draws one block of a Document Excerpt. Every row carries the block's
// sign, + for new, ± for edited and - for removed, blue on the cursor's block;
// its first row carries the caret, a Comment's mark and the block's line range.
// The text is laid out within the prose width and drawn bright on the cursor's
// block, dim elsewhere, an edit as track changes. A Comment is marked in the
// gutter rather than by colouring the text, which in a Document already says
// what is dim and what changed.
func (in paneInputs) blockRows(i int, excerpt daemon.ExcerptWire, hasComment bool, labelWidth int) []string {
	cur, width := in.cur, in.width
	line := cur.lines[i]
	block := excerpt.Blocks[line.block-1]
	isCursor := i == cur.cursor
	selected := cur.sel >= 0 && cur.inSelection(i)

	sign, signColour := " ", lipgloss.TerminalColor(docGreen)
	if s, ok := treatmentSigns[blockTreatment(block.Treatment)]; ok {
		sign, signColour = s.sign, s.colour
	}
	signSt := lipgloss.NewStyle().Foreground(signColour)
	if isCursor {
		signSt = lipgloss.NewStyle().Foreground(docBlue)
	}
	gutterWidth := 1 + 1 + 1 + 1 + labelWidth + 2
	room := width - 2 - gutterWidth
	rows := layoutBlock(blockKind(block.Kind), sourceOf(line, excerpt), proseWidthFor(room, in.proseWidth))

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
		rowSign := signSt.Render(sign)
		if row.widenedOnly() {
			// Widened in only so the block reads whole: context, so unsigned.
			rowSign = " "
		}
		gutter := rowSign + " " + mark + " " + dimSt.Render(fmt.Sprintf("%-*s", labelWidth, label)) + "  "
		text := renderDocRow(row, blockKind(block.Kind) == blockHeading, !isCursor)
		if selected {
			text = selSt.Render(row.plain())
		}
		out = append(out, caret+gutter+ansi.Truncate(text, max(room, 1), "…"))
	}
	return out
}

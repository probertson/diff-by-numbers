package review

import (
	"sort"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// block is one block of a Document: the unit its text is read and charged in.
// A Markdown Document's blocks are its paragraphs, list items, headings, table
// rows, block quotes' paragraphs, code blocks and frontmatter. Every line with
// anything on it belongs to exactly one block, so no word of a Document is
// left out of every block or counted in two; blank lines between blocks belong
// to none.
type block struct {
	// lines is the source lines the block spans, inclusive, blank lines inside
	// it (as in a fenced code block) included.
	lines LineRange
	// kind is what the block is, and level a heading's level (0 for any other
	// kind): what a Reviewer's surface needs to lay the block out.
	kind  BlockKind
	level int
	// words are the block's words in order: its source's whitespace-separated
	// tokens with a letter or digit in them. Markers on their own (`#`, `-`,
	// `|`, `>`, `---`) carry neither, so they are not words; everything else,
	// in fenced code, frontmatter and table cells alike, is.
	words []string
}

// blocksOf parses a Document into its blocks: a Markdown Document by its
// Markdown structure, and one the agent made a Document of any other type by its
// paragraphs, split at blank lines.
func blocksOf(path string, source []string) []block {
	if isMarkdown(path) {
		return markdownBlocks(source)
	}
	return blocksOver(source, ofKind(BlockParagraph, orphans(1, len(source), blankIn(source))))
}

// blankIn reports, for a line of the source numbered from 1, whether it has
// nothing but whitespace on it.
func blankIn(source []string) func(int) bool {
	return func(line int) bool { return strings.TrimSpace(source[line-1]) == "" }
}

// markdownBlocks parses a Markdown Document, given as its source lines, into
// its blocks in source order.
//
// goldmark reports where every block starts and which source lines each leaf
// consumed, but not where a block ends. The few lines no node claims are syntax
// closing the block above them (a setext underline, a closing fence, a table's
// delimiter row), so each block runs on over the unclaimed lines directly below
// it. Anything still unclaimed after that becomes a block of its own, so nothing
// the parser passed over goes uncharged.
func markdownBlocks(source []string) []block {
	var ranges []parsedBlock
	parsed := source
	if last, ok := frontmatterEnd(source); ok {
		ranges = append(ranges, parsedBlock{lines: LineRange{1, last}, kind: BlockFrontmatter})
		// The parser sees blank lines in its place, so the lines it reports
		// still number the file's own.
		parsed = append(make([]string, last), source[last:]...)
	}
	ranges = append(ranges, parsedRanges(parsed)...)
	return blocksOver(source, claimUnclaimed(source, ranges))
}

// frontmatterEnd finds YAML frontmatter: a `---` on the first line, closed by a
// later `---` or `...`. It returns the closing line.
func frontmatterEnd(source []string) (int, bool) {
	if len(source) == 0 || strings.TrimRight(source[0], " \t") != "---" {
		return 0, false
	}
	for i := 1; i < len(source); i++ {
		switch strings.TrimRight(source[i], " \t") {
		case "---", "...":
			return i + 1, true
		}
	}
	return 0, false
}

// parsedBlock is where a block is in the source and what kind it is, before its
// words are read.
type parsedBlock struct {
	lines LineRange
	kind  BlockKind
	level int
}

// ofKind makes blocks of one kind from ranges.
func ofKind(kind BlockKind, ranges []LineRange) []parsedBlock {
	out := make([]parsedBlock, 0, len(ranges))
	for _, r := range ranges {
		out = append(out, parsedBlock{lines: r, kind: kind})
	}
	return out
}

// kindOf is the kind of block a goldmark node makes, and its level if it is a
// heading. A block quote's paragraph is a paragraph: its markers are the
// surface's to style.
func kindOf(n ast.Node) (BlockKind, int) {
	switch n.Kind() {
	case ast.KindHeading:
		return BlockHeading, n.(*ast.Heading).Level
	case ast.KindParagraph, ast.KindTextBlock:
		return BlockParagraph, 0
	case ast.KindFencedCodeBlock, ast.KindCodeBlock:
		return BlockCode, 0
	case extast.KindTableHeader, extast.KindTableRow:
		return BlockTableRow, 0
	}
	return BlockOther, 0
}

// parsedRanges is every block goldmark finds, in no particular order.
func parsedRanges(source []string) []parsedBlock {
	content := []byte(strings.Join(source, "\n"))
	starts := lineStarts(content)
	lineOf := func(offset int) int {
		return sort.Search(len(starts), func(i int) bool { return starts[i] > offset })
	}
	var ranges []parsedBlock
	var visit func(n ast.Node)
	visit = func(n ast.Node) {
		switch n.Kind() {
		case ast.KindDocument, ast.KindBlockquote, ast.KindList,
			extast.KindTable:
			for child := n.FirstChild(); child != nil; child = child.NextSibling() {
				visit(child)
			}
		case ast.KindListItem:
			ranges = append(ranges, ofKind(BlockListItem, listItemRanges(n, lineOf, visit))...)
		default:
			if r, ok := spanOf(n, n, lineOf); ok {
				kind, level := kindOf(n)
				ranges = append(ranges, parsedBlock{lines: r, kind: kind, level: level})
			}
		}
	}
	// CommonMark, plus GFM tables, whose rows are blocks. A parser is made for each
	// Document rather than shared, so concurrent Reviews never share its state.
	parser := goldmark.New(goldmark.WithExtensions(extension.Table)).Parser()
	visit(parser.Parse(text.NewReader(content)))
	return ranges
}

// listItemRanges makes a list item's own content one block, starting at its
// marker, and leaves each list nested in it to be visited for blocks of its own.
// Content after a nested list, rare as it is, is a further block.
func listItemRanges(item ast.Node, lineOf func(int) int, visit func(ast.Node)) []LineRange {
	var ranges []LineRange
	// The first run of content starts at the item's marker; one after a nested
	// list starts at its own first child.
	var from ast.Node = item
	var last ast.Node
	flush := func() {
		if last != nil {
			if r, ok := spanOf(from, last, lineOf); ok {
				ranges = append(ranges, r)
			}
		}
		from, last = nil, nil
	}
	for child := item.FirstChild(); child != nil; child = child.NextSibling() {
		if child.Kind() == ast.KindList {
			flush()
			visit(child)
			continue
		}
		if from == nil {
			from = child
		}
		last = child
	}
	flush()
	// An empty item ("-" alone) has no children but is still its own block.
	if item.FirstChild() == nil {
		if r, ok := spanOf(item, item, lineOf); ok {
			ranges = append(ranges, r)
		}
	}
	return ranges
}

// spanOf is the lines from where first starts to the last source line anything
// under last consumed. A node goldmark gave no position has none.
func spanOf(first, last ast.Node, lineOf func(int) int) (LineRange, bool) {
	if first.Pos() < 0 {
		return LineRange{}, false
	}
	r := LineRange{First: lineOf(first.Pos()), Last: lineOf(first.Pos())}
	_ = ast.Walk(last, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if n.Pos() >= 0 {
			r.Last = max(r.Last, lineOf(n.Pos()))
		}
		if n.Type() == ast.TypeBlock {
			if lines := n.Lines(); lines != nil && lines.Len() > 0 {
				segment := lines.At(lines.Len() - 1)
				r.Last = max(r.Last, lineOf(max(segment.Stop-1, segment.Start)))
			}
		}
		return ast.WalkContinue, nil
	})
	return r, true
}

// lineStarts is the byte offset each line of content starts at.
func lineStarts(content []byte) []int {
	starts := []int{0}
	for i, b := range content {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// claimUnclaimed sorts the blocks, runs each one on over the non-blank lines no
// block claims directly below it, and makes a block of any non-blank run still
// unclaimed, of no kind in particular. A block starting inside one before it is
// merged into it, keeping its kind, so no line is in two blocks.
func claimUnclaimed(source []string, ranges []parsedBlock) []parsedBlock {
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].lines.First < ranges[j].lines.First })
	var merged []parsedBlock
	for _, r := range ranges {
		if n := len(merged); n > 0 && r.lines.First <= merged[n-1].lines.Last {
			merged[n-1].lines.Last = max(merged[n-1].lines.Last, r.lines.Last)
			continue
		}
		merged = append(merged, r)
	}

	blank := blankIn(source)
	var out []parsedBlock
	next := 1 // the first line no block has claimed yet
	for i, r := range merged {
		out = append(out, ofKind(BlockOther, orphans(next, r.lines.First-1, blank))...)
		limit := len(source)
		if i+1 < len(merged) {
			limit = merged[i+1].lines.First - 1
		}
		for r.lines.Last < limit && !blank(r.lines.Last+1) {
			r.lines.Last++
		}
		out = append(out, r)
		next = r.lines.Last + 1
	}
	return append(out, ofKind(BlockOther, orphans(next, len(source), blank))...)
}

// orphans makes a range of each run of non-blank lines from first to last.
func orphans(first, last int, blank func(int) bool) []LineRange {
	var out []LineRange
	for line := first; line <= last; line++ {
		if blank(line) {
			continue
		}
		start := line
		for line < last && !blank(line+1) {
			line++
		}
		out = append(out, LineRange{start, line})
	}
	return out
}

// blocksOver reads each block's words out of the source.
func blocksOver(source []string, parsed []parsedBlock) []block {
	blocks := make([]block, 0, len(parsed))
	for _, p := range parsed {
		b := block{lines: p.lines, kind: p.kind, level: p.level}
		for _, line := range source[p.lines.First-1 : p.lines.Last] {
			b.words = append(b.words, wordsIn(line)...)
		}
		blocks = append(blocks, b)
	}
	return blocks
}

// wordsIn is a line's words: its whitespace-separated tokens with at least one
// letter or digit in them.
func wordsIn(line string) []string {
	var words []string
	for _, token := range strings.Fields(line) {
		if strings.IndexFunc(token, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0 {
			words = append(words, token)
		}
	}
	return words
}

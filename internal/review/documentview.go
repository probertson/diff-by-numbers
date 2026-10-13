package review

import "slices"

// BlockKind is what a block of a Document is, which decides how a Reviewer's
// surface lays it out: a paragraph or list item is reflowed, a heading, table
// row or fenced code is drawn line for line.
type BlockKind string

const (
	BlockParagraph   BlockKind = "paragraph"
	BlockHeading     BlockKind = "heading"
	BlockListItem    BlockKind = "list-item"
	BlockTableRow    BlockKind = "table-row"
	BlockCode        BlockKind = "code"
	BlockFrontmatter BlockKind = "frontmatter"
	// BlockOther is anything else the parser found (an HTML block, a thematic
	// break), and any text it passed over.
	BlockOther BlockKind = "other"
)

// Treatment is how a block of a Document Excerpt was changed, which decides how
// it is marked: a new block is drawn quietly with a `+`, and context is drawn
// with no mark at all.
type Treatment string

const (
	TreatmentNew     Treatment = "new"
	TreatmentContext Treatment = "context"
)

// Block is one block of a Document as an Excerpt shows it: what kind it is,
// where it is in the file, and how it was changed. The Excerpt's rows name the
// block they belong to (Line.Block), so the surface groups them without
// parsing anything.
type Block struct {
	Kind BlockKind
	// Level is a heading's level, 1 to 6; 0 for any other kind.
	Level int
	// Side and Lines are the block's place in the file: the side it is on and
	// the source lines it spans there, which is what identifies it.
	Side      Side
	Lines     LineRange
	Treatment Treatment
}

// withBlocks gives a Document Excerpt its blocks, widens it so that none is
// read in part, and tags each of its rows with the block it belongs to. The
// rows it is widened by are real rows, so an Anchor can start or end on them.
// Anything else is left as it is, drawn line by line as
// code is: an Excerpt that is not of a Document's after-side, one whose
// Document dbn could not read, and, until track changes come, one showing an
// edit, which is any Excerpt with a removed row or a block that is part new,
// part not.
func (s *Session) withBlocks(view ExcerptView) ExcerptView {
	excerpt := view.Excerpt
	if excerpt.Side != NewSide {
		return view
	}
	doc, ok := s.ledger.documents[documentSide{fileRef{excerpt.Repository, excerpt.File}, NewSide}]
	if !ok {
		return view
	}
	for _, line := range view.Lines {
		if line.Side != NewSide || !line.Content() {
			return view
		}
	}

	lines := widened(view.Lines, doc, excerpt)
	if len(lines) == 0 {
		return view
	}
	rowOf := make(map[int]int, len(lines))
	for i, line := range lines {
		rowOf[line.Number] = i
	}
	var blocks []Block
	for _, b := range doc.blocks {
		if !b.lines.overlaps(lines[0].Number, lines[len(lines)-1].Number) {
			continue
		}
		shown, changed, unchanged := false, false, false
		for n := b.lines.First; n <= b.lines.Last; n++ {
			i, ok := rowOf[n]
			if !ok {
				continue
			}
			shown = true
			lines[i].Block = len(blocks) + 1
			switch {
			case lines[i].Widened:
				// Context whatever it was, so it says nothing of the block's treatment.
			case lines[i].Changed:
				changed = true
			default:
				unchanged = true
			}
		}
		if !shown {
			// Between a table's header row and the rows an Excerpt shows of it.
			continue
		}
		if changed && unchanged {
			return view
		}
		treatment := TreatmentContext
		if changed {
			treatment = TreatmentNew
		}
		blocks = append(blocks, Block{Kind: b.kind, Level: b.level, Side: NewSide, Lines: b.lines, Treatment: treatment})
	}
	view.Lines, view.Blocks = lines, blocks
	view.Plain = !isMarkdown(excerpt.File)
	return view
}

// widened is an Excerpt's rows with the rows it needs so that no block is read
// in part. A paragraph or list item it cuts into is shown whole. A table it cuts
// into is not, since widening a large table would bury a one-row edit; it gains
// its header row (and divider) instead, so its rows still read as a table. The
// rows added are context, in source order among the rest.
func widened(rows []Line, doc document, excerpt Excerpt) []Line {
	shown := make(map[int]bool, len(rows))
	for _, row := range rows {
		shown[row.Number] = true
	}
	out := slices.Clone(rows)
	add := func(r LineRange) {
		for n := r.First; n <= r.Last && n <= len(doc.lines); n++ {
			if !shown[n] {
				shown[n] = true
				out = append(out, Line{Number: n, Text: doc.lines[n-1], Side: NewSide, Widened: true})
			}
		}
	}
	for i, b := range doc.blocks {
		if !b.lines.overlaps(excerpt.FirstLine, excerpt.LastLine) {
			continue
		}
		switch b.kind {
		case BlockParagraph, BlockListItem:
			add(b.lines)
		case BlockTableRow:
			add(doc.blocks[tableHeader(doc.blocks, i)].lines)
		}
	}
	slices.SortStableFunc(out, func(a, b Line) int { return a.Number - b.Number })
	return out
}

// tableHeader is the index of the block holding the header row of the table
// the block at i is a row of. A table's rows are contiguous, one block each, and
// its header row and divider are its first block, so the header is the first of
// the run of table-row blocks, each starting on the line after the last.
func tableHeader(blocks []block, i int) int {
	for i > 0 && blocks[i-1].kind == BlockTableRow && blocks[i-1].lines.Last+1 == blocks[i].lines.First {
		i--
	}
	return i
}

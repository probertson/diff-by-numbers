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

// withBlocks gives a Document Excerpt its blocks, and tags each of its rows with
// the block it belongs to. Anything else is left as it is, drawn line by line as
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

	lines := slices.Clone(view.Lines)
	var blocks []Block
	for _, b := range doc.blocks {
		if !b.lines.overlaps(excerpt.FirstLine, excerpt.LastLine) {
			continue
		}
		changed, unchanged := false, false
		for i := range lines {
			if !b.lines.overlaps(lines[i].Number, lines[i].Number) {
				continue
			}
			lines[i].Block = len(blocks) + 1
			if lines[i].Changed {
				changed = true
			} else {
				unchanged = true
			}
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
	return view
}

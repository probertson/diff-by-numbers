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
// it is marked: a new block is drawn quietly with a `+`, an edited one as track
// changes with a `±`, a removed one with a `-`, and context with no mark at all.
//
// It is decided by the run of changes the block is in: every block an edit
// joined by text it kept, on both sides. A run that replaced old text is
// edited, one that replaced nothing is new, and one with nothing after it is
// removed.
type Treatment string

const (
	TreatmentNew     Treatment = "new"
	TreatmentEdited  Treatment = "edited"
	TreatmentRemoved Treatment = "removed"
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

// withRemovedBlocks gives a Document's old-side Excerpt, a deletion shown on its
// own, its blocks: removed where its rows are changed, context where they are
// not. A block with rows of both is left as it is, drawn line by line as code
// is, as is an Excerpt whose Document dbn could not read.
func (s *Session) withRemovedBlocks(view ExcerptView) ExcerptView {
	excerpt := view.Excerpt
	doc, ok := s.ledger.documents[documentSide{fileRef{excerpt.Repository, excerpt.File}, OldSide}]
	if !ok {
		return view
	}
	nodes := doc.nodeOfLine(0)
	lines := slices.Clone(view.Lines)
	var blocks []Block
	numbered := map[int]int{} // by node
	for i, row := range lines {
		if !row.Content() || row.Number < 1 || row.Number > len(nodes) || nodes[row.Number-1] < 0 {
			continue
		}
		node := nodes[row.Number-1]
		treatment := TreatmentContext
		if row.Changed {
			treatment = TreatmentRemoved
		}
		n, ok := numbered[node]
		if !ok {
			b := doc.blocks[node]
			blocks = append(blocks, Block{Kind: b.kind, Level: b.level, Side: OldSide, Lines: b.lines, Treatment: treatment})
			n = len(blocks)
			numbered[node] = n
		}
		if blocks[n-1].Treatment != treatment {
			return view
		}
		lines[i].Block = n
	}
	view.Lines, view.Blocks = lines, blocks
	view.Plain = !isMarkdown(excerpt.File)
	return view
}

// withBlocks gives a Document Excerpt its blocks, widens it so that none is
// read in part, and tags each of its rows with the block it belongs to. The
// rows it is widened by are real rows, so an Anchor can start or end on them.
// Its changes are read against what the rows were drawn against: the
// merge-base, or the previous round when sincePrevious. A block the Excerpt
// shows only by widening, such as a table's header row, is context.
//
// A row of a changed block's other side is tagged with the block it is drawn in:
// a row an edit replaced, with the first block its run has in the Excerpt, whose
// rows carry its struck words; a row of a run with nothing after it, with a
// removed block of its own. One whose run has nothing in the Excerpt, and any
// row that is not content, is in no block.
//
// Anything else is left as it is, drawn line by line as code is: an Excerpt
// that is not of a Document's after-side, and one whose Document dbn could not
// read.
func (s *Session) withBlocks(view ExcerptView, sincePrevious bool) ExcerptView {
	excerpt := view.Excerpt
	if excerpt.Side != NewSide {
		return view
	}
	ref := fileRef{excerpt.Repository, excerpt.File}
	doc, ok := s.ledger.documents[documentSide{ref, NewSide}]
	if !ok {
		return view
	}
	t, otherSide := s.trackedBase(ref), OldSide
	if sincePrevious {
		t, otherSide = s.trackedSincePrevious(ref, s.previous.mappings[excerpt.Repository]), PreviousSide
	}

	lines := widened(view.Lines, doc, excerpt)
	var blocks []Block
	numbered := map[Block]int{}
	number := func(b Block) int {
		if n, ok := numbered[b]; ok {
			return n
		}
		blocks = append(blocks, b)
		numbered[b] = len(blocks)
		return len(blocks)
	}
	// nodeAt is the node of the block a row is in, or -1.
	nodeAt := func(row Line) int {
		nodes := t.afterNode
		if row.Side == otherSide {
			nodes = t.beforeNode
		}
		if !row.Content() || (row.Side != NewSide && row.Side != otherSide) || row.Number < 1 || row.Number > len(nodes) {
			return -1
		}
		return nodes[row.Number-1]
	}
	// shown are the after-side blocks with a row in the Excerpt, and named those
	// with a row it names rather than widens by.
	shown, named := map[int]bool{}, map[int]bool{}
	for _, row := range lines {
		if node := nodeAt(row); node >= 0 && row.Side == NewSide {
			shown[node-len(t.before.blocks)] = true
			named[node-len(t.before.blocks)] = named[node-len(t.before.blocks)] || !row.Widened
		}
	}
	afterBlock := func(n int) Block {
		b := t.after.blocks[n]
		treatment := TreatmentContext
		if named[n] {
			treatment = t.afterTreatment(n)
		}
		return Block{Kind: b.kind, Level: b.level, Side: NewSide, Lines: b.lines, Treatment: treatment}
	}

	for i, row := range lines {
		node := nodeAt(row)
		if node < 0 {
			continue
		}
		if row.Side == NewSide {
			n := node - len(t.before.blocks)
			lines[i].Block = number(afterBlock(n))
			if blocks[lines[i].Block-1].Treatment == TreatmentEdited && !row.Widened {
				lines[i].Runs = t.runs[row.Number]
			}
			continue
		}
		group := t.group[node]
		if afters := t.afterBlocksOf(group); len(afters) > 0 {
			for _, n := range afters {
				if shown[n] {
					lines[i].Block = number(afterBlock(n))
					break
				}
			}
			continue
		}
		b := t.before.blocks[node]
		lines[i].Block = number(Block{Kind: b.kind, Level: b.level, Side: otherSide, Lines: b.lines, Treatment: TreatmentRemoved})
	}
	view.Lines, view.Blocks = lines, blocks
	view.Plain = !isMarkdown(excerpt.File)
	return view
}

// widened is an Excerpt's rows with the rows it needs so that no block is read
// in part. A paragraph or list item it cuts into is shown whole. A table it cuts
// into is not, since widening a large table would bury a one-row edit; it gains
// its header row (and divider) instead, so its rows still read as a table. The
// rows added are context, each in source order among the after-side rows: just
// above the first that follows it, and the other side's rows drawn above that
// one, or below the last.
func widened(rows []Line, doc document, excerpt Excerpt) []Line {
	shown := make(map[int]bool, len(rows))
	for _, row := range rows {
		if row.Side == NewSide {
			shown[row.Number] = true
		}
	}
	var added []Line
	add := func(r LineRange) {
		for n := r.First; n <= r.Last && n <= len(doc.lines); n++ {
			if !shown[n] {
				shown[n] = true
				added = append(added, Line{Number: n, Text: doc.lines[n-1], Side: NewSide, Widened: true})
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
	slices.SortFunc(added, func(a, b Line) int { return a.Number - b.Number })

	out := slices.Clone(rows)
	for _, row := range added {
		at := len(out)
		for i, existing := range out {
			if existing.Side == NewSide && existing.Number > row.Number {
				at = i
				break
			}
		}
		for at > 0 && out[at-1].Side != NewSide && at < len(out) {
			at--
		}
		out = slices.Insert(out, at, row)
	}
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

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
// block they belong to (Line.Block), so the surface groups them without working
// out the Document's structure itself (ADR-0005, #166).
type Block struct {
	Kind BlockKind
	// Level is a heading's level, 1 to 6; 0 for any other kind.
	Level int
	// Diagram is, for a code block that is a Mermaid diagram (a fence whose
	// info string is `mermaid`), its source, which the Browser Renderer draws
	// (#191, #193); nil for any other block.
	Diagram *DiagramSources
	// Side and Lines are the block's place in the file: the side it is on and
	// the source lines it spans there, which is what identifies it.
	Side      Side
	Lines     LineRange
	Treatment Treatment
	// Path is the heading path: the texts of the headings the block sits
	// under, outermost first, read from the whole Document on the block's own
	// side, so a heading far outside the Excerpt still counts. A heading's own
	// block sits under its parents. It is empty outside every heading, and in
	// a Document that is not Markdown (#145).
	Path []string
}

// shown is a parsed block as an Excerpt shows it: on side, and treated so. An
// edited diagram's source before the edit is its caller's to add.
func (b block) shown(side Side, treatment Treatment) Block {
	shown := Block{Kind: b.kind, Level: b.level, Side: side, Lines: b.lines, Treatment: treatment, Path: b.path}
	if b.diagram {
		sources := diagramSourcesOf(treatment, b.diagramSource)
		shown.Diagram = &sources
	}
	return shown
}

// withDocumentBlocks is view drawn as a Document: its rows, tagged with their
// blocks and heading paths, the blocks, and whether the Document is plain prose
// rather than Markdown. own reports which rows already carry the heading path
// of their own text; every other row is given its neighbour's (withNearPaths).
func withDocumentBlocks(view ExcerptView, lines []Line, own []bool, blocks []Block) ExcerptView {
	withNearPaths(lines, own)
	view.Lines, view.Blocks = lines, blocks
	view.Plain = !isMarkdown(view.Excerpt.File)
	return view
}

// withNearPaths gives each row with no heading path of its own, such as a blank
// line between blocks, that of the nearest row above it that has one, or else
// below it: the text it is read with. A blank line ends the block above it.
func withNearPaths(lines []Line, own []bool) {
	first := slices.Index(own, true)
	if first < 0 {
		return
	}
	near := first
	for i := range lines {
		if own[i] {
			near = i
			continue
		}
		lines[i].Path = lines[near].Path
	}
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
	own := make([]bool, len(lines))
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
			blocks = append(blocks, doc.blocks[node].shown(OldSide, treatment))
			n = len(blocks)
			numbered[node] = n
		}
		if blocks[n-1].Treatment != treatment {
			return view
		}
		lines[i].Block, lines[i].Path, own[i] = n, blocks[n-1].Path, true
	}
	return withDocumentBlocks(view, lines, own, blocks)
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
	// A block is numbered by where it is, which is what identifies it.
	type place struct {
		side  Side
		first int
	}
	numbered := map[place]int{}
	number := func(b Block) int {
		if n, ok := numbered[place{b.Side, b.Lines.First}]; ok {
			return n
		}
		blocks = append(blocks, b)
		numbered[place{b.Side, b.Lines.First}] = len(blocks)
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
		treatment := TreatmentContext
		if named[n] {
			treatment = t.afterTreatment(n)
		}
		b := t.after.blocks[n].shown(NewSide, treatment)
		if b.Diagram != nil && treatment == TreatmentEdited {
			b.Diagram.Before = t.diagramBefore(n)
		}
		return b
	}

	// namedLines are the after-side lines the Excerpt names, which carry runs; a
	// line it is widened by, or does not show, carries none.
	namedLines := map[int]bool{}
	for _, row := range lines {
		if row.Side == NewSide && !row.Widened {
			namedLines[row.Number] = true
		}
	}

	own := make([]bool, len(lines))
	for i, row := range lines {
		node := nodeAt(row)
		if node < 0 {
			continue
		}
		own[i] = true
		if row.Side == NewSide {
			n := node - len(t.before.blocks)
			lines[i].Block, lines[i].Path = number(afterBlock(n)), t.after.blocks[n].path
			if blocks[lines[i].Block-1].Treatment == TreatmentEdited && !row.Widened {
				lines[i].Runs = t.runsOn(row.Number, func(n int) bool { return !namedLines[n] })
			}
			continue
		}
		// The row sits under the other side's headings, whichever block it is
		// drawn in, or none.
		lines[i].Path = t.before.blocks[node].path
		// It is drawn in the first shown block it was matched with, whose
		// edit strikes its words; where none of them is shown (git paired it
		// with a line in this Excerpt while its words went to a block shown
		// elsewhere), it is a removed block of its own, as any deletion is.
		group := t.group[node]
		matched := false
		for _, n := range t.afterBlocksOf(group) {
			if shown[n] {
				lines[i].Block, matched = number(afterBlock(n)), true
				break
			}
		}
		if !matched {
			lines[i].Block = number(t.before.blocks[node].shown(otherSide, TreatmentRemoved))
		}
	}
	return withDocumentBlocks(view, lines, own, blocks)
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
		switch {
		case isReadWhole(b.kind):
			add(b.lines)
		case b.kind == BlockTableRow:
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

// isReadWhole reports whether an Excerpt cutting into a block of this kind is
// widened to the whole of it.
func isReadWhole(kind BlockKind) bool {
	return kind == BlockParagraph || kind == BlockListItem
}

// widenedSpan is the lines from first to last, stretched over each block read
// whole that they cut into: the span an Excerpt of them covers once widened,
// table headers aside.
func (d document) widenedSpan(first, last int) (int, int) {
	from, to := first, last
	for _, b := range d.blocks {
		if isReadWhole(b.kind) && b.lines.overlaps(first, last) {
			from, to = min(from, b.lines.First), max(to, b.lines.Last)
		}
	}
	return from, to
}

// headerOf is the number, among an Excerpt's blocks, of the block holding the
// header row of the table that block b is a row of: 0 when b is no table row,
// or when the header is not shown. Widening adds the header to any Excerpt
// showing a table's rows, so it is shown whenever they are.
func (s *Session) headerOf(view ExcerptView, b int) int {
	block := view.Blocks[b-1]
	if block.Kind != BlockTableRow {
		return 0
	}
	doc, ok := s.ledger.documents[documentSide{fileRef{view.Excerpt.Repository, view.Excerpt.File}, block.Side}]
	if !ok {
		return 0
	}
	for i, parsed := range doc.blocks {
		if parsed.lines.First != block.Lines.First {
			continue
		}
		header := doc.blocks[tableHeader(doc.blocks, i)].lines.First
		for h, shown := range view.Blocks {
			if shown.Side == block.Side && shown.Lines.First == header {
				return h + 1
			}
		}
	}
	return 0
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

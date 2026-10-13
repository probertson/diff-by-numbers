package review

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/intraline"
)

// isMarkdown is dbn's own classification of a file: whether its content is
// prose for a person to read rather than code. dbn judges by file type alone,
// so Markdown is a Document and everything else is not; the agent, which knows
// what a file is for, may overrule it (#159), and a round goes by
// isDocumentAfterOverruling. The path is the one the file is described under:
// its new path, or the path it had if it was deleted.
func isMarkdown(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return true
	}
	return false
}

// DocumentBudget is the soft ceiling, in words, on the Document text a Step may
// show before it must carry a justification. It is counted in words because
// lines measure prose only as it happens to be wrapped: an unwrapped Document
// fits a paragraph on one line, and a table row is one line however wide. A page
// of about a thousand words is what a Reviewer was comfortable reading, where
// StepBudget's 30 lines would cut a section into half-pages.
const DocumentBudget = 1000

// documentSide names one side of one Document: the text its blocks are parsed
// from.
type documentSide struct {
	fileRef
	side Side
}

func sideOf(line ChangedLine) documentSide {
	return documentSide{refOf(line), line.Side}
}

// refOf is the file a Changed Line is in.
func refOf(line ChangedLine) fileRef {
	return fileRef{line.Repository, line.File}
}

// withDocuments parses into blocks both sides of each Document that has Changed
// Lines, reading them as the round holds them: the after-side to say what the
// Reviewer reads, the before-side to say what an edit struck. A side the file
// does not have (an added file's before, a deleted one's after) is not read, and
// a side that cannot be read is left out, its lines charged as code.
func (l ledger) withDocuments(resolver Resolver, round RoundSource) ledger {
	reader, ok := resolver.(FileReader)
	if !ok {
		return l
	}
	l.documents = map[documentSide]document{}
	tried := map[fileRef]bool{}
	for _, line := range l.lines {
		ref := refOf(line)
		if tried[ref] || !l.isDocumentAfterOverruling(ref.repository, ref.file) {
			continue
		}
		tried[ref] = true
		for _, side := range []Side{OldSide, NewSide} {
			if (side == OldSide && l.fileStatus[ref] == FileAdded) || (side == NewSide && l.fileStatus[ref] == FileDeleted) {
				continue
			}
			source, err := reader.ReadFile(ref.repository, ref.file, side, round)
			if err != nil {
				continue
			}
			l.documents[documentSide{ref, side}] = document{lines: source, blocks: blocksOf(ref.file, source)}
		}
	}
	return l
}

// document is one side of a Document: its source lines and the blocks they
// make.
type document struct {
	lines  []string
	blocks []block
}

// nodeOfLine is, for each line in order, the block it falls in, numbered from
// first, or -1 for a blank line between blocks.
func (d document) nodeOfLine(first int) []int {
	out := make([]int, len(d.lines))
	for i := range out {
		out[i] = -1
	}
	for n, b := range d.blocks {
		for line := b.lines.First; line <= b.lines.Last; line++ {
			out[line-1] = first + n
		}
	}
	return out
}

// wordsOn is the words of the given lines in order, each with the node of the
// block it falls in.
func (d document) wordsOn(lines, nodes []int) ([]string, []int) {
	var words []string
	var of []int
	for _, line := range lines {
		for _, word := range wordsIn(d.lines[line-1]) {
			words = append(words, word)
			of = append(of, nodes[line-1])
		}
	}
	return words, of
}

// linesExcept is the numbers of the lines not in the set, in order.
func (d document) linesExcept(set map[int]bool) []int {
	var out []int
	for n := 1; n <= len(d.lines); n++ {
		if !set[n] {
			out = append(out, n)
		}
	}
	return out
}

// isChargedInWords reports whether a Changed Line is charged in words, as
// Document text, rather than as a line of code: it is in a Document, after
// overruling, whose text could be read.
func (l ledger) isChargedInWords(line ChangedLine) bool {
	_, ok := l.documents[sideOf(line)]
	return ok
}

// costedBlock is what one changed block of a Document costs to read, in words. It
// is the block as it stands after the change together with the before-side
// blocks it was made from, since the Reviewer reads the two as one: an edited
// paragraph with its struck words marked. A wholly new block has no before-side,
// and a wholly removed one no after-side.
//
// It is the one reckoning of what Document text costs: the budget charges a Step
// every costedBlock it reads, and describe_changes reports them to plan by.
type costedBlock struct {
	fileRef
	// after and before are the lines of the blocks it spans on each side, in
	// file order. An edit that splits or joins paragraphs spans more than one.
	after, before []LineRange
	// words is its cost: the words of its after-side, plus the words of its
	// before-side the change struck.
	words int
	// removed are the before-side Changed Lines in it that the budget counts.
	// They are drawn with whichever Step their edit rides along with, so that
	// Step reads the block too.
	removed []ChangedLine
}

// firstLine orders a costedBlock in its file: by its after-side, or by its
// before-side when it was removed.
func (c costedBlock) firstLine() int {
	if len(c.after) > 0 {
		return c.after[0].First
	}
	return c.before[0].First
}

// costedBlocks are the costs of the blocks of every Document that a change the
// budget counts touched, in file order, leaving out those that cost nothing.
//
// A block's before and after are found by a word-level diff of each edit's two
// sides: blocks are one where they share text it kept (see costsOf). A block costs
// its after-side words and its before-side words the diff struck, or nothing
// when the diff changed none of its words, as in a rewrap. A block no counted
// Changed Line falls in costs nothing either: it is unchanged, or was read in
// the previous Round, by the reckoning that pre-marks Changed Lines.
func (l ledger) costedBlocks() []costedBlock {
	touched := map[fileRef][]ChangedLine{}
	for _, line := range l.budgetedLines() {
		if l.isChargedInWords(line) {
			touched[refOf(line)] = append(touched[refOf(line)], line)
		}
	}
	var out []costedBlock
	for ref, lines := range touched {
		out = append(out, l.costsOf(ref, lines)...)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.repository != b.repository {
			return a.repository < b.repository
		}
		if a.file != b.file {
			return a.file < b.file
		}
		return a.firstLine() < b.firstLine()
	})
	return out
}

// costsOf diffs one Document's two sides and costs the changed blocks the given
// counted Changed Lines fall in.
//
// Every block on either side is a node, the before-side's first, and a block is
// joined to each block on the other side it shares text with. A line nothing
// changed is shared whole, and pairs with the line it was, since the lines
// nothing changed stand in the same order on both sides. Between two of them,
// the changed lines' words are diffed as one passage each, as intraline reads one
// edit, so a word is never paired with a like one in some other part of the
// Document. What that diff struck or added is what changed.
func (l ledger) costsOf(ref fileRef, lines []ChangedLine) []costedBlock {
	before := l.documents[documentSide{ref, OldSide}]
	after := l.documents[documentSide{ref, NewSide}]
	beforeNode := before.nodeOfLine(0)
	afterNode := after.nodeOfLine(len(before.blocks))

	parent := make([]int, len(before.blocks)+len(after.blocks))
	for n := range parent {
		parent[n] = n
	}
	var find func(int) int
	find = func(n int) int {
		if parent[n] != n {
			parent[n] = find(parent[n])
		}
		return parent[n]
	}
	join := func(a, b int) {
		if a >= 0 && b >= 0 {
			parent[find(a)] = find(b)
		}
	}

	struck := make([]int, len(before.blocks)) // words struck, by before-side block
	changedNode := map[int]bool{}
	diff := func(oldLines, newLines []int) {
		beforeWords, beforeNodes := before.wordsOn(oldLines, beforeNode)
		afterWords, afterNodes := after.wordsOn(newLines, afterNode)
		added := make([]bool, len(afterWords))
		for j := range added {
			added[j] = true
		}
		for i, j := range intraline.Matched(beforeWords, afterWords) {
			if j < 0 {
				struck[beforeNodes[i]]++
				changedNode[beforeNodes[i]] = true
				continue
			}
			added[j] = false
			join(beforeNodes[i], afterNodes[j])
		}
		for j, isAdded := range added {
			if isAdded {
				changedNode[afterNodes[j]] = true
			}
		}
	}

	changedOn := map[Side]map[int]bool{OldSide: {}, NewSide: {}}
	for _, line := range l.lines {
		if line.Repository == ref.repository && line.File == ref.file {
			changedOn[line.Side][line.Line] = true
		}
	}
	unchangedBefore := before.linesExcept(changedOn[OldSide])
	unchangedAfter := after.linesExcept(changedOn[NewSide])
	if len(unchangedBefore) != len(unchangedAfter) {
		// The sides cannot be lined up, as when one could not be read: diff
		// them whole.
		diff(before.linesExcept(nil), after.linesExcept(nil))
	} else {
		nextBefore, nextAfter := 1, 1
		for k := range unchangedBefore {
			diff(lineNumbers(nextBefore, unchangedBefore[k]-1), lineNumbers(nextAfter, unchangedAfter[k]-1))
			join(beforeNode[unchangedBefore[k]-1], afterNode[unchangedAfter[k]-1])
			nextBefore, nextAfter = unchangedBefore[k]+1, unchangedAfter[k]+1
		}
		diff(lineNumbers(nextBefore, len(before.lines)), lineNumbers(nextAfter, len(after.lines)))
	}

	costs := map[int]*costedBlock{} // by the node each is found by
	costOf := func(node int) *costedBlock {
		root := find(node)
		if costs[root] == nil {
			costs[root] = &costedBlock{fileRef: ref}
		}
		return costs[root]
	}
	for n, b := range before.blocks {
		c := costOf(n)
		c.before = append(c.before, b.lines)
		c.words += struck[n]
	}
	for n, b := range after.blocks {
		c := costOf(len(before.blocks) + n)
		c.after = append(c.after, b.lines)
		c.words += len(b.words)
	}
	changed := map[int]bool{}
	for n := range changedNode {
		changed[find(n)] = true
	}

	counted := map[int]bool{}
	for _, line := range lines {
		nodes := afterNode
		if line.Side == OldSide {
			nodes = beforeNode
		}
		if line.Line < 1 || line.Line > len(nodes) || nodes[line.Line-1] < 0 {
			continue
		}
		root := find(nodes[line.Line-1])
		counted[root] = true
		if line.Side == OldSide {
			costs[root].removed = append(costs[root].removed, line)
		}
	}
	var out []costedBlock
	for root, c := range costs {
		if counted[root] && changed[root] && c.words > 0 {
			out = append(out, *c)
		}
	}
	return out
}

// lineNumbers is the line numbers first to last, none when last is before first.
func lineNumbers(first, last int) []int {
	var out []int
	for n := first; n <= last; n++ {
		out = append(out, n)
	}
	return out
}

// documentWordsIn is what a Step's Document text costs: every changed block the
// Step reads any of, charged whole even where an Excerpt cuts into it, since
// reading part of a paragraph means reading the paragraph. A block read twice is
// charged once.
func (l ledger) documentWordsIn(step Step, all []Step, costs []costedBlock) int {
	words := 0
	for _, c := range costs {
		if l.reads(step, all, c) {
			words += c.words
		}
	}
	return words
}

// reads reports whether a Step reads any part of a changed block: an Excerpt
// touches it on either side, or the Step draws its removed lines riding along
// with their edit.
func (l ledger) reads(step Step, all []Step, c costedBlock) bool {
	for _, excerpt := range step.Excerpts {
		if excerpt.Repository != c.repository || excerpt.File != c.file {
			continue
		}
		ranges := c.after
		if excerpt.Side == OldSide {
			ranges = c.before
		}
		for _, r := range ranges {
			if r.overlaps(excerpt.FirstLine, excerpt.LastLine) {
				return true
			}
		}
	}
	for _, line := range c.removed {
		if l.riddenAlong(line, []Step{step}, all) {
			return true
		}
	}
	return false
}

// documentCosts prices each Document's changes for a description. Rather than
// count words itself, it asks the budget check what a Step would be charged: each
// changed block is priced as a Step showing it alone, and a Document's total as
// one Step showing every block listed, so the description can never disagree
// with a refusal.
//
// A block is listed by its after-side. One with none, removed, is listed by its
// before-side only where it has to be shown on its own; one an edit replaced is
// drawn beside the after-side that replaced it, and a Step showing that
// after-side is charged for it there.
func (l ledger) documentCosts() map[fileRef]*DocumentCost {
	costed := l.costedBlocks()
	alone := l.removedOutright()
	out := map[fileRef]*DocumentCost{}
	shown := map[fileRef]*Step{}
	for parsed := range l.documents {
		out[parsed.fileRef] = &DocumentCost{}
		shown[parsed.fileRef] = &Step{}
	}
	for _, c := range costed {
		file := c.fileRef
		side, ranges := NewSide, c.after
		if len(ranges) == 0 {
			if !alone.anyOf(c.removed) {
				continue
			}
			side, ranges = OldSide, c.before
		}
		var alonePriced Step
		for _, r := range ranges {
			alonePriced.Excerpts = append(alonePriced.Excerpts,
				Excerpt{Repository: c.repository, File: c.file, Side: side, FirstLine: r.First, LastLine: r.Last})
		}
		words := l.documentWordsIn(alonePriced, []Step{alonePriced}, costed)
		if words == 0 {
			continue
		}
		listed := BlockCost{Lines: LineRange{First: ranges[0].First, Last: ranges[len(ranges)-1].Last}, Words: words}
		if side == OldSide {
			out[file].OldBlocks = append(out[file].OldBlocks, listed)
		} else {
			out[file].NewBlocks = append(out[file].NewBlocks, listed)
		}
		shown[file].Excerpts = append(shown[file].Excerpts, alonePriced.Excerpts...)
	}
	for file, cost := range out {
		cost.Words = l.documentWordsIn(*shown[file], []Step{*shown[file]}, costed)
		for _, blocks := range [][]BlockCost{cost.NewBlocks, cost.OldBlocks} {
			sort.Slice(blocks, func(i, j int) bool { return blocks[i].Lines.First < blocks[j].Lines.First })
		}
	}
	return out
}

// removedLines is a set of before-side Changed Lines.
type removedLines map[ChangedLine]bool

// anyOf reports whether any of the lines is in the set.
func (r removedLines) anyOf(lines []ChangedLine) bool {
	for _, line := range lines {
		if r[line] {
			return true
		}
	}
	return false
}

// removedOutright are the before-side Changed Lines the budget counts that no
// modification replaced: the ones only an old-side Excerpt can show.
func (l ledger) removedOutright() removedLines {
	out := removedLines{}
	for _, line := range l.budgetedLines() {
		if line.Side == OldSide && !l.replaced(line) {
			out[line] = true
		}
	}
	return out
}

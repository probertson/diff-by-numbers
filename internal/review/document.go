package review

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/intraline"
)

// isDocument is dbn's own classification of a file: whether its content is
// prose for a person to read rather than code. dbn judges by file type alone,
// so Markdown is a Document and everything else is not; the agent, which knows
// what a file is for, may overrule it (#159). The path is the one the file is
// described under: its new path, or the path it had if it was deleted.
func isDocument(path string) bool {
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
	repository, file string
	side             Side
}

func sideOf(line ChangedLine) documentSide {
	return documentSide{line.Repository, line.File, line.Side}
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
	l.documents = map[documentSide][]block{}
	tried := map[fileRef]bool{}
	for _, line := range l.lines {
		ref := fileRef{line.Repository, line.File}
		if tried[ref] || !isDocument(ref.file) {
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
			l.documents[documentSide{ref.repository, ref.file, side}] = markdownBlocks(source)
		}
	}
	return l
}

// isDocumentText reports whether a Changed Line is charged in words, as Document
// text, rather than as a line of code.
func (l ledger) isDocumentText(line ChangedLine) bool {
	_, ok := l.documents[sideOf(line)]
	return ok
}

// blockCost is what one changed block of a Document costs to read, in words. It
// is the block as it stands after the change together with the before-side
// blocks it was made from, since the Reviewer reads the two as one: an edited
// paragraph with its struck words marked. A wholly new block has no before-side,
// and a wholly removed one no after-side.
//
// It is the one reckoning of what Document text costs: the budget charges a Step
// every blockCost it reads, and describe_changes reports them to plan by.
type blockCost struct {
	repository, file string
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

// firstLine orders a blockCost in its file: by its after-side, or by its
// before-side when it was removed.
func (c blockCost) firstLine() int {
	if len(c.after) > 0 {
		return c.after[0].First
	}
	return c.before[0].First
}

// documentCosts are the costs of the blocks of every Document that a change the
// budget counts touched, in file order, leaving out those that cost nothing.
//
// A block's before and after are found by a word-level diff of the Document's
// two sides: blocks are one where they share a word the diff kept. A block costs
// its after-side words and its before-side words the diff struck, or nothing
// when the diff changed none of its words, as in a rewrap. A block no counted
// Changed Line falls in costs nothing either: it is unchanged, or was read in
// the previous Round, by the reckoning that pre-marks Changed Lines.
func (l ledger) documentCosts() []blockCost {
	touched := map[fileRef][]ChangedLine{}
	for _, line := range l.budgetedLines() {
		if l.isDocumentText(line) {
			ref := fileRef{line.Repository, line.File}
			touched[ref] = append(touched[ref], line)
		}
	}
	var out []blockCost
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
// Changed Lines fall in.
func (l ledger) costsOf(ref fileRef, lines []ChangedLine) []blockCost {
	before := l.documents[documentSide{ref.repository, ref.file, OldSide}]
	after := l.documents[documentSide{ref.repository, ref.file, NewSide}]

	// Every block on either side is a node, the before-side's first, and each
	// is joined to every block it shares a kept word with.
	parent := make([]int, len(before)+len(after))
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
	beforeWords, beforeNode := flatten(before, 0)
	afterWords, afterNode := flatten(after, len(before))
	added := make([]bool, len(afterWords))
	for j := range added {
		added[j] = true
	}
	struck := make([]int, len(before)) // words struck, by before-side block
	for i, j := range intraline.Matched(beforeWords, afterWords) {
		if j < 0 {
			struck[beforeNode[i]]++
			continue
		}
		added[j] = false
		parent[find(beforeNode[i])] = find(afterNode[j])
	}

	costs := map[int]*blockCost{} // by the node each is found by
	changed := map[int]bool{}
	costOf := func(node int) *blockCost {
		root := find(node)
		if costs[root] == nil {
			costs[root] = &blockCost{repository: ref.repository, file: ref.file}
		}
		return costs[root]
	}
	for n, b := range before {
		c := costOf(n)
		c.before = append(c.before, b.lines)
		c.words += struck[n]
		if struck[n] > 0 {
			changed[find(n)] = true
		}
	}
	for n, b := range after {
		c := costOf(len(before) + n)
		c.after = append(c.after, b.lines)
		c.words += len(b.words)
	}
	for j, isAdded := range added {
		if isAdded {
			changed[find(afterNode[j])] = true
		}
	}

	counted := map[int]bool{}
	for _, line := range lines {
		blocks, first := after, len(before)
		if line.Side == OldSide {
			blocks, first = before, 0
		}
		for n, b := range blocks {
			if !b.overlaps(line.Line, line.Line) {
				continue
			}
			root := find(first + n)
			counted[root] = true
			if line.Side == OldSide {
				costs[root].removed = append(costs[root].removed, line)
			}
			break
		}
	}
	var out []blockCost
	for root, c := range costs {
		if counted[root] && changed[root] && c.words > 0 {
			out = append(out, *c)
		}
	}
	return out
}

// flatten is the words of blocks in order, each with the node of the block it is
// in, numbering the blocks from first.
func flatten(blocks []block, first int) ([]string, []int) {
	var words []string
	var nodes []int
	for n, b := range blocks {
		for _, word := range b.words {
			words = append(words, word)
			nodes = append(nodes, first+n)
		}
	}
	return words, nodes
}

// documentWordsIn is what a Step's Document text costs: every changed block the
// Step reads any of, charged whole even where an Excerpt cuts into it, since
// reading part of a paragraph means reading the paragraph. A block read twice is
// charged once.
func (l ledger) documentWordsIn(step Step, all []Step, costs []blockCost) int {
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
func (l ledger) reads(step Step, all []Step, c blockCost) bool {
	for _, excerpt := range step.Excerpts {
		if excerpt.Repository != c.repository || excerpt.File != c.file {
			continue
		}
		ranges := c.after
		if excerpt.Side == OldSide {
			ranges = c.before
		}
		for _, r := range ranges {
			if excerpt.FirstLine <= r.Last && excerpt.LastLine >= r.First {
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

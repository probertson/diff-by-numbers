package review

import (
	"strings"
	"unicode"

	"github.com/probertson/diff-by-numbers/internal/intraline"
)

// docWord is one word of a Document and where it stands: runes are not needed,
// so start and end are byte offsets into its line, numbered from 1. node is the
// block it falls in (see documentDiff).
type docWord struct {
	text       string
	line       int
	start, end int
	node       int
}

// wordsAt is the words of the given lines in order, each with where it stands
// and the node of the block it falls in. A word is what wordsIn counts.
func (d document) wordsAt(lines, nodes []int) []docWord {
	var out []docWord
	for _, line := range lines {
		text := d.lines[line-1]
		at := 0
		for _, field := range strings.Fields(text) {
			start := at + strings.Index(text[at:], field)
			at = start + len(field)
			if strings.IndexFunc(field, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) < 0 {
				continue
			}
			out = append(out, docWord{text: field, line: line, start: start, end: at, node: nodes[line-1]})
		}
	}
	return out
}

// wordStep is one step of the word diff of a Document's two sides: a word kept,
// struck (no after) or added (no before).
type wordStep struct {
	before, after *docWord
}

// documentDiff is how one Document changed between two of its versions, in
// words: the before and after sides, which block each line of them falls in, how
// the blocks group into runs of changes, and every step of the word diff, in the
// order a reader meets them.
//
// Every block on either side is a node, the before-side's first, and a block is
// joined to each block on the other side it shares text with. A line nothing
// changed is shared whole, and pairs with the line it was, since the lines
// nothing changed stand in the same order on both sides. Between two of them,
// the changed lines' words are diffed as one passage each, as intraline reads one
// edit, so a word is never paired with a like one in some other part of the
// Document. What that diff struck or added is what changed.
type documentDiff struct {
	before, after         document
	beforeNode, afterNode []int
	// group is the run of changes each node belongs to, named by one of its
	// nodes: every block an edit joined, on both sides.
	group []int
	// struck counts, by before-side block, the words the diff struck; wordsChanged
	// says, by group, that the diff struck or added any word in it, which a
	// rewrap does not; linesChanged says that any line in it was changed.
	struck       []int
	wordsChanged map[int]bool
	linesChanged map[int]bool
	steps        []wordStep
}

// diffDocument diffs a Document's two sides, given the lines changed on each.
func diffDocument(before, after document, changedBefore, changedAfter map[int]bool) documentDiff {
	d := documentDiff{
		before: before, after: after,
		beforeNode: before.nodeOfLine(0), afterNode: after.nodeOfLine(len(before.blocks)),
		struck:       make([]int, len(before.blocks)),
		wordsChanged: map[int]bool{},
		linesChanged: map[int]bool{},
	}
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

	changedNode := map[int]bool{}
	diff := func(oldLines, newLines []int) {
		beforeWords := before.wordsAt(oldLines, d.beforeNode)
		afterWords := after.wordsAt(newLines, d.afterNode)
		beforeTexts, afterTexts := make([]string, len(beforeWords)), make([]string, len(afterWords))
		for i, w := range beforeWords {
			beforeTexts[i] = w.text
		}
		for j, w := range afterWords {
			afterTexts[j] = w.text
		}
		for _, p := range intraline.Aligned(beforeTexts, afterTexts) {
			var step wordStep
			if p.Before >= 0 {
				step.before = &beforeWords[p.Before]
			}
			if p.After >= 0 {
				step.after = &afterWords[p.After]
			}
			switch {
			case step.before != nil && step.after != nil:
				join(step.before.node, step.after.node)
			case step.before != nil:
				d.struck[step.before.node]++
				changedNode[step.before.node] = true
			default:
				changedNode[step.after.node] = true
			}
			d.steps = append(d.steps, step)
		}
	}

	unchangedBefore := before.linesExcept(changedBefore)
	unchangedAfter := after.linesExcept(changedAfter)
	if len(unchangedBefore) != len(unchangedAfter) {
		// The sides cannot be lined up, as when one could not be read: diff
		// them whole.
		diff(before.linesExcept(nil), after.linesExcept(nil))
	} else {
		nextBefore, nextAfter := 1, 1
		for k := range unchangedBefore {
			diff(lineNumbers(nextBefore, unchangedBefore[k]-1), lineNumbers(nextAfter, unchangedAfter[k]-1))
			diff([]int{unchangedBefore[k]}, []int{unchangedAfter[k]})
			join(d.beforeNode[unchangedBefore[k]-1], d.afterNode[unchangedAfter[k]-1])
			nextBefore, nextAfter = unchangedBefore[k]+1, unchangedAfter[k]+1
		}
		diff(lineNumbers(nextBefore, len(before.lines)), lineNumbers(nextAfter, len(after.lines)))
	}

	d.group = make([]int, len(parent))
	for n := range parent {
		d.group[n] = find(n)
	}
	for n := range changedNode {
		d.wordsChanged[d.group[n]] = true
	}
	for line := range changedBefore {
		if line >= 1 && line <= len(d.beforeNode) && d.beforeNode[line-1] >= 0 {
			d.linesChanged[d.group[d.beforeNode[line-1]]] = true
		}
	}
	for line := range changedAfter {
		if line >= 1 && line <= len(d.afterNode) && d.afterNode[line-1] >= 0 {
			d.linesChanged[d.group[d.afterNode[line-1]]] = true
		}
	}
	return d
}

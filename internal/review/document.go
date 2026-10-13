package review

import (
	"path/filepath"
	"sort"
	"strings"
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

// withDocuments parses into blocks each side of each Document that has Changed
// Lines, reading it as the round holds it. A side that cannot be read is left
// out, and its lines are charged as code.
func (l ledger) withDocuments(resolver Resolver, round RoundSource) ledger {
	reader, ok := resolver.(FileReader)
	if !ok {
		return l
	}
	l.documents = map[documentSide][]block{}
	tried := map[documentSide]bool{}
	for _, line := range l.lines {
		ref := sideOf(line)
		if tried[ref] || !l.isDocumentFile(ref.repository, ref.file) {
			continue
		}
		tried[ref] = true
		source, err := reader.ReadFile(ref.repository, ref.file, ref.side, round)
		if err != nil {
			continue
		}
		l.documents[ref] = blocksOf(ref.file, source)
	}
	return l
}

// isDocumentText reports whether a Changed Line is charged in words, as Document
// text, rather than as a line of code.
func (l ledger) isDocumentText(line ChangedLine) bool {
	_, ok := l.documents[sideOf(line)]
	return ok
}

// costingBlocks are the blocks a change touched: those holding a Changed Line
// the budget counts. Only they cost anything; a block around them that nothing
// changed is context, and costs nothing however much of it a Step shows.
func (l ledger) costingBlocks() map[documentSide][]block {
	touched := map[documentSide]map[int]bool{}
	for _, line := range l.budgetedLines() {
		ref := sideOf(line)
		if _, ok := l.documents[ref]; !ok {
			continue
		}
		if touched[ref] == nil {
			touched[ref] = map[int]bool{}
		}
		touched[ref][line.Line] = true
	}
	costing := map[documentSide][]block{}
	for ref, lines := range touched {
		for _, b := range l.documents[ref] {
			for n := b.lines.First; n <= b.lines.Last; n++ {
				if lines[n] {
					costing[ref] = append(costing[ref], b)
					break
				}
			}
		}
	}
	return costing
}

// documentWordsIn is what a Step's Document text costs: every word of each
// costing block any of its Excerpts touches. A block an Excerpt cuts into is
// charged whole, since reading part of a paragraph means reading the paragraph.
// A block two Excerpts touch is charged once.
func documentWordsIn(step Step, costing map[documentSide][]block) int {
	words := 0
	for ref, blocks := range costing {
		for _, b := range blocks {
			for _, excerpt := range step.Excerpts {
				if excerpt.Repository == ref.repository && excerpt.File == ref.file && excerpt.Side == ref.side &&
					b.overlaps(excerpt.FirstLine, excerpt.LastLine) {
					words += len(b.words)
					break
				}
			}
		}
	}
	return words
}

// documentCosts prices each Document's changes for a description. Rather than
// count words itself, it asks the budget check what a Step would be charged: each
// block is priced as a Step showing it alone, and a Document's total as one Step
// showing every block listed, so the description can never disagree with a
// refusal.
//
// A before-side block is listed only where it has to be shown on its own. One
// an edit replaced is drawn beside the after-side that replaced it, and a Step
// showing that after-side is charged for it there.
func (l ledger) documentCosts() map[fileRef]*DocumentCost {
	costing := l.costingBlocks()
	alone := l.removedOutright()
	out := map[fileRef]*DocumentCost{}
	shown := map[fileRef]*Step{}
	for ref := range l.documents {
		file := fileRef{ref.repository, ref.file}
		out[file] = &DocumentCost{}
		shown[file] = &Step{}
	}
	for ref, blocks := range costing {
		file := fileRef{ref.repository, ref.file}
		for _, b := range blocks {
			if ref.side == OldSide && !alone.any(ref, b) {
				continue
			}
			excerpt := Excerpt{Repository: ref.repository, File: ref.file, Side: ref.side, FirstLine: b.lines.First, LastLine: b.lines.Last}
			words := documentWordsIn(Step{Excerpts: []Excerpt{excerpt}}, costing)
			if words == 0 {
				continue
			}
			listed := BlockCost{Lines: b.lines, Words: words}
			if ref.side == OldSide {
				out[file].OldBlocks = append(out[file].OldBlocks, listed)
			} else {
				out[file].NewBlocks = append(out[file].NewBlocks, listed)
			}
			shown[file].Excerpts = append(shown[file].Excerpts, excerpt)
		}
	}
	for file, cost := range out {
		cost.Words = documentWordsIn(*shown[file], costing)
		for _, blocks := range [][]BlockCost{cost.NewBlocks, cost.OldBlocks} {
			sort.Slice(blocks, func(i, j int) bool { return blocks[i].Lines.First < blocks[j].Lines.First })
		}
	}
	return out
}

// removedLines is a set of before-side Changed Lines.
type removedLines map[ChangedLine]bool

// any reports whether a block holds any of the lines.
func (r removedLines) any(ref documentSide, b block) bool {
	for n := b.lines.First; n <= b.lines.Last; n++ {
		if r[ChangedLine{Repository: ref.repository, File: ref.file, Side: ref.side, Line: n}] {
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
		if line.Side != OldSide {
			continue
		}
		replaced := false
		for _, c := range l.correspondences {
			if c.Repository == line.Repository && c.File == line.File && c.isModification() && c.coversOld(line.Line) {
				replaced = true
				break
			}
		}
		if !replaced {
			out[line] = true
		}
	}
	return out
}

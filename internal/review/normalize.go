package review

import "sort"

// normalize rewrites a Walkthrough's Excerpts into the ranges dbn will actually
// use, before anything judges them. It runs once the ledger is derived, so it
// can take account of what git says changed, and its result is what gets stored:
// rendering, coverage, the budget and anchoring all see one set of ranges, so
// "shown" and "accounted for" stay in lockstep (the rule riddenAlong documents,
// correspondence.go).
func normalize(steps []Step, l ledger, preShown map[ChangedLine]bool) []Step {
	out := copySteps(steps)
	absorbWhitespace(out, l, preShown)
	return out
}

// copySteps deep-copies the Excerpts, so normalisation never writes through to
// the Walkthrough the caller handed in. A post that is then rejected must leave
// the agent's own value exactly as it sent it.
func copySteps(steps []Step) []Step {
	out := make([]Step, len(steps))
	copy(out, steps)
	for i, step := range steps {
		if len(step.Excerpts) == 0 {
			continue
		}
		out[i].Excerpts = make([]Excerpt, len(step.Excerpts))
		copy(out[i].Excerpts, step.Excerpts)
	}
	return out
}

// fileSide is the scope absorption works within: a whitespace-only Changed Line
// in one file on one side can only ever be absorbed by an Excerpt showing that
// same file and side.
type fileSide struct {
	repository string
	file       string
	side       Side
}

// span is an inclusive run of line numbers on one side of one file.
type span struct{ first, last int }

// showing is the Excerpts over one file and side, in Step order and then Excerpt
// order within a Step. That order is what the rule for a run between two
// Excerpts means by the "earlier" one.
type showing []*Excerpt

// absorbWhitespace widens Excerpts over the runs of whitespace-only Changed
// Lines beside them. Agents split a new file into its sections and leave out the
// blank separators between them, which are Changed Lines like any other: without
// this, a post is refused over lines nobody needs to be told to read.
//
// Only a run that would otherwise go unaccounted for is absorbed, and only where
// it touches an Excerpt. A whitespace-only line standing on its own still has to
// be covered, because exempting them everywhere would count lines as accounted
// for without ever showing them, and a blank line can carry meaning — a Markdown
// paragraph break, a YAML block scalar.
func absorbWhitespace(steps []Step, l ledger, preShown map[ChangedLine]bool) {
	if len(l.whitespace) == 0 {
		return
	}
	for key, shown := range excerptsByFileSide(steps) {
		for _, run := range unaccountedRuns(l, key, steps, preShown) {
			if excerpt := shown.absorber(run); excerpt != nil {
				widen(excerpt, run)
			}
		}
	}
}

// excerptsByFileSide groups the Walkthrough's Excerpts by what they show,
// keeping the Walkthrough's own order within each group.
func excerptsByFileSide(steps []Step) map[fileSide]showing {
	out := map[fileSide]showing{}
	for i := range steps {
		for j := range steps[i].Excerpts {
			excerpt := &steps[i].Excerpts[j]
			key := fileSide{excerpt.Repository, excerpt.File, excerpt.Side}
			out[key] = append(out[key], excerpt)
		}
	}
	return out
}

// unaccountedRuns groups the file's unaccounted-for whitespace-only Changed Lines
// into the maximal runs of consecutive lines they form. Runs are worked out
// against the ranges as posted and are disjoint, so absorbing one cannot change
// which Excerpt another touches.
func unaccountedRuns(l ledger, key fileSide, steps []Step, preShown map[ChangedLine]bool) []span {
	var numbers []int
	for line := range l.whitespace {
		if line.Repository != key.repository || line.File != key.file || line.Side != key.side {
			continue
		}
		if l.accountedFor(line, steps, preShown) {
			continue
		}
		numbers = append(numbers, line.Line)
	}
	sort.Ints(numbers)

	var runs []span
	for i := 0; i < len(numbers); {
		run := span{first: numbers[i], last: numbers[i]}
		for i++; i < len(numbers) && numbers[i] == run.last+1; i++ {
			run.last = numbers[i]
		}
		runs = append(runs, run)
	}
	return runs
}

// absorber picks the Excerpt an unaccounted-for run joins: the first one it touches, in
// Step order and then Excerpt order. A run between two Excerpts therefore goes
// to whichever of them the Reviewer reaches first, so the blank lines arrive
// with a section the Walkthrough has already introduced rather than ahead of one
// it has not.
func (s showing) absorber(run span) *Excerpt {
	for _, excerpt := range s {
		if excerpt.LastLine == run.first-1 || excerpt.FirstLine == run.last+1 {
			return excerpt
		}
	}
	return nil
}

func widen(excerpt *Excerpt, run span) {
	if run.first < excerpt.FirstLine {
		excerpt.FirstLine = run.first
	}
	if run.last > excerpt.LastLine {
		excerpt.LastLine = run.last
	}
}

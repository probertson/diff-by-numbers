package review

import "sort"

// normalize rewrites a Round's Excerpts into the ranges dbn will actually
// use, before anything judges them. It runs once the ledger is derived, so it
// can take account of what git says changed, and its result is what gets stored:
// rendering, coverage, the budget and anchoring all see one set of ranges, so
// "shown" and "accounted for" stay in lockstep (the rule riddenAlong documents,
// correspondence.go).
func normalize(steps []Step, changeSet ChangeSet, l ledger) []Step {
	out := copySteps(steps)
	// The order is what each pass needs of the one before it. Filling in the
	// repository comes first because everything after keys on it; renaming comes
	// before absorption because absorption groups Excerpts by the file they show.
	fillInRepositories(out, changeSet)
	aliasRenames(out, l)
	absorbWhitespace(out, l)
	return out
}

// fillInRepositories fills in the repository an Excerpt or Acknowledgement left
// out. Validation has already refused a missing one where the Change Set has
// several, so by here an empty value can only mean the single repository under
// review — and from here on everything downstream sees a concrete value.
func fillInRepositories(steps []Step, changeSet ChangeSet) {
	only, ok := changeSet.sole()
	if !ok {
		return
	}
	for i := range steps {
		for j := range steps[i].Excerpts {
			if steps[i].Excerpts[j].Repository == "" {
				steps[i].Excerpts[j].Repository = only
			}
		}
		for j := range steps[i].Acknowledgements {
			if steps[i].Acknowledgements[j].Repository == "" {
				steps[i].Acknowledgements[j].Repository = only
			}
		}
	}
}

// aliasRenames rewrites a rename's source path to its destination, which is the
// path every atom of the file was derived under. Writing about a rename under the
// name the file came from is the natural thing to do and accounted for nothing.
//
// Only the old side of an Excerpt is aliased. A new-side range names the working
// tree, where the source path no longer exists — if it does exist, it is a
// different file, and renamedTo declines to alias it.
func aliasRenames(steps []Step, l ledger) {
	if len(l.renames) == 0 {
		return
	}
	for i := range steps {
		for j := range steps[i].Excerpts {
			excerpt := &steps[i].Excerpts[j]
			if excerpt.Side != OldSide {
				continue
			}
			if destination, ok := l.renamedTo(excerpt.Repository, excerpt.File); ok {
				excerpt.File = destination
			}
		}
		aliasAcknowledged(steps[i].Acknowledgements, l)
	}
}

// aliasAcknowledged resolves each claimed path and drops the duplicates that
// leaves behind: an agent covering its bases lists both ends of a rename, which
// is one file and must appear once in the manifest.
func aliasAcknowledged(acknowledgements []Acknowledgement, l ledger) {
	for i, acknowledgement := range acknowledgements {
		files := make([]string, 0, len(acknowledgement.Files))
		claimed := map[string]bool{}
		for _, file := range acknowledgement.Files {
			if destination, ok := l.renamedTo(acknowledgement.Repository, file); ok {
				file = destination
			}
			if claimed[file] {
				continue
			}
			claimed[file] = true
			files = append(files, file)
		}
		acknowledgements[i].Files = files
	}
}

// copySteps deep-copies everything normalisation rewrites, so it never writes
// through to the Round the caller handed in. A post that is then rejected
// must leave the agent's own value exactly as it sent it, and keeping that
// guarantee here means no later pass has to remember to make its own copy.
func copySteps(steps []Step) []Step {
	out := make([]Step, len(steps))
	copy(out, steps)
	for i, step := range steps {
		if len(step.Excerpts) > 0 {
			out[i].Excerpts = make([]Excerpt, len(step.Excerpts))
			copy(out[i].Excerpts, step.Excerpts)
		}
		if len(step.Acknowledgements) > 0 {
			out[i].Acknowledgements = make([]Acknowledgement, len(step.Acknowledgements))
			copy(out[i].Acknowledgements, step.Acknowledgements)
		}
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
func absorbWhitespace(steps []Step, l ledger) {
	if len(l.whitespace) == 0 {
		return
	}
	for key, shown := range excerptsByFileSide(steps) {
		for _, run := range unaccountedRuns(l, key, steps) {
			if excerpt := shown.absorber(run); excerpt != nil {
				widen(excerpt, run)
			}
		}
	}
}

// excerptsByFileSide groups the Round's Excerpts by what they show,
// keeping the Round's own order within each group.
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
func unaccountedRuns(l ledger, key fileSide, steps []Step) []span {
	var numbers []int
	for line := range l.whitespace {
		if line.Repository != key.repository || line.File != key.file || line.Side != key.side {
			continue
		}
		if l.accountedFor(line, steps) {
			continue
		}
		numbers = append(numbers, line.Line)
	}
	sort.Ints(numbers)
	return runsOf(numbers)
}

// runsOf turns sorted line numbers into the contiguous runs they form.
func runsOf(numbers []int) []span {
	var out []span
	for i := 0; i < len(numbers); {
		run := span{first: numbers[i], last: numbers[i]}
		for i++; i < len(numbers) && numbers[i] == run.last+1; i++ {
			run.last = numbers[i]
		}
		out = append(out, run)
	}
	return out
}

// absorber picks the Excerpt an unaccounted-for run joins: the first one it touches, in
// Step order and then Excerpt order. A run between two Excerpts therefore goes
// to whichever of them the Reviewer reaches first, so the blank lines arrive
// with a section the Round has already introduced rather than ahead of one
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

package review

import "fmt"

// previousRound is what the Round on screen is compared with when it is
// shaded by what changed since the last round rather than since the merge-base
// (#44): that round's number, the Round its code is read from, and how each
// repository's files changed between the two.
//
// The merge-base shading answers "what does this branch change", which stays
// the same round after round: an all-new file reads as all-new every time,
// however little the agent touched it since. What a Reviewer in a Revision
// Round wants to see first is what changed since they last read it.
type previousRound struct {
	number int
	round  RoundSource
	// mappings has no entry for a repository whose two snapshots could not be
	// compared; its code is shaded by the merge-base as it always was.
	mappings map[string]RoundMapping
	// withdrawn and unchanged are worked out once, when the round is accepted:
	// nothing they depend on changes while it is on screen, and the view is
	// drawn every second.
	withdrawn []Withdrawal
	unchanged []bool
}

// comparedWith works out how the round being accepted changed since the earlier
// one it answers. It is nil for a first round, and for one whose repositories
// could not be compared with that round at all: a comparison the Reviewer could
// switch to but never see would only mislead.
func (s *Session) comparedWith(earlier *earlierRound, current RoundSource, set ChangeSet) *previousRound {
	snapshotter, ok := s.deriver.(Snapshotter)
	if !ok || earlier == nil {
		return nil
	}
	mappings := map[string]RoundMapping{}
	for _, repo := range set.Repositories {
		from, had := earlier.state.Snapshots[repo.Root]
		to, took := current.Snapshots[repo.Root]
		if !had || !took {
			continue
		}
		mapping, err := snapshotter.MapBetween(repo.Root, from, to)
		if err != nil {
			continue
		}
		mappings[repo.Root] = mapping
	}
	if len(mappings) == 0 {
		return nil
	}
	return &previousRound{number: s.roundNumber - 1, round: earlier.state.RoundSource, mappings: mappings}
}

// settle works out what the comparison shows that does not change while the
// round is on screen. It runs once the Round is accepted, since it reads
// the Round's Steps.
func (s *Session) settle(previous *previousRound) {
	if previous == nil {
		return
	}
	previous.withdrawn = s.withdrawals(previous)
	previous.unchanged = make([]bool, len(s.current.Steps))
	for i, step := range s.current.Steps {
		previous.unchanged[i] = s.stepUnchangedSince(previous, step)
	}
}

// ToggleSincePreviousRound switches the Revision Round on screen between
// showing what changed since the previous round and showing every change under
// review. It is the Reviewer's view of the whole review, not of one Step, so it
// lives here with the rows the daemon draws (#57) and holds as they move between
// Steps.
func (s *Session) ToggleSincePreviousRound() error {
	if s.current == nil {
		return reject(RejectedNoRound, "there is no Round to show")
	}
	if s.previous == nil {
		return reject(RejectedNoPreviousRound,
			"round %d has no earlier round it can be compared with", s.roundNumber)
	}
	s.showAll = !s.showAll
	return nil
}

// sincePrevious reports whether the round on screen is being shaded by what
// changed since the previous round.
func (s *Session) sincePrevious() bool { return s.previous != nil && !s.showAll }

// previousNumber is the round the one on screen is compared with, or 0.
func (s *Session) previousNumber() int {
	if s.previous == nil {
		return 0
	}
	return s.previous.number
}

// placedAt is the line of an Excerpt an edit's previous-round rows are drawn at,
// and whether this Excerpt draws them at all. The lines an edit replaced go
// above the first of their replacements the Excerpt shows. The lines it
// withdrew go in the gap they left, below the line git names as standing above
// it — so a gap just under the Excerpt's last line is drawn at its foot, and one
// just above its first line is left to whatever is shown above, or to the
// Overview, which lists every withdrawal regardless.
func placedAt(excerpt Excerpt, edit RoundEdit) (int, bool) {
	if edit.NewCount == 0 {
		gap := edit.NewFirst
		return gap, excerpt.FirstLine <= gap && gap <= excerpt.LastLine
	}
	first := max(edit.NewFirst, excerpt.FirstLine)
	return first, first <= min(edit.NewFirst+edit.NewCount-1, excerpt.LastLine)
}

// drawsEdit reports whether the Excerpt being drawn is the one in its Step that
// draws an edit's previous-round rows: the first to place them. Like a
// merge-base before-side (firstShowing), they belong to the Step, not to each
// range in it, so a Step showing one edit through two ranges draws it once.
func drawsEdit(in drawnIn, excerpt Excerpt, edit RoundEdit) bool {
	for _, earlier := range in.step.Excerpts[:max(in.at, 0)] {
		if earlier.Side != NewSide || earlier.Repository != excerpt.Repository || earlier.File != excerpt.File {
			continue
		}
		if _, places := placedAt(earlier, edit); places {
			return false
		}
	}
	return true
}

// sinceRows draws a new-side Excerpt shaded by what changed since the previous
// round: a line that round already had, untouched, is plain; one added or
// rewritten since is shaded; and the lines each edit replaced or withdrew are
// drawn from the previous round's snapshot, as removed rows where they stood.
// It answers false when the round on screen is not being shaded this way, or
// this repository's two rounds could not be compared, and the caller shades by
// the merge-base as ever.
func (s *Session) sinceRows(excerpt Excerpt, in drawnIn, after []Line) ([]Line, bool) {
	if !s.sincePrevious() {
		return nil, false
	}
	mapping, ok := s.previous.mappings[excerpt.Repository]
	if !ok {
		return nil, false
	}
	above := map[int][]RoundEdit{}
	below := map[int][]RoundEdit{}
	for _, edit := range mapping.Edits(excerpt.File) {
		at, places := placedAt(excerpt, edit)
		if !places || !drawsEdit(in, excerpt, edit) {
			continue
		}
		if edit.NewCount == 0 {
			below[at] = append(below[at], edit)
		} else {
			above[at] = append(above[at], edit)
		}
	}
	earlierPath := mapping.PathIn(excerpt.File)
	previousOf := func(edits []RoundEdit) []Line {
		var rows []Line
		for _, edit := range edits {
			rows = append(rows, s.previousRows(s.previous, excerpt.Repository, earlierPath, edit)...)
		}
		return rows
	}

	var out []Line
	for _, line := range after {
		for _, edit := range above[line.Number] {
			rows := previousOf([]RoundEdit{edit})
			changes := s.roundEditChanges(excerpt.Repository, excerpt.File, earlierPath, edit)
			for i := range rows {
				if rows[i].Content() {
					rows[i].Emphasis = changes.removed[rows[i].Number]
				}
			}
			out = append(out, rows...)
		}
		_, untouched := mapping.Lookup(excerpt.File, line.Number)
		line.Side = NewSide
		line.Changed = !untouched
		// The added line is marked against the line it took the place of,
		// whether or not this range is the one that draws that line.
		for _, edit := range mapping.Edits(excerpt.File) {
			if line.Number >= edit.NewFirst && line.Number < edit.NewFirst+edit.NewCount {
				line.Emphasis = s.roundEditChanges(excerpt.Repository, excerpt.File, earlierPath, edit).added[line.Number]
			}
		}
		out = append(out, line)
		out = append(out, previousOf(below[line.Number])...)
	}
	return out, true
}

// previousRows reads the lines an edit replaced or withdrew, as the previous
// round had them. A read that fails is marked in their place rather than left
// out, so a removal is never silently missing from the rows.
func (s *Session) previousRows(previous *previousRound, repository, file string, edit RoundEdit) []Line {
	if edit.OldCount == 0 {
		return nil
	}
	lines, err := s.resolver.Resolve(Excerpt{
		Repository: repository, File: file, Side: NewSide,
		FirstLine: edit.OldFirst, LastLine: edit.OldFirst + edit.OldCount - 1,
	}, previous.round)
	if err != nil {
		return []Line{{Number: edit.OldFirst, Side: PreviousSide, Unreadable: true,
			Text: fmt.Sprintf("(round %d's lines could not be read: %v)", previous.number, err)}}
	}
	for i := range lines {
		lines[i].Side = PreviousSide
		lines[i].Changed = true
	}
	return lines
}

// Withdrawal is a run of lines the previous round had that this one removed
// outright, with nothing in their place.
//
// No Changed Line can name one. A line added in the previous round and taken
// out again was never in the merge-base and is not in the working tree, so it is
// in neither round's Change Set, and the agent has no coverage reason to show
// the spot where it was. dbn finds these in the comparison of the two rounds'
// snapshots instead, and always lists them.
type Withdrawal struct {
	Repository string
	// File is the path in this round, or in the previous one for a file this
	// round no longer has.
	File string
	// After is the line of this round the withdrawn lines stood below; 0 means
	// the top of the file.
	After int
	// PreviousFirst is where the run began in the previous round.
	PreviousFirst int
	Lines         []string
}

// withdrawals lists every Withdrawal since the previous round, by repository in
// Change Set order and then by file.
func (s *Session) withdrawals(previous *previousRound) []Withdrawal {
	var out []Withdrawal
	for _, repo := range s.current.ChangeSet.Repositories {
		mapping, ok := previous.mappings[repo.Root]
		if !ok {
			continue
		}
		for _, file := range mapping.Files() {
			for _, edit := range mapping.Edits(file) {
				if edit.NewCount > 0 || edit.OldCount == 0 {
					continue
				}
				withdrawal := Withdrawal{Repository: repo.Root, File: file, After: edit.NewFirst, PreviousFirst: edit.OldFirst}
				for _, line := range s.previousRows(previous, repo.Root, mapping.PathIn(file), edit) {
					withdrawal.Lines = append(withdrawal.Lines, line.Text)
				}
				out = append(out, withdrawal)
			}
		}
	}
	return out
}

// stepUnchangedSince says whether nothing a Step shows is new or edited since
// the previous round: every line its new-side Excerpts show maps back untouched
// and no withdrawal is drawn among them, every deletion its old-side Excerpts
// show was already deleted then, and no file its Acknowledgements claim differs
// between the rounds. A repository whose rounds could not be compared counts as
// changed, since nothing says otherwise.
func (s *Session) stepUnchangedSince(previous *previousRound, step Step) bool {
	for _, excerpt := range step.Excerpts {
		mapping, ok := previous.mappings[excerpt.Repository]
		if !ok {
			return false
		}
		if excerpt.Side != NewSide {
			if s.ledger.deletedSinceEarlier(excerpt) {
				return false
			}
			continue
		}
		for n := excerpt.FirstLine; n <= excerpt.LastLine; n++ {
			if _, untouched := mapping.Lookup(excerpt.File, n); !untouched {
				return false
			}
		}
		for _, edit := range mapping.Edits(excerpt.File) {
			if _, places := placedAt(excerpt, edit); places && edit.NewCount == 0 && edit.OldCount > 0 {
				return false
			}
		}
	}
	for _, ack := range step.Acknowledgements {
		mapping, ok := previous.mappings[ack.Repository]
		if !ok {
			return false
		}
		for _, file := range ack.Files {
			if mapping.Touched(file) {
				return false
			}
		}
	}
	return true
}

// previousWithdrawn is what the Overview lists as withdrawn since the previous
// round, or nil in a round with nothing to compare with.
func (s *Session) previousWithdrawn() []Withdrawal {
	if s.previous == nil {
		return nil
	}
	return append([]Withdrawal(nil), s.previous.withdrawn...)
}

// previousUnchanged says, per Step, that nothing it shows changed since the
// previous round, or is nil in a round with nothing to compare with.
func (s *Session) previousUnchanged() []bool {
	if s.previous == nil {
		return nil
	}
	return append([]bool(nil), s.previous.unchanged...)
}

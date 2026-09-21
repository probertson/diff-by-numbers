package review

// Correspondence pairs the before-side lines a single edit removed with the
// after-side lines that replaced them, as git derived it. It is adapter-derived
// plumbing, not a domain concept the Authoring Agent authors against (ADR-0002):
// the agent still writes arbitrary, Side-qualified ranges. dbn keeps the pairing
// only so that showing an edit's after-side can account for and render the
// before-side it replaced — the "point once" behaviour of a Walkthrough.
//
// A modification carries both sides. A pure addition carries only a new side; a
// pure deletion only an old side, and a pure deletion never rides along — it has
// no after-side to point at, so it must be shown or acknowledged deliberately.
type Correspondence struct {
	Repository string
	File       string
	// OldFirst..OldLast is the removed range, empty (OldFirst == 0) when the edit
	// only added. NewFirst..NewLast is the range that replaced it, empty
	// (NewFirst == 0) when the edit only removed.
	OldFirst, OldLast int
	NewFirst, NewLast int
}

func (c Correspondence) hasOld() bool { return c.OldFirst > 0 }
func (c Correspondence) hasNew() bool { return c.NewFirst > 0 }

// isModification reports a correspondence with both sides — the only kind whose
// before-side rides along when its after-side is shown.
func (c Correspondence) isModification() bool { return c.hasOld() && c.hasNew() }

// coversOld reports whether a before-side line number falls in this
// correspondence's removed range.
func (c Correspondence) coversOld(line int) bool {
	return c.hasOld() && line >= c.OldFirst && line <= c.OldLast
}

// riddenAlong reports whether a before-side Changed Line is rendered by one of
// the given Steps as part of the edit that removed it. Only a modification rides
// along, and a line rides along exactly where it is drawn, so "shown" and
// "accounted for" stay in lockstep and no line can be counted covered without
// also being seen.
//
// It takes the Steps twice on purpose. `all` is the whole Walkthrough, because
// which Step draws a given before-side line is a question about all of them: a
// line no Step claims falls to the Step showing the replacement's first line.
// `within` is the subset being asked about — every Step when validating
// coverage, and the Steps behind the Reviewer when counting what they have seen.
func (l ledger) riddenAlong(line ChangedLine, within, all []Step) bool {
	if line.Side != OldSide {
		return false
	}
	for _, c := range l.correspondences {
		if c.Repository != line.Repository || c.File != line.File || !c.isModification() {
			continue
		}
		if !c.coversOld(line.Line) {
			continue
		}
		// Worked out once for the line rather than once per Step: whether anyone
		// claimed it is what decides where the remainder goes, and this runs for
		// every Changed Line each time the screen is drawn.
		claimed := claimedSomewhere(all, c, line.Line)
		for _, step := range within {
			if drawsBefore(c, step, line.Line, claimed) {
				return true
			}
		}
	}
	return false
}

// assignedBefore is the before-side of one modification that one Step draws, in
// file order.
//
// A rewrite reaches dbn as a single modification with no pairing inside it, so
// when its after-side is split across Steps by idea, dbn cannot know which old
// lines belong with which new ones — and guessing would put code under an
// explanation that does not describe it. The Authoring Agent says so instead,
// with old-side Excerpts (ADR-0002): whatever a Step covers that way is the
// before-side it draws.
//
// Whatever no Step claims falls to the Step showing the replacement's first
// line, which is where the whole before-side has always gone. With no old-side
// Excerpts at all that is the entire remainder, which is the behaviour before
// any of this existed.
func assignedBefore(c Correspondence, step Step, all []Step) []int {
	if !c.isModification() {
		return nil
	}
	var out []int
	for line := c.OldFirst; line <= c.OldLast; line++ {
		if drawsBefore(c, step, line, claimedSomewhere(all, c, line)) {
			out = append(out, line)
		}
	}
	return out
}

// drawsBefore reports whether a Step draws one before-side line of a
// modification: it claims the line with an old-side Excerpt, or it shows the
// replacement's first line and no Step claimed this one. The caller works out
// claimedElsewhere, which depends on the whole Walkthrough rather than this Step.
func drawsBefore(c Correspondence, step Step, line int, claimedElsewhere bool) bool {
	if claimsOld(step, c, line) {
		return true
	}
	return !claimedElsewhere && stepShowsNewStart(step, c)
}

// claimedSomewhere reports whether any Step claims a before-side line.
func claimedSomewhere(all []Step, c Correspondence, line int) bool {
	return anyStep(all, func(s Step) bool { return claimsOld(s, c, line) })
}

// claimsOld reports whether a Step covers a before-side line with an old-side
// Excerpt — the agent's way of saying which part of a rewrite this Step replaced.
func claimsOld(step Step, c Correspondence, line int) bool {
	return stepShows(step, OldSide, c, line)
}

// stepShowsNewStart reports whether a Step has a new-side Excerpt covering the
// first line a correspondence's replacement occupies — the Step that takes
// whatever before-side no Step claimed.
func stepShowsNewStart(step Step, c Correspondence) bool {
	return stepShows(step, NewSide, c, c.NewFirst)
}

// stepShows reports whether one of a Step's Excerpts on the given side covers a
// line of the file a modification is in.
func stepShows(step Step, side Side, c Correspondence, line int) bool {
	for _, e := range step.Excerpts {
		if e.Side == side && e.Repository == c.Repository && e.File == c.File &&
			line >= e.FirstLine && line <= e.LastLine {
			return true
		}
	}
	return false
}

// showsPartOf reports whether a new-side Excerpt shows any part of a
// modification's replacement.
func showsPartOf(e Excerpt, c Correspondence) bool {
	return e.Side == NewSide && e.Repository == c.Repository && e.File == c.File &&
		c.isModification() && c.NewFirst <= e.LastLine && c.NewLast >= e.FirstLine
}

// modificationsShownBy returns the modifications a new-side Excerpt shows any
// part of, in file order. Any part, not just the start: a Step showing the tail
// of a rewrite still needs its own before-side beside it, or a signpost saying
// where that before-side went.
func (l ledger) modificationsShownBy(e Excerpt) []Correspondence {
	var out []Correspondence
	for _, c := range l.correspondences {
		if showsPartOf(e, c) {
			out = append(out, c)
		}
	}
	sortByNewFirst(out)
	return out
}

// firstShowing is the index of the first of a Step's Excerpts to show any part
// of a modification — the one place its before-side is drawn. A Step may show
// one rewrite through several ranges, and the before-side belongs to the Step,
// not to each range in it.
func firstShowing(step Step, c Correspondence) int {
	for i, e := range step.Excerpts {
		if showsPartOf(e, c) {
			return i
		}
	}
	return -1
}

func sortByNewFirst(cs []Correspondence) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && cs[j].NewFirst < cs[j-1].NewFirst; j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}

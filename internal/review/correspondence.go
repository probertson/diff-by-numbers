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

// riddenAlong reports whether a before-side Changed Line is accounted for by a
// Step that shows the after-side of the edit which removed it. Only a
// modification rides along, and only when a Step's new-side Excerpt covers the
// start of the replacement — the very condition under which the before-side is
// rendered, so "shown" and "accounted for" stay in lockstep and no line can be
// counted covered without also being seen.
func (l ledger) riddenAlong(line ChangedLine, steps []Step) bool {
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
		if anyStep(steps, func(s Step) bool { return stepShowsNewStart(s, c) }) {
			return true
		}
	}
	return false
}

// stepShowsNewStart reports whether a Step has a new-side Excerpt covering the
// first line a correspondence's replacement occupies — where its before-side is
// injected when rendered.
func stepShowsNewStart(step Step, c Correspondence) bool {
	for _, e := range step.Excerpts {
		if e.Side == NewSide && e.Repository == c.Repository && e.File == c.File &&
			c.NewFirst >= e.FirstLine && c.NewFirst <= e.LastLine {
			return true
		}
	}
	return false
}

// correspondencesFor returns the modifications on a file whose before-side is
// injected when the given new-side Excerpt is rendered: those whose replacement
// begins within the Excerpt's range, in file order.
func (l ledger) modificationsShownBy(e Excerpt) []Correspondence {
	var out []Correspondence
	for _, c := range l.correspondences {
		if c.Repository == e.Repository && c.File == e.File && c.isModification() &&
			c.NewFirst >= e.FirstLine && c.NewFirst <= e.LastLine {
			out = append(out, c)
		}
	}
	sortByNewFirst(out)
	return out
}

// beforeRangeShown reports whether [first, last] lies within the removed range of
// some edit whose before-side the given new-side Excerpt renders — the check that
// a before-side selection anchors code the Excerpt actually showed.
func (l ledger) beforeRangeShown(e Excerpt, first, last int) bool {
	for _, c := range l.modificationsShownBy(e) {
		if first >= c.OldFirst && last <= c.OldLast {
			return true
		}
	}
	return false
}

func sortByNewFirst(cs []Correspondence) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && cs[j].NewFirst < cs[j-1].NewFirst; j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}

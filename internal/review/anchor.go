package review

import (
	"fmt"
	"path/filepath"
	"strings"
)

// AnchorTarget names a line range the Reviewer selected within the current Step:
// which Excerpt, the first and last line, and the Side of the selected rows. A
// new-side Excerpt renders as a unified diff, so a selection may land on the
// after-side or on a before-side row shown for context; an empty Side means the
// Excerpt's own side.
type AnchorTarget struct {
	ExcerptIndex int
	FirstLine    int
	LastLine     int
	Side         Side
}

// Anchor is dbn's composed reference to a selected line range. It exists because
// the expensive part of raising a point during review is the pointing, not the
// saying: an Anchor carries the repository, file, lines, the Step it sits in, and
// the code itself, so pasting it into a chat needs no further explanation and it
// still makes sense after the agent's context has been compacted.
type Anchor struct {
	Repository string
	File       string
	Side       Side
	FirstLine  int
	LastLine   int
	StepName   string
	Lines      []Line
}

// Anchor composes an Anchor for a selection within the Step in view.
func (s *Session) Anchor(target AnchorTarget) (Anchor, error) {
	if s.walkthrough == nil {
		return Anchor{}, reject(RejectedNoWalkthrough, "there is no Walkthrough to anchor into")
	}
	if s.position == 0 {
		return Anchor{}, reject(RejectedNoSuchStep, "you can only anchor within a Step, not the Brief")
	}

	step := s.walkthrough.Steps[s.position-1]
	if target.ExcerptIndex < 0 || target.ExcerptIndex >= len(step.Excerpts) {
		return Anchor{}, reject(RejectedBadSelection,
			"Step %d has no Excerpt %d", s.position, target.ExcerptIndex)
	}
	excerpt := step.Excerpts[target.ExcerptIndex]
	side := target.Side
	if side == "" {
		side = excerpt.Side
	}

	for _, file := range s.staleFiles(step) {
		if file == excerpt.File {
			return Anchor{}, reject(RejectedStaleContent,
				"%s changed since the Walkthrough was accepted; ask the agent to re-plan before anchoring it", excerpt.File)
		}
	}

	if target.LastLine < target.FirstLine {
		return Anchor{}, reject(RejectedBadSelection,
			"the selection ends at line %d, before it starts at %d", target.LastLine, target.FirstLine)
	}
	// A selection on the Excerpt's own side must lie within it. A before-side row
	// shown for context inside a new-side Excerpt carries old-file line numbers
	// unrelated to the Excerpt's range, so it is instead bounded by the removed
	// range of an edit the Excerpt actually renders.
	if side == excerpt.Side {
		if target.FirstLine < excerpt.FirstLine || target.LastLine > excerpt.LastLine {
			return Anchor{}, reject(RejectedBadSelection,
				"the selection %d-%d lies outside the Excerpt's %d-%d", target.FirstLine, target.LastLine, excerpt.FirstLine, excerpt.LastLine)
		}
	} else if !s.ledger.beforeRangeShown(excerpt, target.FirstLine, target.LastLine) {
		return Anchor{}, reject(RejectedBadSelection,
			"the before-side selection %d-%d is not part of a change shown in this Excerpt", target.FirstLine, target.LastLine)
	}

	// Resolve exactly the selected range on the selected side, so a before-side
	// selection reads the before-side, not the after-side that shares its numbers.
	selection := Excerpt{
		Repository: excerpt.Repository, File: excerpt.File, Side: side,
		FirstLine: target.FirstLine, LastLine: target.LastLine,
	}
	lines, err := s.resolver.Resolve(selection)
	if err != nil {
		return Anchor{}, fmt.Errorf("could not read the selected code: %w", err)
	}
	for i := range lines {
		lines[i].Side = side
		lines[i].Changed = s.ledger.isChanged(excerpt.Repository, excerpt.File, side, lines[i].Number)
	}

	return Anchor{
		Repository: excerpt.Repository,
		File:       excerpt.File,
		Side:       side,
		FirstLine:  target.FirstLine,
		LastLine:   target.LastLine,
		StepName:   step.Name,
		Lines:      lines,
	}, nil
}

// Render produces the paste-ready text. It leads with a line the agent can read
// at a glance and follows with the code, each line numbered and marked so a
// changed line is unambiguous even stripped of colour.
func (a Anchor) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Re: %s:%d-%d (%s side) — Step \"%s\" in %s\n\n",
		a.File, a.FirstLine, a.LastLine, a.Side, a.StepName, filepath.Base(a.Repository))
	for _, line := range a.Lines {
		marker := " "
		if line.Changed {
			marker = "+"
			if a.Side == OldSide {
				marker = "-"
			}
		}
		fmt.Fprintf(&b, "%s %5d | %s\n", marker, line.Number, strings.ReplaceAll(line.Text, "\t", "    "))
	}
	return b.String()
}

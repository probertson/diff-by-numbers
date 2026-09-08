package review

import (
	"fmt"
	"path/filepath"
	"strings"
)

// AnchorTarget names a line range the Reviewer selected within the current Step:
// which Excerpt, and the first and last line.
type AnchorTarget struct {
	ExcerptIndex int
	FirstLine    int
	LastLine     int
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
	if target.FirstLine < excerpt.FirstLine || target.LastLine > excerpt.LastLine {
		return Anchor{}, reject(RejectedBadSelection,
			"the selection %d-%d lies outside the Excerpt's %d-%d", target.FirstLine, target.LastLine, excerpt.FirstLine, excerpt.LastLine)
	}

	lines, err := s.resolver.Resolve(excerpt)
	if err != nil {
		return Anchor{}, fmt.Errorf("could not read the selected code: %w", err)
	}

	selected := make([]Line, 0, target.LastLine-target.FirstLine+1)
	for _, line := range lines {
		if line.Number >= target.FirstLine && line.Number <= target.LastLine {
			line.Changed = s.ledger.isChanged(excerpt.Repository, excerpt.File, excerpt.Side, line.Number)
			selected = append(selected, line)
		}
	}

	return Anchor{
		Repository: excerpt.Repository,
		File:       excerpt.File,
		Side:       excerpt.Side,
		FirstLine:  target.FirstLine,
		LastLine:   target.LastLine,
		StepName:   step.Name,
		Lines:      selected,
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

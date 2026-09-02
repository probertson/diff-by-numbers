package review

import (
	"fmt"
	"strings"
)

// Dump renders the posted Walkthrough as plain text. It exists so the round trip
// from the Authoring Agent into dbn is verifiable before any terminal UI exists,
// and it shows only what the agent supplied — Excerpts appear as the ranges they
// are, not as resolved code, because resolving them is the renderer's job.
func (s *Session) Dump() string {
	if s.walkthrough == nil {
		return "no Walkthrough is posted\n"
	}

	w := s.walkthrough
	var out strings.Builder

	fmt.Fprintf(&out, "Ask:        %s\n", w.Brief.Ask)
	fmt.Fprintf(&out, "Approach:   %s\n", w.Brief.Approach)
	fmt.Fprintf(&out, "Provenance: %s", w.Brief.Provenance.Kind)
	if w.Brief.Provenance.Citation != "" {
		fmt.Fprintf(&out, " (%s)", w.Brief.Provenance.Citation)
	}
	out.WriteString("\n\nChange Set:\n")
	for _, repository := range w.ChangeSet.Repositories {
		fmt.Fprintf(&out, "  %s @ %s\n", repository.Root, repository.Range)
	}

	fmt.Fprintf(&out, "\n%s:\n", pluralize(len(w.Steps), "Step"))
	for i, step := range w.Steps {
		fmt.Fprintf(&out, "\n  Step %d of %d: %s\n", i+1, len(w.Steps), step.Name)
		fmt.Fprintf(&out, "    %s\n", step.Explanation)
		if step.OversizeJustification != "" {
			fmt.Fprintf(&out, "    oversized because: %s\n", step.OversizeJustification)
		}
		for _, excerpt := range step.Excerpts {
			fmt.Fprintf(&out, "    %s %s:%d-%d (%s side)\n",
				excerpt.Repository, excerpt.File, excerpt.FirstLine, excerpt.LastLine, excerpt.Side)
		}
	}
	return out.String()
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

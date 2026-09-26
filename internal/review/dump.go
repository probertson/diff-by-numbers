package review

import (
	"fmt"
	"strings"
)

// Dump renders the posted Round as plain text. It exists so the round trip
// from the Authoring Agent into dbn is verifiable before any terminal UI exists,
// and it shows only what the agent supplied — Excerpts appear as the ranges they
// are, not as resolved code, because resolving them is the renderer's job.
func (s *Session) Dump() string {
	if s.current == nil {
		return "no Round is posted\n"
	}

	w := s.current
	var out strings.Builder

	fmt.Fprintf(&out, "Goal:     %s\n", w.Brief.Goal)
	fmt.Fprintf(&out, "Approach: %s\n", w.Brief.Approach)
	for _, question := range w.Questions {
		fmt.Fprintf(&out, "Asks:     %s\n", question.Text)
	}
	out.WriteString("\nChange Set:\n")
	for _, repository := range w.ChangeSet.Repositories {
		fmt.Fprintf(&out, "  %s @ %s\n", repository.Root, repository.Base)
	}

	fmt.Fprintf(&out, "\n%s:\n", pluralize(len(w.Steps), "Step"))
	for i, step := range w.Steps {
		fmt.Fprintf(&out, "\n  Step %d of %d: %s\n", i+1, len(w.Steps), step.Name)
		fmt.Fprintf(&out, "    %s\n", step.Explanation)
		if step.OversizeJustification != "" {
			fmt.Fprintf(&out, "    oversized because: %s\n", step.OversizeJustification)
		}
		for _, question := range step.Questions {
			fmt.Fprintf(&out, "    asks: %s\n", question.Text)
		}
		for _, excerpt := range step.Excerpts {
			fmt.Fprintf(&out, "    %s %s:%d-%d (%s side)\n",
				excerpt.Repository, excerpt.File, excerpt.FirstLine, excerpt.LastLine, excerpt.Side)
		}
		for _, ack := range step.Acknowledgements {
			fmt.Fprintf(&out, "    acknowledged in %s: %s (%s)\n",
				ack.Repository, strings.Join(ack.Files, ", "), ack.Reason)
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

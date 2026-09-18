package review

import (
	"fmt"
	"path/filepath"
	"strings"
)

// AnchorEndpoint names one row of a Step's rendering: the Side it belongs to and
// its line number on that side. Both are needed because a new-side Excerpt renders
// as a unified diff, where a before-side row and an after-side row can carry the
// same number. A (Side, Line) pair is unambiguous within a single Excerpt, since
// no two edits can claim the same removed line. An empty Side means the Excerpt's
// own side.
type AnchorEndpoint struct {
	Side Side
	Line int
}

// AnchorTarget names the run of rendered rows the Reviewer selected: which Excerpt
// it lies in, and the rows at each end of it. It is the two ends rather than the
// range itself because the run may cross from the before-side to the after-side,
// and dbn — not the client — is the authority on what order those rows are in.
//
// Acknowledgement, when set, says the run lies in acknowledged code the Reviewer
// expanded: ExcerptIndex then counts into that Acknowledgement's expansion rather
// than the Step's own Excerpts.
type AnchorTarget struct {
	Acknowledgement *int
	ExcerptIndex    int
	Start, End      AnchorEndpoint
}

// AnchorSegment is one side-qualified run of lines within an Anchor. An Anchor
// selected out of a unified diff alternates between the sides, so its extent is a
// list of these rather than a single range; a one-sided Anchor is the list of one.
type AnchorSegment struct {
	Side      Side
	FirstLine int
	LastLine  int
}

// Anchor is dbn's composed reference to a selected run of rendered lines. It
// exists because the expensive part of raising a point during review is the
// pointing, not the saying: an Anchor carries the repository, file, lines, the
// Step it sits in, and the code itself, so pasting it into a chat needs no
// further explanation and it still makes sense after the agent's context has been
// compacted.
//
// Its extent may cross sides, because the Reviewer reads a unified diff and a
// point is often about the removal and its replacement together (#57).
type Anchor struct {
	Repository string
	File       string
	Segments   []AnchorSegment
	StepName   string
	Lines      []Line
	// AcknowledgementReason is set when the Anchor lies in acknowledged code: a
	// point raised there disputes the Acknowledgement's "mechanical" claim as well
	// as the line, so the Anchor carries the claim it disputes.
	AcknowledgementReason string
}

// Anchor composes an Anchor for a selection within the Step in view. The run
// between the two endpoints is taken from the Excerpt's own rendering, so an
// Anchor is always a contiguous run of rows the Reviewer actually saw.
func (s *Session) Anchor(target AnchorTarget) (Anchor, error) {
	if s.walkthrough == nil {
		return Anchor{}, reject(RejectedNoWalkthrough, "there is no Walkthrough to anchor into")
	}
	if s.position == 0 {
		return Anchor{}, reject(RejectedNoSuchStep, "you can only anchor within a Step, not the Brief")
	}

	step := s.walkthrough.Steps[s.position-1]
	excerpt, reason, err := s.anchoredExcerpt(step, target)
	if err != nil {
		return Anchor{}, err
	}

	for _, file := range s.staleFiles(step) {
		if file == excerpt.File {
			return Anchor{}, reject(RejectedStaleContent,
				"%s changed since the Walkthrough was accepted; ask the agent to re-plan before anchoring it", excerpt.File)
		}
	}

	// The Excerpt's rendering is the authority on which rows exist and what order
	// they are in, so the selection is resolved against it rather than re-derived.
	view := s.resolveExcerpt(excerpt)
	if view.Problem != "" {
		return Anchor{}, fmt.Errorf("could not read the selected code: %s", view.Problem)
	}

	start, err := rowIndex(view.Lines, target.Start, excerpt)
	if err != nil {
		return Anchor{}, err
	}
	end, err := rowIndex(view.Lines, target.End, excerpt)
	if err != nil {
		return Anchor{}, err
	}
	// The ends are rows, not bounds, so which was selected first carries no meaning.
	if start > end {
		start, end = end, start
	}

	lines := append([]Line(nil), view.Lines[start:end+1]...)
	// A stand-in row for code that could not be read is honest on screen but would
	// be a fabrication in an Anchor, which is quoted as source and acted on.
	for _, line := range lines {
		if line.Unreadable {
			return Anchor{}, fmt.Errorf("could not read the selected code: %s", line.Text)
		}
	}
	return Anchor{
		Repository: excerpt.Repository,
		File:       excerpt.File,
		Segments:   segmentsOf(lines),
		StepName:   step.Name,
		Lines:      lines,

		AcknowledgementReason: reason,
	}, nil
}

// anchoredExcerpt finds the Excerpt a selection lies in — one of the Step's own,
// or one an Acknowledgement expands into — and, for the latter, the reason the
// Acknowledgement gave. The expansion is derived by the same helper that drew it,
// so what the Reviewer saw and what the Anchor resolves against cannot drift.
func (s *Session) anchoredExcerpt(step Step, target AnchorTarget) (Excerpt, string, error) {
	if target.Acknowledgement == nil {
		if target.ExcerptIndex < 0 || target.ExcerptIndex >= len(step.Excerpts) {
			return Excerpt{}, "", reject(RejectedBadSelection,
				"Step %d has no Excerpt %d", s.position, target.ExcerptIndex)
		}
		return step.Excerpts[target.ExcerptIndex], "", nil
	}

	ackIndex := *target.Acknowledgement
	if ackIndex < 0 || ackIndex >= len(step.Acknowledgements) {
		return Excerpt{}, "", reject(RejectedBadSelection,
			"Step %d has no Acknowledgement %d", s.position, ackIndex+1)
	}
	ack := step.Acknowledgements[ackIndex]
	parts := s.acknowledgedParts(ack)
	if target.ExcerptIndex < 0 || target.ExcerptIndex >= len(parts) {
		return Excerpt{}, "", reject(RejectedBadSelection,
			"Acknowledgement %d of Step %d has no Excerpt %d", ackIndex+1, s.position, target.ExcerptIndex)
	}
	part := parts[target.ExcerptIndex]
	if part.opaque != nil {
		return Excerpt{}, "", reject(RejectedBadSelection,
			"%s is an Opaque Change (%s) with no lines to select", part.opaque.File, part.opaque.Kind)
	}
	return part.excerpt, ack.Reason, nil
}

// rowIndex finds the endpoint among an Excerpt's rendered rows. An endpoint that
// names no row is a selection of something never on screen, which is a rejection
// rather than a silent nearest-match.
func rowIndex(lines []Line, endpoint AnchorEndpoint, excerpt Excerpt) (int, error) {
	side := endpoint.Side
	if side == "" {
		side = excerpt.Side
	}
	for i, line := range lines {
		if line.Side == side && line.Number == endpoint.Line {
			return i, nil
		}
	}
	return 0, reject(RejectedBadSelection,
		"this Excerpt shows no %s-side line %d", side, endpoint.Line)
}

// segmentsOf collapses a run of rendered lines into the side-qualified ranges it
// covers, in the order they were rendered.
func segmentsOf(lines []Line) []AnchorSegment {
	var out []AnchorSegment
	for _, line := range lines {
		if n := len(out); n > 0 && out[n-1].Side == line.Side && out[n-1].LastLine+1 == line.Number {
			out[n-1].LastLine = line.Number
			continue
		}
		out = append(out, AnchorSegment{Side: line.Side, FirstLine: line.Number, LastLine: line.Number})
	}
	return out
}

// Location says where the Anchor points, in the Reviewer's terms rather than
// dbn's: "src/fetch.ts — before 651 — after 691-697". It names both sides because
// a before-side number is a line that no longer exists on disk, and an agent
// handed only the numbers would otherwise edit the wrong place. The sides are
// held apart by a dash rather than a comma, because a comma already separates the
// several ranges one side can carry: "before 651, 700 — after 691-710".
func (a Anchor) Location() string {
	parts := []string{a.File}
	for _, group := range []struct {
		side  Side
		label string
	}{{OldSide, "before"}, {NewSide, "after"}} {
		if ranges := rangesOn(a.Segments, group.side); ranges != "" {
			parts = append(parts, group.label+" "+ranges)
		}
	}
	return strings.Join(parts, " — ")
}

// rangesOn lists one side's line ranges, merging runs that meet — the after-side
// of a selection arrives as several segments when before-side rows are injected
// between them, but it reads as the one range it is.
func rangesOn(segments []AnchorSegment, side Side) string {
	var merged []AnchorSegment
	for _, segment := range segments {
		if segment.Side != side {
			continue
		}
		if n := len(merged); n > 0 && merged[n-1].LastLine+1 >= segment.FirstLine {
			if segment.LastLine > merged[n-1].LastLine {
				merged[n-1].LastLine = segment.LastLine
			}
			continue
		}
		merged = append(merged, segment)
	}
	var out []string
	for _, segment := range merged {
		if segment.FirstLine == segment.LastLine {
			out = append(out, fmt.Sprintf("%d", segment.FirstLine))
			continue
		}
		out = append(out, fmt.Sprintf("%d-%d", segment.FirstLine, segment.LastLine))
	}
	return strings.Join(out, ", ")
}

// Render produces the paste-ready text. It leads with a line the agent can read
// at a glance and follows with the code, each line numbered and marked so a
// changed line is unambiguous even stripped of colour. The marker comes from the
// line's own side, since the run may cross from the before-side to the after-side.
func (a Anchor) Render() string {
	var b strings.Builder
	where := fmt.Sprintf("Step %q", a.StepName)
	if a.AcknowledgementReason != "" {
		where = fmt.Sprintf("acknowledged in Step %q (%s)", a.StepName, a.AcknowledgementReason)
	}
	fmt.Fprintf(&b, "Re: %s — %s in %s\n\n", a.Location(), where, filepath.Base(a.Repository))
	for _, line := range a.Lines {
		marker := " "
		if line.Changed {
			marker = "+"
			if line.Side == OldSide {
				marker = "-"
			}
		}
		fmt.Fprintf(&b, "%s %5d | %s\n", marker, line.Number, strings.ReplaceAll(line.Text, "\t", "    "))
	}
	return b.String()
}

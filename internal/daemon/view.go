package daemon

import (
	"github.com/probertson/diff-by-numbers/internal/review"
)

// The view wire types are the private protocol between the daemon and the TUI:
// one process owns the review, the other only draws it. Exported because the
// TUI imports them; still internal to the module.

type LineWire struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
	// Side is the line's own side, since a new-side Excerpt renders as a unified
	// diff whose rows mix the after-side with the before-side it replaced.
	Side    string `json:"side,omitempty"`
	Changed bool   `json:"changed"`
}

type ExcerptWire struct {
	Repository string     `json:"repository"`
	File       string     `json:"file"`
	Side       string     `json:"side"`
	FirstLine  int        `json:"first_line"`
	LastLine   int        `json:"last_line"`
	Problem    string     `json:"problem,omitempty"`
	Lines      []LineWire `json:"lines"`
}

type AcknowledgedFileWire struct {
	Repository   string `json:"repository"`
	File         string `json:"file"`
	ChangedLines int    `json:"changed_lines"`
	Opaque       string `json:"opaque,omitempty"`
	OpaqueDetail string `json:"opaque_detail,omitempty"`
	Change       string `json:"change"`
}

type AcknowledgementWire struct {
	Reason  string                 `json:"reason"`
	Entries []AcknowledgedFileWire `json:"entries"`
}

type StepWire struct {
	Number                int                   `json:"number"`
	Name                  string                `json:"name"`
	Explanation           string                `json:"explanation"`
	OversizeJustification string                `json:"oversize_justification,omitempty"`
	Excerpts              []ExcerptWire         `json:"excerpts"`
	Acknowledgements      []AcknowledgementWire `json:"acknowledgements,omitempty"`
	Stale                 bool                  `json:"stale,omitempty"`
	StaleFiles            []string              `json:"stale_files,omitempty"`
}

type BriefWire struct {
	Ask                string `json:"ask"`
	Approach           string `json:"approach"`
	ProvenanceKind     string `json:"provenance_kind"`
	ProvenanceCitation string `json:"provenance_citation,omitempty"`
}

type RepositoryWire struct {
	Root  string `json:"root"`
	Range string `json:"range"`
}

type CoverageWire struct {
	Seen  int `json:"seen"`
	Total int `json:"total"`
}

type DispositionWire struct {
	CommentID int    `json:"comment_id"`
	Status    string `json:"status"`
	Response  string `json:"response,omitempty"`
	Note      string `json:"note"`
	Location  string `json:"location"`
}

type ViewWire struct {
	Posted       bool              `json:"posted"`
	Posting      int               `json:"posting"`
	ReviewID     string            `json:"review_id,omitempty"`
	Brief        BriefWire         `json:"brief"`
	StepNames    []string          `json:"step_names"`
	StepCount    int               `json:"step_count"`
	Position     int               `json:"position"`
	Step         *StepWire         `json:"step,omitempty"`
	Coverage     CoverageWire      `json:"coverage"`
	Repositories []RepositoryWire  `json:"repositories"`
	Seen         []bool            `json:"seen"`
	StepStatuses []string          `json:"step_statuses"`
	Comments     []CommentWire     `json:"comments"`
	Finished     bool              `json:"finished"`
	Concluded    bool              `json:"concluded"`
	Dispositions []DispositionWire `json:"dispositions,omitempty"`
}

// SegmentWire is one side-qualified range of an Anchor's extent. An Anchor taken
// from a unified diff crosses sides, so its extent travels as a list.
type SegmentWire struct {
	Side      string `json:"side"`
	FirstLine int    `json:"first_line"`
	LastLine  int    `json:"last_line"`
}

type CommentWire struct {
	ID       int           `json:"id"`
	Step     int           `json:"step"`
	File     string        `json:"file"`
	Segments []SegmentWire `json:"segments"`
	Location string        `json:"location"`
	Anchor   string        `json:"anchor"`
	Note     string        `json:"note"`
	// Acknowledgement is the index, within its Step, of the Acknowledgement the
	// Comment was raised in, absent for the Step's own code.
	Acknowledgement *int `json:"acknowledgement,omitempty"`
}

// ReRaised reports whether the Comment was carried over from a previous round
// rather than raised on a Step of this one, which is what a Step of 0 encodes.
func (comment CommentWire) ReRaised() bool { return comment.Step == 0 }

// Covers reports whether the Comment is anchored over a row of the
// rendering. It tests every segment, so a Comment spanning a removal and
// its replacement is found from either side.
func (comment CommentWire) Covers(file, side string, line int) bool {
	if comment.File != file {
		return false
	}
	for _, segment := range comment.Segments {
		if segment.Side == side && line >= segment.FirstLine && line <= segment.LastLine {
			return true
		}
	}
	return false
}

func toSegmentWires(segments []review.AnchorSegment) []SegmentWire {
	out := make([]SegmentWire, 0, len(segments))
	for _, segment := range segments {
		out = append(out, SegmentWire{
			Side: string(segment.Side), FirstLine: segment.FirstLine, LastLine: segment.LastLine,
		})
	}
	return out
}

func toViewWire(v review.ViewModel) ViewWire {
	wire := ViewWire{
		Posted:   v.Posted,
		Posting:  v.Posting,
		ReviewID: v.ReviewID,
		Brief: BriefWire{
			Ask:                v.Brief.Ask,
			Approach:           v.Brief.Approach,
			ProvenanceKind:     string(v.Brief.Provenance.Kind),
			ProvenanceCitation: v.Brief.Provenance.Citation,
		},
		StepNames: v.StepNames,
		StepCount: v.StepCount,
		Position:  v.Position,
		Coverage:  CoverageWire{Seen: v.Coverage.Seen, Total: v.Coverage.Total},
		Seen:      v.Seen,
		Finished:  v.Finished,
		Concluded: v.Concluded,
	}
	for _, st := range v.StepStatuses {
		wire.StepStatuses = append(wire.StepStatuses, string(st))
	}
	for _, comment := range v.Comments {
		wire.Comments = append(wire.Comments, CommentWire{
			ID: comment.ID, Step: comment.Step,
			File:     comment.Anchor.File,
			Segments: toSegmentWires(comment.Anchor.Segments),
			Location: comment.Anchor.Location(),
			Anchor:   comment.Anchor.Render(),
			Note:     comment.Note,

			Acknowledgement: comment.Anchor.Acknowledgement,
		})
	}
	for _, repository := range v.Repositories {
		wire.Repositories = append(wire.Repositories, RepositoryWire{Root: repository.Root, Range: repository.Range})
	}
	for _, disposition := range v.Dispositions {
		comment := disposition.Comment
		wire.Dispositions = append(wire.Dispositions, DispositionWire{
			CommentID: comment.ID,
			Status:    string(disposition.Status),
			Response:  disposition.Response,
			Note:      comment.Note,
			Location:  comment.Anchor.Location(),
		})
	}
	if v.Step != nil {
		step := StepWire{
			Number:                v.Step.Number,
			Name:                  v.Step.Name,
			Explanation:           v.Step.Explanation,
			OversizeJustification: v.Step.OversizeJustification,
			Stale:                 v.Step.Stale,
			StaleFiles:            v.Step.StaleFiles,
		}
		for _, excerpt := range v.Step.Excerpts {
			excerptWire := ExcerptWire{
				Repository: excerpt.Excerpt.Repository,
				File:       excerpt.Excerpt.File,
				Side:       string(excerpt.Excerpt.Side),
				FirstLine:  excerpt.Excerpt.FirstLine,
				LastLine:   excerpt.Excerpt.LastLine,
				Problem:    excerpt.Problem,
			}
			for _, line := range excerpt.Lines {
				excerptWire.Lines = append(excerptWire.Lines, LineWire{Number: line.Number, Text: line.Text, Side: string(line.Side), Changed: line.Changed})
			}
			step.Excerpts = append(step.Excerpts, excerptWire)
		}
		for _, ack := range v.Step.Acknowledgements {
			ackWire := AcknowledgementWire{Reason: ack.Reason}
			for _, entry := range ack.Entries {
				ackWire.Entries = append(ackWire.Entries, AcknowledgedFileWire{
					Repository:   entry.Repository,
					File:         entry.File,
					ChangedLines: entry.ChangedLines,
					Opaque:       string(entry.Opaque),
					OpaqueDetail: entry.OpaqueDetail,
					Change:       entry.Change,
				})
			}
			step.Acknowledgements = append(step.Acknowledgements, ackWire)
		}
		wire.Step = &step
	}
	return wire
}

// toExcerptWires renders resolved Excerpts (as from an Acknowledgement expansion)
// for the TUI, reusing the same shape a Step's Excerpts take.
func toExcerptWires(views []review.ExcerptView) []ExcerptWire {
	wires := make([]ExcerptWire, 0, len(views))
	for _, view := range views {
		wire := ExcerptWire{
			Repository: view.Excerpt.Repository,
			File:       view.Excerpt.File,
			Side:       string(view.Excerpt.Side),
			FirstLine:  view.Excerpt.FirstLine,
			LastLine:   view.Excerpt.LastLine,
			Problem:    view.Problem,
		}
		for _, line := range view.Lines {
			wire.Lines = append(wire.Lines, LineWire{Number: line.Number, Text: line.Text, Side: string(line.Side), Changed: line.Changed})
		}
		wires = append(wires, wire)
	}
	return wires
}

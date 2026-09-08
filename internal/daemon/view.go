package daemon

import (
	"fmt"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// The view wire types are the private protocol between the daemon and the TUI:
// one process owns the review, the other only draws it. Exported because the
// TUI imports them; still internal to the module.

type LineWire struct {
	Number  int    `json:"number"`
	Text    string `json:"text"`
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

type ViewWire struct {
	Posted         bool                `json:"posted"`
	Brief          BriefWire           `json:"brief"`
	StepNames      []string            `json:"step_names"`
	StepCount      int                 `json:"step_count"`
	Position       int                 `json:"position"`
	Step           *StepWire           `json:"step,omitempty"`
	Coverage       CoverageWire        `json:"coverage"`
	Repositories   []RepositoryWire    `json:"repositories"`
	Seen           []bool              `json:"seen"`
	StepStatuses   []string            `json:"step_statuses"`
	ChangeRequests []ChangeRequestWire `json:"change_requests"`
	Finished       bool                `json:"finished"`
}

type ChangeRequestWire struct {
	ID        int    `json:"id"`
	Step      int    `json:"step"`
	File      string `json:"file"`
	FirstLine int    `json:"first_line"`
	LastLine  int    `json:"last_line"`
	Location  string `json:"location"`
	Anchor    string `json:"anchor"`
	Note      string `json:"note"`
}

func toViewWire(v review.ViewModel) ViewWire {
	wire := ViewWire{
		Posted: v.Posted,
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
	}
	for _, st := range v.StepStatuses {
		wire.StepStatuses = append(wire.StepStatuses, string(st))
	}
	for _, cr := range v.ChangeRequests {
		wire.ChangeRequests = append(wire.ChangeRequests, ChangeRequestWire{
			ID: cr.ID, Step: cr.Step,
			File: cr.Anchor.File, FirstLine: cr.Anchor.FirstLine, LastLine: cr.Anchor.LastLine,
			Location: fmt.Sprintf("%s:%d-%d", cr.Anchor.File, cr.Anchor.FirstLine, cr.Anchor.LastLine),
			Anchor:   cr.Anchor.Render(),
			Note:     cr.Note,
		})
	}
	for _, repository := range v.Repositories {
		wire.Repositories = append(wire.Repositories, RepositoryWire{Root: repository.Root, Range: repository.Range})
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
				excerptWire.Lines = append(excerptWire.Lines, LineWire{Number: line.Number, Text: line.Text, Changed: line.Changed})
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
			wire.Lines = append(wire.Lines, LineWire{Number: line.Number, Text: line.Text, Changed: line.Changed})
		}
		wires = append(wires, wire)
	}
	return wires
}

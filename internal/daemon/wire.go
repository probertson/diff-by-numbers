// Package daemon exposes the review core over MCP. It is deliberately thin: it
// translates wire payloads into core calls and back, and holds no review logic
// of its own. Logic accreting here is the signal it belongs in the core.
package daemon

import (
	"fmt"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// The wire types are separate from the domain types on purpose. Their struct
// tags are the contract the Authoring Agent is held to — a schema it cannot
// drift from, where a prose instruction is only a hope.

type wireProvenance struct {
	Kind     string `json:"kind" jsonschema:"Where this account of intent came from: 'stated' if you were there or read the session transcript, 'inferred' if you reverse-engineered it from the changes themselves"`
	Citation string `json:"citation,omitempty" jsonschema:"What backs a stated Provenance, such as a session or prompt reference. Required when kind is 'stated'"`
}

type wireBrief struct {
	Ask        string         `json:"ask" jsonschema:"What the Reviewer originally asked for, in their terms"`
	Approach   string         `json:"approach" jsonschema:"The approach you took, so the Reviewer can judge it separately from the code implementing it"`
	Provenance wireProvenance `json:"provenance"`
}

type wireExcerpt struct {
	Repository string `json:"repository" jsonschema:"Root path of the repository this Excerpt is in. Must be one named in the Change Set"`
	File       string `json:"file" jsonschema:"Path to the file, relative to the repository root"`
	Side       string `json:"side" jsonschema:"'new' for the after-side of a change — added or edited lines, and unchanged context. Point at the after-side of an edit and dbn shows the before-side it replaced automatically; you need not name the old side. Use 'old' only to show a standalone deletion: removed lines that nothing replaced"`
	FirstLine  int    `json:"first_line" jsonschema:"First line of the range, counting from 1"`
	LastLine   int    `json:"last_line" jsonschema:"Last line of the range, inclusive"`
}

type wireAcknowledgement struct {
	Repository string   `json:"repository" jsonschema:"Root path of the repository these files are in. Must be one named in the Change Set"`
	Files      []string `json:"files" jsonschema:"Paths, relative to the repository root, whose entire change is mechanical. Every changed line and every binary, mode or rename change in these files is thereby accounted for"`
	Reason     string   `json:"reason" jsonschema:"One line saying why these changes are mechanical and need not be read, e.g. 'regenerated lockfile'. The Reviewer sees this and may expand it into the real code"`
}

type wireStep struct {
	Name                  string                `json:"name" jsonschema:"The single self-contained idea this Step contains, named so the Reviewer knows what they are about to look at"`
	Explanation           string                `json:"explanation" jsonschema:"What changed here and why. This is the reason the Reviewer is not reading a bare diff"`
	Excerpts              []wireExcerpt         `json:"excerpts,omitempty" jsonschema:"The line ranges to show. Send ranges, never code: dbn reads the bytes from the working tree itself. A Step needs at least one Excerpt or one Acknowledgement"`
	Acknowledgements      []wireAcknowledgement `json:"acknowledgements,omitempty" jsonschema:"Files whose changes are mechanical and covered without reading, in place of an Excerpt. The only way to account for a binary file, a mode change or a pure rename, which have no lines to show"`
	OversizeJustification string                `json:"oversize_justification,omitempty" jsonschema:"Why this Step exceeds the size budget, if it does. dbn never refuses a large Step, it only asks for a reason"`
}

type wireRepository struct {
	Root  string `json:"root" jsonschema:"Absolute path to the repository root. Never assume the session's working directory is one"`
	Range string `json:"range" jsonschema:"The range under review in this repository"`
}

type wireDisposition struct {
	ChangeRequestID int    `json:"change_request_id" jsonschema:"The id of a Change Request from the previous round this accounts for"`
	Status          string `json:"status" jsonschema:"'addressed' if you made the change, 'declined' if you did not"`
	Reasoning       string `json:"reasoning,omitempty" jsonschema:"Why you declined, in one line. Required when status is 'declined': the Reviewer sees it before any code and may re-raise the request"`
}

type wireWalkthrough struct {
	Brief        wireBrief         `json:"brief"`
	Repositories []wireRepository  `json:"repositories" jsonschema:"Every repository this Walkthrough covers. A Walkthrough may span several"`
	Steps        []wireStep        `json:"steps" jsonschema:"The Steps, ordered so each is comprehensible given only the Steps before it"`
	Dispositions []wireDisposition `json:"dispositions,omitempty" jsonschema:"When this is a Revision Round posted after a finish, one entry per Change Request the previous round raised, saying whether you addressed or declined it. Omit for a first Walkthrough"`
}

type postResult struct {
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty" jsonschema:"Why the Walkthrough was refused, as a value you can act on"`
	Detail   string `json:"detail,omitempty" jsonschema:"What specifically to fix"`
}

type changeRequestWire struct {
	ID       int    `json:"id"`
	Step     int    `json:"step" jsonschema:"The Step number this Change Request was raised on"`
	Location string `json:"location" jsonschema:"Where in the code it points: file, line range, and side"`
	Anchor   string `json:"anchor" jsonschema:"The full anchored context, ready to act on: the code and where it lives"`
	Note     string `json:"note" jsonschema:"What the Reviewer asked for"`
}

type stepReportWire struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
	Status string `json:"status" jsonschema:"unseen, seen, or flagged"`
}

type fetchResult struct {
	Posted         bool                `json:"posted" jsonschema:"Whether a Walkthrough exists at all. If false, nothing was ever accepted and there is nothing to wait for"`
	Finished       bool                `json:"finished" jsonschema:"Whether the Reviewer has completed the Walkthrough"`
	Message        string              `json:"message"`
	Ask            string              `json:"ask" jsonschema:"What the review was originally about, so you can re-ground yourself if your context has moved on"`
	Approach       string              `json:"approach"`
	ChangeRequests []changeRequestWire `json:"change_requests"`
	Steps          []stepReportWire    `json:"steps" jsonschema:"Every Step and its final disposition: unseen, seen, or flagged"`
}

func (w wireWalkthrough) toDomain() review.Walkthrough {
	repositories := make([]review.Repository, 0, len(w.Repositories))
	for _, r := range w.Repositories {
		repositories = append(repositories, review.Repository{Root: r.Root, Range: r.Range})
	}

	steps := make([]review.Step, 0, len(w.Steps))
	for _, s := range w.Steps {
		excerpts := make([]review.Excerpt, 0, len(s.Excerpts))
		for _, e := range s.Excerpts {
			excerpts = append(excerpts, review.Excerpt{
				Repository: e.Repository,
				File:       e.File,
				Side:       review.Side(e.Side),
				FirstLine:  e.FirstLine,
				LastLine:   e.LastLine,
			})
		}
		acknowledgements := make([]review.Acknowledgement, 0, len(s.Acknowledgements))
		for _, a := range s.Acknowledgements {
			acknowledgements = append(acknowledgements, review.Acknowledgement{
				Repository: a.Repository,
				Files:      a.Files,
				Reason:     a.Reason,
			})
		}
		steps = append(steps, review.Step{
			Name:                  s.Name,
			Explanation:           s.Explanation,
			Excerpts:              excerpts,
			Acknowledgements:      acknowledgements,
			OversizeJustification: s.OversizeJustification,
		})
	}

	dispositions := make([]review.Disposition, 0, len(w.Dispositions))
	for _, d := range w.Dispositions {
		dispositions = append(dispositions, review.Disposition{
			ChangeRequestID: d.ChangeRequestID,
			Status:          review.DispositionStatus(d.Status),
			Reasoning:       d.Reasoning,
		})
	}

	return review.Walkthrough{
		Brief: review.Brief{
			Ask:      w.Brief.Ask,
			Approach: w.Brief.Approach,
			Provenance: review.Provenance{
				Kind:     review.ProvenanceKind(w.Brief.Provenance.Kind),
				Citation: w.Brief.Provenance.Citation,
			},
		},
		ChangeSet:    review.ChangeSet{Repositories: repositories},
		Steps:        steps,
		Dispositions: dispositions,
	}
}

func toFetchResult(r review.Results, message string) fetchResult {
	out := fetchResult{
		Posted:   r.Posted,
		Finished: r.Finished,
		Message:  message,
		Ask:      r.Brief.Ask,
		Approach: r.Brief.Approach,
	}
	for _, cr := range r.ChangeRequests {
		out.ChangeRequests = append(out.ChangeRequests, changeRequestWire{
			ID:       cr.ID,
			Step:     cr.Step,
			Location: fmt.Sprintf("%s:%d-%d (%s)", cr.Anchor.File, cr.Anchor.FirstLine, cr.Anchor.LastLine, cr.Anchor.Side),
			Anchor:   cr.Anchor.Render(),
			Note:     cr.Note,
		})
	}
	for _, sr := range r.StepReports {
		out.Steps = append(out.Steps, stepReportWire{Number: sr.Number, Name: sr.Name, Status: string(sr.Status)})
	}
	return out
}

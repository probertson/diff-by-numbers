// Package daemon exposes the review core over MCP. It is deliberately thin: it
// translates wire payloads into core calls and back, and holds no review logic
// of its own. Logic accreting here is the signal it belongs in the core.
package daemon

import (
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
	Root string `json:"root" jsonschema:"Absolute path to the repository root. Never assume the session's working directory is one"`
	Base string `json:"base" jsonschema:"A base ref (branch, tag or commit). dbn reviews everything from the merge-base of this ref and HEAD to the working tree, including uncommitted changes."`
}

type wireDisposition struct {
	CommentID int    `json:"comment_id" jsonschema:"The id of a Comment from the previous round this accounts for"`
	Status    string `json:"status" jsonschema:"'addressed' if you made a change, 'answered' if you responded without changing anything (as to a question), 'declined' if you will not make the change it asks for"`
	Response  string `json:"response,omitempty" jsonschema:"What you say back to the Reviewer, who sees it before any code. Required when status is 'answered' (it is the answer) or 'declined' (why you won't, in a line or two; the Reviewer may re-raise it). Optional but welcome when 'addressed', e.g. to note how you made the change"`
}

type wireWalkthrough struct {
	Brief        wireBrief         `json:"brief"`
	Repositories []wireRepository  `json:"repositories" jsonschema:"Every repository this Walkthrough covers. A Walkthrough may span several"`
	Steps        []wireStep        `json:"steps" jsonschema:"The Steps, ordered so each is comprehensible given only the Steps before it"`
	Dispositions []wireDisposition `json:"dispositions,omitempty" jsonschema:"When this is a Revision Round posted after a hand-off, one entry per Comment the previous round raised, saying whether you addressed, answered or declined it. Omit for a first Walkthrough"`
	Label        string            `json:"label,omitempty" jsonschema:"An optional short human-readable name for this review, shown to the Reviewer to tell several reviews apart, e.g. 'auth refactor'. It is not the review's id — dbn mints that — only a display aid. On a Revision Round you may omit it to keep the one you first gave"`
}

type postResult struct {
	Accepted bool   `json:"accepted"`
	ReviewID string `json:"review_id,omitempty" jsonschema:"The id dbn assigned this review. Record it: pass it to conclude when the review is fully done so dbn can release it"`
	Reason   string `json:"reason,omitempty" jsonschema:"Why the Walkthrough was refused, as a value you can act on"`
	Detail   string `json:"detail,omitempty" jsonschema:"What specifically to fix"`
}

type concludeInput struct {
	ReviewID string `json:"review_id" jsonschema:"The id of the review to conclude, as returned by post_walkthrough"`
}

type concludeResult struct {
	Concluded bool   `json:"concluded"`
	Reason    string `json:"reason,omitempty" jsonschema:"Why the conclude was refused, as a value you can act on"`
	Message   string `json:"message" jsonschema:"A human-readable account of the outcome"`
}

type commentWire struct {
	ID       int    `json:"id"`
	Step     int    `json:"step" jsonschema:"The Step number this Comment was raised on"`
	Location string `json:"location" jsonschema:"Where in the code it points: the file, and the before-side and after-side lines it covers"`
	Anchor   string `json:"anchor" jsonschema:"The full anchored context, ready to act on: the code and where it lives"`
	Note     string `json:"note" jsonschema:"What the Reviewer said: a change they want, or a question"`
	// ReRaisedFrom is only set when the Reviewer pushed back on how you resolved a
	// Comment last round.
	ReRaisedFrom int `json:"re_raised_from,omitempty" jsonschema:"Set when the Reviewer re-raised the Comment you declined or answered as #N: they did not accept your reasoning. Answer the point or change the code — repeating the same reasoning is not a response"`
}

type stepReportWire struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
	Status string `json:"status" jsonschema:"unseen, seen, or flagged"`
}

type fetchResult struct {
	Posted   bool             `json:"posted" jsonschema:"Whether a Walkthrough exists at all. If false, nothing was ever accepted and there is nothing to wait for"`
	Finished bool             `json:"finished" jsonschema:"Whether the Reviewer has handed the Walkthrough off to you"`
	Message  string           `json:"message"`
	Ask      string           `json:"ask" jsonschema:"What the review was originally about, so you can re-ground yourself if your context has moved on"`
	Approach string           `json:"approach"`
	Comments []commentWire    `json:"comments"`
	Steps    []stepReportWire `json:"steps" jsonschema:"Every Step and its final disposition: unseen, seen, or flagged"`
}

func (w wireWalkthrough) toDomain() review.Walkthrough {
	repositories := make([]review.Repository, 0, len(w.Repositories))
	for _, r := range w.Repositories {
		repositories = append(repositories, review.Repository{Root: r.Root, Base: r.Base})
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
			CommentID: d.CommentID,
			Status:    review.DispositionStatus(d.Status),
			Response:  d.Response,
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
		Label:        w.Label,
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
	for _, comment := range r.Comments {
		out.Comments = append(out.Comments, commentWire{
			ID:       comment.ID,
			Step:     comment.Step,
			Location: comment.Anchor.Location(),
			Anchor:   comment.Anchor.Render(),
			Note:     comment.Note,

			ReRaisedFrom: comment.ReRaisedFrom,
		})
	}
	for _, sr := range r.StepReports {
		out.Steps = append(out.Steps, stepReportWire{Number: sr.Number, Name: sr.Name, Status: string(sr.Status)})
	}
	return out
}

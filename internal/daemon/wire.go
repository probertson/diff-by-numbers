// Package daemon exposes the review core over MCP. It is deliberately thin: it
// translates wire payloads into core calls and back, and holds no review logic
// of its own. Logic accreting here is the signal it belongs in the core.
package daemon

import "github.com/probertson/diff-by-numbers/internal/review"

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
	Side       string `json:"side" jsonschema:"'old' for deleted lines, 'new' for added or unchanged lines"`
	FirstLine  int    `json:"first_line" jsonschema:"First line of the range, counting from 1"`
	LastLine   int    `json:"last_line" jsonschema:"Last line of the range, inclusive"`
}

type wireStep struct {
	Name                  string        `json:"name" jsonschema:"The single self-contained idea this Step contains, named so the Reviewer knows what they are about to look at"`
	Explanation           string        `json:"explanation" jsonschema:"What changed here and why. This is the reason the Reviewer is not reading a bare diff"`
	Excerpts              []wireExcerpt `json:"excerpts" jsonschema:"The line ranges to show. Send ranges, never code: dbn reads the bytes from the working tree itself"`
	OversizeJustification string        `json:"oversize_justification,omitempty" jsonschema:"Why this Step exceeds the size budget, if it does. dbn never refuses a large Step, it only asks for a reason"`
}

type wireRepository struct {
	Root  string `json:"root" jsonschema:"Absolute path to the repository root. Never assume the session's working directory is one"`
	Range string `json:"range" jsonschema:"The range under review in this repository"`
}

type wireWalkthrough struct {
	Brief        wireBrief        `json:"brief"`
	Repositories []wireRepository `json:"repositories" jsonschema:"Every repository this Walkthrough covers. A Walkthrough may span several"`
	Steps        []wireStep       `json:"steps" jsonschema:"The Steps, ordered so each is comprehensible given only the Steps before it"`
}

type postResult struct {
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty" jsonschema:"Why the Walkthrough was refused, as a value you can act on"`
	Detail   string `json:"detail,omitempty" jsonschema:"What specifically to fix"`
}

type fetchResult struct {
	Posted         bool     `json:"posted" jsonschema:"Whether a Walkthrough exists at all. If false, nothing was ever accepted and there is nothing to wait for"`
	Finished       bool     `json:"finished" jsonschema:"Whether the Reviewer has completed the Walkthrough"`
	Message        string   `json:"message"`
	ChangeRequests []string `json:"change_requests"`
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
		steps = append(steps, review.Step{
			Name:                  s.Name,
			Explanation:           s.Explanation,
			Excerpts:              excerpts,
			OversizeJustification: s.OversizeJustification,
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
		ChangeSet: review.ChangeSet{Repositories: repositories},
		Steps:     steps,
	}
}

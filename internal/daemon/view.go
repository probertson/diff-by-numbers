package daemon

import "github.com/probertson/diff-by-numbers/internal/review"

// The view wire types are the private protocol between the daemon and the TUI:
// one process owns the review, the other only draws it. Exported because the
// TUI imports them; still internal to the module.

type LineWire struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
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

type StepWire struct {
	Number                int           `json:"number"`
	Name                  string        `json:"name"`
	Explanation           string        `json:"explanation"`
	OversizeJustification string        `json:"oversize_justification,omitempty"`
	Excerpts              []ExcerptWire `json:"excerpts"`
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
	Posted       bool             `json:"posted"`
	Brief        BriefWire        `json:"brief"`
	StepNames    []string         `json:"step_names"`
	StepCount    int              `json:"step_count"`
	Position     int              `json:"position"`
	Step         *StepWire        `json:"step,omitempty"`
	Coverage     CoverageWire     `json:"coverage"`
	Repositories []RepositoryWire `json:"repositories"`
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
				excerptWire.Lines = append(excerptWire.Lines, LineWire{Number: line.Number, Text: line.Text})
			}
			step.Excerpts = append(step.Excerpts, excerptWire)
		}
		wire.Step = &step
	}
	return wire
}

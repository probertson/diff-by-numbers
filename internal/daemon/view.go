package daemon

import (
	"github.com/probertson/diff-by-numbers/internal/intraline"
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
	// Signpost marks a row that stands where a before-side would go, saying which
	// Steps draw it instead. It is not code: the cursor never lands on it, and
	// nothing may be selected, copied or commented from it.
	Signpost bool `json:"signpost,omitempty"`
	// Emphasis is the [start, end) rune ranges of the text that changed within
	// a removed line and the added line matched with it (#81). The TUI only
	// styles them.
	Emphasis [][2]int `json:"emphasis,omitempty"`
}

// toLineWire carries one rendered row to the TUI, including whether it is code
// at all — a signpost drawn as an ordinary removed line would read as deleted
// source and invite the Reviewer to quote it.
func toLineWire(line review.Line) LineWire {
	return LineWire{
		Number:   line.Number,
		Text:     line.Text,
		Side:     string(line.Side),
		Changed:  line.Changed,
		Signpost: line.Signpost,
		Emphasis: toEmphasisWire(line.Emphasis),
	}
}

func toEmphasisWire(ranges []intraline.Range) [][2]int {
	var out [][2]int
	for _, r := range ranges {
		out = append(out, [2]int{r.Start, r.End})
	}
	return out
}

type ExcerptWire struct {
	Repository string     `json:"repository"`
	File       string     `json:"file"`
	Side       string     `json:"side"`
	FirstLine  int        `json:"first_line"`
	LastLine   int        `json:"last_line"`
	Problem    string     `json:"problem,omitempty"`
	Lines      []LineWire `json:"lines"`
	// ChangedOnDisk says the file has been edited since the round was posted;
	// the lines are still the posted ones.
	ChangedOnDisk bool `json:"changed_on_disk,omitempty"`
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
	Questions             []QuestionWire        `json:"questions,omitempty"`
}

// QuestionWire is an Agent Question as the TUI draws it, with the Reviewer's
// Answer so far.
type QuestionWire struct {
	ID     int    `json:"id"`
	Step   int    `json:"step"`
	Text   string `json:"text"`
	Answer string `json:"answer,omitempty"`
	// CarriedOver marks an answered question from a Round the agent has since
	// replaced in place, which belongs to no Step of this one.
	CarriedOver bool `json:"carried_over,omitempty"`
}

// Answered reports whether the Reviewer has answered the question.
func (q QuestionWire) Answered() bool { return q.Answer != "" }

// AccountedQuestionWire is an Agent Question of the previous round as a
// Revision Round's Overview shows it: what was asked, what the Reviewer
// answered, and what the agent did about it.
type AccountedQuestionWire struct {
	Question QuestionWire `json:"question"`
	Status   string       `json:"status"`
	Response string       `json:"response,omitempty"`
}

func toQuestionWire(question review.AgentQuestion) QuestionWire {
	return QuestionWire{
		ID: question.ID, Step: question.Step, Text: question.Text, Answer: question.Answer,
		CarriedOver: question.CarriedOver,
	}
}

func toQuestionWires(questions []review.AgentQuestion) []QuestionWire {
	var out []QuestionWire
	for _, question := range questions {
		out = append(out, toQuestionWire(question))
	}
	return out
}

type BriefWire struct {
	Goal     string `json:"goal"`
	Approach string `json:"approach"`
}

type RepositoryWire struct {
	Root string `json:"root"`
	Base string `json:"base"`
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
	// Anchor is the previous round's Anchor rendered, so re-raising can open the
	// note editor over the code the point was about.
	Anchor string `json:"anchor,omitempty"`
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
	Questions    []QuestionWire    `json:"questions,omitempty"`
	Finished     bool              `json:"finished"`
	Concluded    bool              `json:"concluded"`
	Dispositions []DispositionWire `json:"dispositions,omitempty"`
	// Dismissed says the Reviewer discarded this review, so a window holding it
	// has nothing left to show.
	Dismissed bool `json:"dismissed,omitempty"`
	// AgentTold says the Authoring Agent has heard about the Hand Off on screen
	// directly, so the Reviewer need not relay it (ADR-0016). Only ever set once
	// the Round is handed off.
	AgentTold bool `json:"agent_told,omitempty"`
	// Replaced says the Round on screen replaced another in place.
	Replaced bool `json:"replaced,omitempty"`
	// RoundQuestions are the Agent Questions asked on the Round, which the
	// Overview shows with the Brief.
	RoundQuestions []QuestionWire `json:"round_questions,omitempty"`
	// AccountedQuestions are the previous round's Agent Questions, each with its
	// Answer and the status the agent gave it.
	AccountedQuestions []AccountedQuestionWire `json:"accounted_questions,omitempty"`
	// Round is which round this is; PreviousRound the one it is compared with,
	// 0 in a first round. SincePreviousRound says the code is shaded by what
	// changed since then rather than since the merge-base.
	Round              int  `json:"round"`
	PreviousRound      int  `json:"previous_round,omitempty"`
	SincePreviousRound bool `json:"since_previous_round,omitempty"`
	// Withdrawn is what the previous round had and this one removed outright;
	// UnchangedSincePrevious says, per Step, that nothing it shows changed since.
	Withdrawn              []WithdrawalWire `json:"withdrawn,omitempty"`
	UnchangedSincePrevious []bool           `json:"unchanged_since_previous,omitempty"`
}

// WithdrawalWire is a run of lines the previous round had and this one removed.
type WithdrawalWire struct {
	Repository    string   `json:"repository"`
	File          string   `json:"file"`
	After         int      `json:"after"`
	PreviousFirst int      `json:"previous_first"`
	Lines         []string `json:"lines"`
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
	// ReRaisedFrom is the previous round's Comment this one disputes, or 0 for a
	// Comment raised on this round's code.
	ReRaisedFrom int `json:"re_raised_from,omitempty"`
	// CarriedOver marks a Comment raised on a Round the agent has since
	// replaced in place, which belongs to no Step of this one.
	CarriedOver bool `json:"carried_over,omitempty"`
}

// ReRaised reports whether the Comment was carried over from a previous round
// rather than raised on a Step of this one — which is exactly the Comments that
// name the resolution they dispute.
func (comment CommentWire) ReRaised() bool { return comment.ReRaisedFrom != 0 }

// Why a Comment belongs to no Step of the Round on screen, as Stepless
// reports it.
const (
	ReRaised    = "re-raised"
	CarriedOver = "carried over"
)

// Stepless says why a Comment belongs to no Step of the Round on screen —
// re-raised from an earlier round, or carried over from a Round since
// replaced — or "" for a Comment raised on one of its Steps. A re-raise that was
// also carried over is still a re-raise: that is what the Reviewer needs to
// know about it.
func (comment CommentWire) Stepless() string {
	switch {
	case comment.ReRaised():
		return ReRaised
	case comment.CarriedOver:
		return CarriedOver
	}
	return ""
}

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
			Goal:     v.Brief.Goal,
			Approach: v.Brief.Approach,
		},
		StepNames: v.StepNames,
		StepCount: v.StepCount,
		Position:  v.Position,
		Coverage:  CoverageWire{Seen: v.Coverage.Seen, Total: v.Coverage.Total},
		Seen:      v.Seen,
		Finished:  v.Finished,
		Concluded: v.Concluded,
		Dismissed: v.Dismissed,
		Replaced:  v.Replaced,

		Questions:      toQuestionWires(v.Questions),
		RoundQuestions: toQuestionWires(v.RoundQuestions),

		Round:                  v.Round,
		PreviousRound:          v.PreviousRound,
		SincePreviousRound:     v.SincePreviousRound,
		UnchangedSincePrevious: v.UnchangedSincePrevious,
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
			ReRaisedFrom:    comment.ReRaisedFrom,
			CarriedOver:     comment.CarriedOver,
		})
	}
	for _, repository := range v.Repositories {
		wire.Repositories = append(wire.Repositories, RepositoryWire{Root: repository.Root, Base: repository.Base})
	}
	for _, disposition := range v.Dispositions {
		comment := disposition.Comment
		wire.Dispositions = append(wire.Dispositions, DispositionWire{
			CommentID: comment.ID,
			Status:    string(disposition.Status),
			Response:  disposition.Response,
			Note:      comment.Note,
			Location:  comment.Anchor.Location(),
			Anchor:    comment.Anchor.Render(),
		})
	}
	for _, accounted := range v.AccountedQuestions {
		wire.AccountedQuestions = append(wire.AccountedQuestions, AccountedQuestionWire{
			Question: toQuestionWire(accounted.Question),
			Status:   string(accounted.Status),
			Response: accounted.Response,
		})
	}
	for _, w := range v.Withdrawn {
		wire.Withdrawn = append(wire.Withdrawn, WithdrawalWire{
			Repository: w.Repository, File: w.File, After: w.After, PreviousFirst: w.PreviousFirst, Lines: w.Lines,
		})
	}
	if v.Step != nil {
		step := StepWire{
			Number:                v.Step.Number,
			Name:                  v.Step.Name,
			Explanation:           v.Step.Explanation,
			OversizeJustification: v.Step.OversizeJustification,
			Questions:             toQuestionWires(v.Step.Questions),
		}
		if len(v.Step.Excerpts) > 0 {
			step.Excerpts = toExcerptWires(v.Step.Excerpts)
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

// toExcerptWires renders resolved Excerpts for the TUI — a Step's own, or an
// Acknowledgement's expansion, which take the same shape.
func toExcerptWires(views []review.ExcerptView) []ExcerptWire {
	wires := make([]ExcerptWire, 0, len(views))
	for _, view := range views {
		wire := ExcerptWire{
			Repository:    view.Excerpt.Repository,
			File:          view.Excerpt.File,
			Side:          string(view.Excerpt.Side),
			FirstLine:     view.Excerpt.FirstLine,
			LastLine:      view.Excerpt.LastLine,
			Problem:       view.Problem,
			ChangedOnDisk: view.ChangedOnDisk,
		}
		for _, line := range view.Lines {
			wire.Lines = append(wire.Lines, toLineWire(line))
		}
		wires = append(wires, wire)
	}
	return wires
}

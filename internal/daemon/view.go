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
	// Emphasis is the [start, end) rune ranges of the text on this line that the
	// edit it belongs to removed or added (#81, #125). The TUI only styles them.
	Emphasis [][2]int `json:"emphasis,omitempty"`
	// Block is the block of a Document Excerpt the row belongs to, numbered from
	// 1 among the Excerpt's Blocks; 0 for a row in none.
	Block int `json:"block,omitempty"`
	// Path is the heading path of a row of a Document Excerpt, as dbn decides
	// it (review.Line.Path): on the row's own side, so a row an edit replaced
	// sits under the old headings, and for a row in no block that of the text
	// beside it. The TUI draws it over the row as it is, never working one out
	// (#145).
	Path []string `json:"path,omitempty"`
	// Runs are a row of an edited Document block as track changes: its text cut
	// into what the edit kept and added, with the words it struck there put
	// back. The TUI only lays them out and styles them.
	Runs []RunWire `json:"runs,omitempty"`
	// Widened marks a row of a Document Excerpt shown so that no block is read
	// in part, though the Excerpt does not name it: context, dimmed and unsigned.
	// Unlike a line Absorption adds to an Excerpt, it is added only as the
	// Excerpt is drawn, and covers nothing.
	Widened bool `json:"widened,omitempty"`
}

// RunWire is one run of an edited row: text the edit kept, struck ("removed")
// or added.
type RunWire struct {
	Text   string `json:"text"`
	Change string `json:"change"`
}

// BlockWire is one block of a Document Excerpt: what kind it is, where it is in
// the file, and how it was changed. The TUI lays it out and styles it, working
// out only the inline styling within it (markers, bullets, pipes); the block's
// structure is decided here, never again in the TUI (ADR-0005, #166).
type BlockWire struct {
	Kind  string `json:"kind"`
	Level int    `json:"level,omitempty"`
	// Diagram is, for a code block that is a Mermaid diagram, its source,
	// which the Browser Renderer draws (#191, #193); nil for any other block.
	Diagram   *DiagramSourcesWire `json:"diagram,omitempty"`
	Side      string              `json:"side"`
	FirstLine int                 `json:"first_line"`
	LastLine  int                 `json:"last_line"`
	Treatment string              `json:"treatment"`
	// Path is the block's heading path, outermost heading first, which the TUI
	// draws in the Excerpt's sticky file header (#145).
	Path []string `json:"path,omitempty"`
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
		Block:    line.Block,
		Path:     line.Path,
		Runs:     toRunWires(line.Runs),
		Widened:  line.Widened,
	}
}

func toRunWires(runs []review.WordRun) []RunWire {
	var out []RunWire
	for _, r := range runs {
		out = append(out, RunWire{Text: r.Text, Change: string(r.Change)})
	}
	return out
}

func toBlockWires(blocks []review.Block) []BlockWire {
	var out []BlockWire
	for _, b := range blocks {
		out = append(out, BlockWire{
			Kind: string(b.Kind), Level: b.Level, Diagram: toDiagramSourcesWire(b.Diagram), Side: string(b.Side),
			FirstLine: b.Lines.First, LastLine: b.Lines.Last, Treatment: string(b.Treatment),
			Path: b.Path,
		})
	}
	return out
}

// DiagramSourcesWire is a Mermaid diagram's source before and after the
// change, each line ending in a newline, "" for a side it is no diagram on
// (review.DiagramSources).
type DiagramSourcesWire struct {
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

func toDiagramSourcesWire(sources *review.DiagramSources) *DiagramSourcesWire {
	if sources == nil {
		return nil
	}
	return &DiagramSourcesWire{Before: sources.Before, After: sources.After}
}

// DiagramFileWire is an Excerpt of a Mermaid diagram file (.mmd, .mermaid)
// seen as the one diagram the whole file is (review.DiagramFile): how the
// change treated what the Excerpt shows, the file's source, and, when dbn
// could read the file on the Excerpt's side (Read), the lines it spans there.
type DiagramFileWire struct {
	Treatment string             `json:"treatment"`
	Sources   DiagramSourcesWire `json:"sources"`
	Read      bool               `json:"read,omitempty"`
	FirstLine int                `json:"first_line,omitempty"`
	LastLine  int                `json:"last_line,omitempty"`
}

func toDiagramFileWire(file *review.DiagramFile) *DiagramFileWire {
	if file == nil {
		return nil
	}
	return &DiagramFileWire{
		Treatment: string(file.Treatment), Sources: *toDiagramSourcesWire(&file.Sources),
		Read: file.Read, FirstLine: file.Lines.First, LastLine: file.Lines.Last,
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
	// Blocks are a Document Excerpt's blocks, which its rows name; none for code.
	Blocks []BlockWire `json:"blocks,omitempty"`
	// Plain says a Document Excerpt's file is prose that is not Markdown, so its
	// paragraphs are drawn without Markdown styling.
	Plain bool `json:"plain,omitempty"`
	// DiagramFile is set on an Excerpt of a Mermaid diagram file. The file is
	// code: its rows are in no block and Blocks is empty. The Browser Renderer
	// draws the diagram when the Excerpt shows all of it (#194).
	DiagramFile *DiagramFileWire `json:"diagram_file,omitempty"`
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
	// Chapter is the Chapter the Step is in, numbered from 1, or 0 in a Round
	// without Chapters.
	Chapter int `json:"chapter,omitempty"`
}

// ChapterWire is one Chapter of the Round on screen, with the Steps it holds
// named by their numbers in the Round.
type ChapterWire struct {
	Number    int    `json:"number"`
	Name      string `json:"name"`
	Intro     string `json:"intro"`
	FirstStep int    `json:"first_step"`
	LastStep  int    `json:"last_step"`
	// Seen says every Step in the Chapter has been seen; QuestionCount is how
	// many Agent Questions its Steps ask; UnchangedSincePrevious says nothing
	// any of its Steps shows changed since the previous round.
	Seen                   bool `json:"seen,omitempty"`
	QuestionCount          int  `json:"question_count,omitempty"`
	UnchangedSincePrevious bool `json:"unchanged_since_previous,omitempty"`
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
	// History is every earlier asking of a question asked again, oldest first.
	History []QuestionExchangeWire `json:"history,omitempty"`
}

// QuestionExchangeWire is one earlier asking of a question: its wording and the
// Answer it got, empty if none.
type QuestionExchangeWire struct {
	Text   string `json:"text"`
	Answer string `json:"answer,omitempty"`
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
	// AskedAgainAs is the question of this Round asking it again, when it was
	// asked again, so the Overview can say where without repeating it.
	AskedAgainAs *QuestionWire `json:"asked_again_as,omitempty"`
}

func toQuestionWire(question review.AgentQuestion) QuestionWire {
	wire := QuestionWire{
		ID: question.ID, Step: question.Step, Text: question.Text, Answer: question.Answer,
		CarriedOver: question.CarriedOver,
	}
	for _, earlier := range question.History {
		wire.History = append(wire.History, QuestionExchangeWire{Text: earlier.Text, Answer: earlier.Answer})
	}
	return wire
}

func toQuestionWires(questions []review.AgentQuestion) []QuestionWire {
	var out []QuestionWire
	for _, question := range questions {
		out = append(out, toQuestionWire(question))
	}
	return out
}

type BriefWire struct {
	Goal       string `json:"goal"`
	Background string `json:"background"`
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
	Posted    bool      `json:"posted"`
	Posting   int       `json:"posting"`
	ReviewID  string    `json:"review_id,omitempty"`
	Brief     BriefWire `json:"brief"`
	StepNames []string  `json:"step_names"`
	StepCount int       `json:"step_count"`
	// Position and ChapterIntro together name the stop the Reviewer is at.
	// Position is how far through the Steps they have come: 0 before Step 1,
	// else the Step on screen. ChapterIntro is 0 except on a Chapter's intro,
	// where it numbers the Chapter from 1 and Position is the Step before the
	// Chapter's first. So Position 0 with ChapterIntro 0 is the Overview, and
	// Position 0 with ChapterIntro 1 is the first Chapter's intro.
	Position     int       `json:"position"`
	ChapterIntro int       `json:"chapter_intro,omitempty"`
	Step         *StepWire `json:"step,omitempty"`
	// Chapters are the Round's Chapters, none for a Round without them.
	Chapters     []ChapterWire     `json:"chapters,omitempty"`
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
	ID         int           `json:"id"`
	Step       int           `json:"step"`
	Repository string        `json:"repository"`
	File       string        `json:"file"`
	Segments   []SegmentWire `json:"segments"`
	Location   string        `json:"location"`
	Anchor     string        `json:"anchor"`
	Note       string        `json:"note"`
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
func (comment CommentWire) Covers(repository, file, side string, line int) bool {
	if comment.Repository != repository || comment.File != file {
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
			Goal:       v.Brief.Goal,
			Background: v.Brief.Background,
		},
		StepNames:    v.StepNames,
		StepCount:    v.StepCount,
		Position:     v.Position,
		ChapterIntro: v.ChapterIntro,
		Coverage:     CoverageWire{Seen: v.Coverage.Seen, Total: v.Coverage.Total},
		Seen:         v.Seen,
		Finished:     v.Finished,
		Concluded:    v.Concluded,
		Dismissed:    v.Dismissed,
		Replaced:     v.Replaced,

		Questions:      toQuestionWires(v.Questions),
		RoundQuestions: toQuestionWires(v.RoundQuestions),

		Round:                  v.Round,
		PreviousRound:          v.PreviousRound,
		SincePreviousRound:     v.SincePreviousRound,
		UnchangedSincePrevious: v.UnchangedSincePrevious,
	}
	for _, chapter := range v.Chapters {
		wire.Chapters = append(wire.Chapters, ChapterWire{
			Number:                 chapter.Number,
			Name:                   chapter.Name,
			Intro:                  chapter.Intro,
			FirstStep:              chapter.FirstStep,
			LastStep:               chapter.LastStep,
			Seen:                   chapter.Seen,
			QuestionCount:          chapter.QuestionCount,
			UnchangedSincePrevious: chapter.UnchangedSincePrevious,
		})
	}
	for _, st := range v.StepStatuses {
		wire.StepStatuses = append(wire.StepStatuses, string(st))
	}
	for _, comment := range v.Comments {
		wire.Comments = append(wire.Comments, CommentWire{
			ID: comment.ID, Step: comment.Step,
			Repository: comment.Anchor.Repository,
			File:       comment.Anchor.File,
			Segments:   toSegmentWires(comment.Anchor.Segments),
			Location:   comment.Anchor.Location(),
			Anchor:     comment.Anchor.Render(),
			Note:       comment.Note,

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
		entry := AccountedQuestionWire{
			Question: toQuestionWire(accounted.Question),
			Status:   string(accounted.Status),
			Response: accounted.Response,
		}
		if accounted.AskedAgainAs != nil {
			asked := toQuestionWire(*accounted.AskedAgainAs)
			entry.AskedAgainAs = &asked
		}
		wire.AccountedQuestions = append(wire.AccountedQuestions, entry)
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
			Chapter:               v.Step.Chapter,
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
			Blocks:        toBlockWires(view.Blocks),
			Plain:         view.Plain,
			DiagramFile:   toDiagramFileWire(view.DiagramFile),
		}
		for _, line := range view.Lines {
			wire.Lines = append(wire.Lines, toLineWire(line))
		}
		wires = append(wires, wire)
	}
	return wires
}

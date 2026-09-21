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
	Repository string `json:"repository,omitempty" jsonschema:"Root path of the repository this Excerpt is in. Must be one named in the Change Set. Optional when reviewing a single repository: leave it out and dbn uses the only one there is"`
	File       string `json:"file" jsonschema:"Path to the file, relative to the repository root"`
	Side       string `json:"side" jsonschema:"'new' for the after-side of a change — added or edited lines, and unchanged context. Point at the after-side of an edit and dbn shows the before-side it replaced automatically; you need not name the old side. Use 'old' to show a standalone deletion — removed lines that nothing replaced — or, when you split one edit's after-side across Steps, to give each Step the removed lines its new lines replaced"`
	FirstLine  int    `json:"first_line" jsonschema:"First line of the range, counting from 1"`
	LastLine   int    `json:"last_line" jsonschema:"Last line of the range, inclusive"`
}

type wireAcknowledgement struct {
	Repository string   `json:"repository,omitempty" jsonschema:"Root path of the repository these files are in. Must be one named in the Change Set. Optional when reviewing a single repository: leave it out and dbn uses the only one there is"`
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
	Dispositions []wireDisposition `json:"dispositions,omitempty" jsonschema:"When this is a Revision Round posted after a hand-off, or a replacement of one, one entry per Comment the previous round raised, saying whether you addressed, answered or declined it. Omit for a first Walkthrough"`
	Label        string            `json:"label,omitempty" jsonschema:"An optional short human-readable name for this review, shown to the Reviewer to tell several reviews apart, e.g. 'auth refactor'. It is not the review's id — dbn mints that — only a display aid. On a Revision Round you may omit it to keep the one you first gave"`
	Replaces     string            `json:"replaces,omitempty" jsonschema:"The id of the review under review, to replace its Walkthrough in place rather than wait for a hand-off. Use it only when the Reviewer asked for a change during the review, or you see your Walkthrough is wrong before they have got far. The review keeps its id and the Reviewer's Comments carry over. When replacing a Revision Round, supply its dispositions again"`
}

type postResult struct {
	Accepted bool          `json:"accepted"`
	ReviewID string        `json:"review_id,omitempty" jsonschema:"The id dbn assigned this review. Record it: pass it to conclude when the review is fully done so dbn can release it"`
	Problems []problemWire `json:"problems,omitempty" jsonschema:"Everything wrong with this Walkthrough, not just the first thing found. Fix them all before posting again"`
	Message  string        `json:"message,omitempty" jsonschema:"When accepted: what to tell the human, including how they open the review. Relay it to them, then end your turn"`
}

// postedMessage is written for the agent to relay: the Authoring Agent cannot
// see the Reviewer's terminal, and without this it could only say the review
// was ready, not where to look (#89). The command carries the port whenever it
// is not the one dbn finds on its own.
func postedMessage(kind review.PostKind, port int) string {
	command := "dbn"
	if port != DefaultPort {
		command = fmt.Sprintf("dbn -port %d", port)
	}
	lead := "Posted."
	switch kind {
	case review.PostedRevisionRound:
		lead = "The Revision Round is posted."
	case review.PostedReplacement:
		lead = "The Walkthrough is replaced."
	}
	return fmt.Sprintf("%s The Reviewer opens it by running %s in a terminal; if dbn is already open, it appears there. Tell them it's ready, then end your turn.", lead, command)
}

// problemWire is one fault in a refused post. A rejection carries at most one
// per reason, in the order dbn checks them.
type problemWire struct {
	Reason string `json:"reason" jsonschema:"What kind of fault this is, as a value you can act on"`
	Detail string `json:"detail" jsonschema:"What specifically to fix"`
}

type concludeInput struct {
	ReviewID string `json:"review_id" jsonschema:"The id of the review to conclude, as returned by post_walkthrough"`
}

type concludeResult struct {
	Concluded bool          `json:"concluded"`
	Problems  []problemWire `json:"problems,omitempty" jsonschema:"Everything wrong with this conclude, not just the first thing found"`
	Message   string        `json:"message" jsonschema:"A human-readable account of the outcome"`
}

type commentWire struct {
	ID       int    `json:"id"`
	Step     int    `json:"step" jsonschema:"The Step number this Comment was raised on"`
	Location string `json:"location" jsonschema:"Where in the code it points: the file, and the before-side and after-side lines it covers"`
	Anchor   string `json:"anchor" jsonschema:"The full anchored context, ready to act on: the code and where it lives"`
	Note     string `json:"note" jsonschema:"What the Reviewer said: a change they want, or a question"`
	// ReRaisedFrom is only set when the Reviewer pushed back on how you resolved a
	// Comment last round.
	ReRaisedFrom int  `json:"re_raised_from,omitempty" jsonschema:"Set when the Reviewer re-raised the Comment you declined or answered as #N: they did not accept your reasoning. Answer the point or change the code — repeating the same reasoning is not a response"`
	CarriedOver  bool `json:"carried_over,omitempty" jsonschema:"Set when the Comment was raised on a Walkthrough you since replaced in place. Its step is 0, since that Walkthrough's Steps are gone; its anchor still quotes the code it was raised on"`
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
		ChangeSet:    toChangeSet(w.Repositories),
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
			CarriedOver:  comment.CarriedOver,
		})
	}
	for _, sr := range r.StepReports {
		out.Steps = append(out.Steps, stepReportWire{Number: sr.Number, Name: sr.Name, Status: string(sr.Status)})
	}
	return out
}

type describeInput struct {
	Repositories []wireRepository `json:"repositories" jsonschema:"The repositories to describe, exactly as you would give them to post_walkthrough"`
}

// lineRangeWire is an inclusive [first, last] pair: compact, since a large
// Change Set describes hundreds of them.
type lineRangeWire [2]int

type modificationWire struct {
	Old lineRangeWire `json:"old" jsonschema:"The before-side lines this edit removed"`
	New lineRangeWire `json:"new" jsonschema:"The after-side lines that replaced them. Showing any of these with a new-side Excerpt draws the removed lines beside them"`
}

type fileChangesWire struct {
	Path          string             `json:"path"`
	Status        string             `json:"status" jsonschema:"'added', 'modified', 'deleted', 'renamed' (see from), 'binary' or 'mode'. A binary, mode or edit-free renamed file has no lines: account for it with an Acknowledgement"`
	From          string             `json:"from,omitempty" jsonschema:"Where the file was renamed from. Set whenever git detected a rename, including on a binary or mode change that was also renamed"`
	NewRanges     []lineRangeWire    `json:"new_ranges,omitempty" jsonschema:"The after-side Changed Lines, as [first, last] pairs: what new-side Excerpts must cover"`
	OldRanges     []lineRangeWire    `json:"old_ranges,omitempty" jsonschema:"The before-side Changed Lines, as [first, last] pairs. Those inside a modification are covered by showing its new side; the rest need an old-side Excerpt"`
	Modifications []modificationWire `json:"modifications,omitempty" jsonschema:"Each edit that replaced lines, pairing what it removed with what replaced it"`
	PreMarkedNew  []lineRangeWire    `json:"pre_marked_new,omitempty" jsonschema:"In a Revision Round, after-side ranges the Reviewer has already read and the next post need not cover"`
	PreMarkedOld  []lineRangeWire    `json:"pre_marked_old,omitempty" jsonschema:"In a Revision Round, before-side ranges the Reviewer has already read and the next post need not cover"`
	PreMarked     bool               `json:"pre_marked,omitempty" jsonschema:"In a Revision Round, set on a binary, mode or edit-free renamed file the Reviewer has already seen and the next post need not acknowledge again"`
}

type repositoryChangesWire struct {
	Root      string            `json:"root"`
	MergeBase string            `json:"merge_base" jsonschema:"The commit the Change Set is derived from: the merge-base of base and HEAD"`
	Files     []fileChangesWire `json:"files"`
}

type describeResult struct {
	Repositories  []repositoryChangesWire `json:"repositories,omitempty"`
	RevisionRound bool                    `json:"revision_round" jsonschema:"Whether your next post would be a Revision Round, scoped against what the Reviewer has already read"`
	StillToCover  *int                    `json:"still_to_cover,omitempty" jsonschema:"In a Revision Round, how many Changed Lines the next post must still account for once the pre-marked ones are set aside. It counts every one, on both sides: removed lines that ride along with a modification's new side, and blank lines dbn absorbs, are included, so it is a measure of what is left rather than of what you must excerpt"`
	Problems      []problemWire           `json:"problems,omitempty" jsonschema:"Why the Change Set could not be described, e.g. a base that does not resolve"`
}

func toChangeSet(repositories []wireRepository) review.ChangeSet {
	out := make([]review.Repository, 0, len(repositories))
	for _, r := range repositories {
		out = append(out, review.Repository{Root: r.Root, Base: r.Base})
	}
	return review.ChangeSet{Repositories: out}
}

func toDescribeResult(d review.ChangeDescription) describeResult {
	out := describeResult{RevisionRound: d.RevisionRound}
	if d.RevisionRound {
		still := d.StillToCover
		out.StillToCover = &still
	}
	for _, repository := range d.Repositories {
		wire := repositoryChangesWire{Root: repository.Root, MergeBase: repository.MergeBase, Files: []fileChangesWire{}}
		for _, file := range repository.Files {
			fileWire := fileChangesWire{
				Path:         file.Path,
				Status:       string(file.Status),
				From:         file.From,
				NewRanges:    toRangeWires(file.NewRanges),
				OldRanges:    toRangeWires(file.OldRanges),
				PreMarkedNew: toRangeWires(file.PreMarkedNew),
				PreMarkedOld: toRangeWires(file.PreMarkedOld),
				PreMarked:    file.PreMarked,
			}
			for _, m := range file.Modifications {
				fileWire.Modifications = append(fileWire.Modifications, modificationWire{
					Old: lineRangeWire{m.Old.First, m.Old.Last},
					New: lineRangeWire{m.New.First, m.New.Last},
				})
			}
			wire.Files = append(wire.Files, fileWire)
		}
		out.Repositories = append(out.Repositories, wire)
	}
	return out
}

func toRangeWires(ranges []review.LineRange) []lineRangeWire {
	var out []lineRangeWire
	for _, r := range ranges {
		out = append(out, lineRangeWire{r.First, r.Last})
	}
	return out
}

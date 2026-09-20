package review

// A Revision Round is a second Walkthrough posted after a Finish. It re-derives
// the full Change Set, pre-marks as shown every Changed Line whose content is
// unchanged since the previous round (ADR-0007), and accounts for every Comment
// the previous round raised. All of this lives in memory: restarting the
// daemon mid-review starts the review over.

// DispositionStatus is what the Authoring Agent did with a Comment.
type DispositionStatus string

const (
	// DispositionAddressed means the agent made the change.
	DispositionAddressed DispositionStatus = "addressed"
	// DispositionAnswered means the agent responded without changing anything,
	// as it would to a question.
	DispositionAnswered DispositionStatus = "answered"
	// DispositionDeclined means the agent will not make the change, and says why.
	DispositionDeclined DispositionStatus = "declined"
)

// Disposition is the Authoring Agent's account, posted with a Revision Round, of
// what it did with one Comment from the previous round.
type Disposition struct {
	CommentID int
	Status    DispositionStatus
	// Response is required when Answered, since it is the answer, and when
	// Declined, since a decline the Reviewer cannot weigh is just a refusal. It
	// is optional when Addressed.
	Response string
}

// ResolvedDisposition pairs a previous-round Comment with what the agent
// did about it, ready to show before any code in a Revision Round.
type ResolvedDisposition struct {
	Comment  Comment
	Status   DispositionStatus
	Response string
}

// contentKey identifies a Changed Line by its content rather than its position,
// so a line that keeps its content across a Revision Round is recognised even
// when edits above it have shifted its number.
type contentKey struct {
	repository string
	file       string
	side       Side
	text       string
}

// resolveChangedTexts reads the current text of every new-side Changed Line,
// resolving each file's span once. Old-side lines are removals with no
// working-tree text, so they are not included — and therefore never pre-marked.
func (s *Session) resolveChangedTexts(l ledger) map[ChangedLine]string {
	byFile := map[fileRef][]ChangedLine{}
	for _, line := range l.lines {
		if line.Side != NewSide {
			continue
		}
		ref := fileRef{line.Repository, line.File}
		byFile[ref] = append(byFile[ref], line)
	}

	out := map[ChangedLine]string{}
	for ref, lines := range byFile {
		first, last := lines[0].Line, lines[0].Line
		for _, line := range lines {
			if line.Line < first {
				first = line.Line
			}
			if line.Line > last {
				last = line.Line
			}
		}
		resolved, err := s.resolver.Resolve(Excerpt{Repository: ref.repository, File: ref.file, Side: NewSide, FirstLine: first, LastLine: last})
		if err != nil {
			continue
		}
		text := make(map[int]string, len(resolved))
		for _, line := range resolved {
			text[line.Number] = line.Text
		}
		for _, line := range lines {
			if t, ok := text[line.Line]; ok {
				out[line] = t
			}
		}
	}
	return out
}

// captureContent counts how many of the round's Changed Lines carry each content,
// so the next Revision Round can tell what has since moved. It is a count, not a
// set, because pre-marking is only safe for content that is unique.
func (s *Session) captureContent(l ledger) map[contentKey]int {
	out := map[contentKey]int{}
	for line, text := range s.resolveChangedTexts(l) {
		out[contentKey{line.Repository, line.File, line.Side, text}]++
	}
	return out
}

// preMarkUnchanged returns the Changed Lines of a Revision Round already reviewed
// in the previous round, so the Coverage Ledger is left pointing at what moved. A
// line is pre-marked only when its content is unique in both rounds: content that
// appears more than once cannot be matched to a specific line, so a genuinely new
// line whose text collides with an old one must not be allowed to escape coverage.
func (s *Session) preMarkUnchanged(l ledger) map[ChangedLine]bool {
	if len(s.priorContent) == 0 {
		return nil
	}
	texts := s.resolveChangedTexts(l)

	currentCount := map[contentKey]int{}
	for line, text := range texts {
		currentCount[contentKey{line.Repository, line.File, line.Side, text}]++
	}

	preShown := map[ChangedLine]bool{}
	for line, text := range texts {
		key := contentKey{line.Repository, line.File, line.Side, text}
		if s.priorContent[key] == 1 && currentCount[key] == 1 {
			preShown[line] = true
		}
	}
	return preShown
}

// resolveDispositions pairs each posted Disposition with the previous round's
// Comment it names, and refuses a Revision Round that does not account for
// every one — addressed, or answered or declined with a response.
func (s *Session) resolveDispositions(dispositions []Disposition) ([]ResolvedDisposition, *Rejection) {
	prior := s.comments
	byID := make(map[int]Comment, len(prior))
	for _, comment := range prior {
		byID[comment.ID] = comment
	}

	seen := map[int]bool{}
	out := make([]ResolvedDisposition, 0, len(dispositions))
	for _, disposition := range dispositions {
		comment, ok := byID[disposition.CommentID]
		if !ok {
			return nil, reject(RejectedMalformedDisposition,
				"a disposition names Comment %d, which the previous round did not raise", disposition.CommentID)
		}
		if seen[disposition.CommentID] {
			return nil, reject(RejectedMalformedDisposition,
				"Comment %d has more than one disposition", disposition.CommentID)
		}
		seen[disposition.CommentID] = true

		switch disposition.Status {
		case DispositionAddressed:
		case DispositionAnswered:
			if disposition.Response == "" {
				return nil, reject(RejectedMalformedDisposition,
					"answering Comment %d needs a response: the response is the answer", disposition.CommentID)
			}
		case DispositionDeclined:
			if disposition.Response == "" {
				return nil, reject(RejectedMalformedDisposition,
					"declining Comment %d needs a response the Reviewer can weigh", disposition.CommentID)
			}
		default:
			return nil, reject(RejectedMalformedDisposition,
				"Comment %d must be disposed as %q, %q or %q", disposition.CommentID, DispositionAddressed, DispositionAnswered, DispositionDeclined)
		}
		out = append(out, ResolvedDisposition{Comment: comment, Status: disposition.Status, Response: disposition.Response})
	}

	for _, comment := range prior {
		if !seen[comment.ID] {
			return nil, reject(RejectedMalformedDisposition,
				"Comment %d from the previous round has no disposition; a Revision Round must account for every one", comment.ID)
		}
	}
	return out, nil
}

// Dispositions reports how the previous round's Comments were resolved,
// for display before any code.
func (s *Session) Dispositions() []ResolvedDisposition {
	out := make([]ResolvedDisposition, len(s.dispositions))
	copy(out, s.dispositions)
	return out
}

// ReRaise raises again a Comment the agent declined or answered, carrying its
// original Anchor into the current round so the Reviewer can insist in place
// without re-composing it. A resolution the agent addressed is not re-raisable:
// the code moved, and there is fresh code to comment on instead.
//
// note is what the Comment says. An empty one keeps the original wording, which
// is what a Reviewer who simply disagrees wants; anything else replaces it, so a
// follow-up or a counter-argument can go with the push-back.
//
// A resolution can carry only one standing re-raise (#80). Withdrawing that
// Comment frees it to be raised again.
func (s *Session) ReRaise(commentID int, note string) (Comment, error) {
	if s.finished {
		return Comment{}, reject(RejectedWalkthroughFinished,
			"this Walkthrough is handed off; resume it before re-raising")
	}
	for _, disposition := range s.dispositions {
		if disposition.Comment.ID != commentID {
			continue
		}
		if disposition.Status == DispositionAddressed {
			return Comment{}, reject(RejectedNoSuchComment,
				"Comment %d was addressed, so there is nothing to push back on; comment on the new code instead", commentID)
		}
		if standing, ok := s.reRaiseOf(commentID); ok {
			return Comment{}, reject(RejectedAlreadyReRaised,
				"Comment %d is already re-raised as Comment %d; withdraw that one to raise it afresh", commentID, standing.ID)
		}
		if note == "" {
			note = disposition.Comment.Note
		}
		s.nextCommentID++
		// Step is left 0: the previous round's Step number means nothing in this
		// round, whose Steps are authored afresh. The Anchor carries the location,
		// and it is self-contained. Step 0 keeps it from flagging the wrong Step.
		comment := Comment{
			ID:           s.nextCommentID,
			Step:         0,
			Anchor:       disposition.Comment.Anchor,
			Note:         note,
			ReRaisedFrom: commentID,
		}
		s.comments = append(s.comments, comment)
		return comment, nil
	}
	return Comment{}, reject(RejectedNoSuchComment,
		"there is no Comment %d from the previous round to re-raise", commentID)
}

// reRaiseOf is the Comment standing against a previous round's resolution this
// round, if the Reviewer has raised one.
func (s *Session) reRaiseOf(commentID int) (Comment, bool) {
	for _, comment := range s.comments {
		if comment.ReRaisedFrom == commentID {
			return comment, true
		}
	}
	return Comment{}, false
}

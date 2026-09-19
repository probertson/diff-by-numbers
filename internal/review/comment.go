package review

// Comment is a Reviewer's remark, a request for an edit or a question, anchored
// to the code it concerns. It is a proposal, not an instruction: the Authoring
// Agent responds to it after the Walkthrough ends, and may address, answer or
// decline it. Nothing on disk moves while the Walkthrough is under review
// (ADR-0004).
type Comment struct {
	ID     int
	Step   int
	Anchor Anchor
	Note   string
}

// RaiseComment anchors a note to a selection in the Step in view and
// records it. The Step it lands on becomes flagged.
func (s *Session) RaiseComment(target AnchorTarget, note string) (Comment, error) {
	if s.finished {
		return Comment{}, reject(RejectedWalkthroughFinished,
			"this Walkthrough is handed off; resume it to add more")
	}
	anchor, err := s.Anchor(target)
	if err != nil {
		return Comment{}, err
	}
	s.nextCommentID++
	comment := Comment{ID: s.nextCommentID, Step: s.position, Anchor: anchor, Note: note}
	s.comments = append(s.comments, comment)
	return comment, nil
}

// Comments lists what the Reviewer has raised so far, in the order raised.
func (s *Session) Comments() []Comment {
	out := make([]Comment, len(s.comments))
	copy(out, s.comments)
	return out
}

// WithdrawComment removes a Comment the Reviewer has reconsidered.
func (s *Session) WithdrawComment(id int) error {
	for i, comment := range s.comments {
		if comment.ID == id {
			s.comments = append(s.comments[:i], s.comments[i+1:]...)
			return nil
		}
	}
	return reject(RejectedNoSuchComment, "there is no Comment %d", id)
}

func (s *Session) commentsForStep(step int) int {
	count := 0
	for _, comment := range s.comments {
		if comment.Step == step {
			count++
		}
	}
	return count
}

// EditComment replaces the note of an existing Comment, so the
// Reviewer can refine a comment without withdrawing and re-anchoring it.
func (s *Session) EditComment(id int, note string) error {
	for i := range s.comments {
		if s.comments[i].ID == id {
			s.comments[i].Note = note
			return nil
		}
	}
	return reject(RejectedNoSuchComment, "there is no Comment %d", id)
}

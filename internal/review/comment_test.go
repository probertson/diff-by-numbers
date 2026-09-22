package review_test

import (
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

func raise(t *testing.T, s *review.Session, first, last int, note string) review.Comment {
	t.Helper()
	comment, err := s.RaiseComment(span(0, first, last), note)
	if err != nil {
		t.Fatalf("expected to raise a Comment, got %v", err)
	}
	return comment
}

func TestARaisedCommentCarriesItsAnchorAndNote(t *testing.T) {
	session := newSession()
	mustPost(t, session, validRound())
	mustAdvance(t, session)

	comment := raise(t, session, 20, 22, "cap this retry at 3 attempts")

	if comment.Note != "cap this retry at 3 attempts" {
		t.Errorf("unexpected note %q", comment.Note)
	}
	if comment.Anchor.Location() != "src/fetch.ts — after 20-22" {
		t.Errorf("unexpected anchor %q", comment.Anchor.Location())
	}
	if comment.Step != 1 {
		t.Errorf("expected the Comment to record Step 1, got %d", comment.Step)
	}
}

func TestSeveralCommentsCanBeRaisedOnOneStepAndListed(t *testing.T) {
	session := newSession()
	mustPost(t, session, validRound())
	mustAdvance(t, session)

	raise(t, session, 20, 20, "first")
	raise(t, session, 25, 25, "second")

	list := session.Comments()
	if len(list) != 2 {
		t.Fatalf("expected 2 Comments, got %d", len(list))
	}
	if list[0].ID == list[1].ID {
		t.Error("expected distinct IDs")
	}
}

func TestACommentCanBeWithdrawn(t *testing.T) {
	session := newSession()
	mustPost(t, session, validRound())
	mustAdvance(t, session)
	comment := raise(t, session, 20, 22, "reconsidered")

	if err := session.WithdrawComment(comment.ID); err != nil {
		t.Fatalf("expected to withdraw, got %v", err)
	}
	if len(session.Comments()) != 0 {
		t.Error("expected the Comment to be gone")
	}
}

func TestWithdrawingAnUnknownCommentIsRejected(t *testing.T) {
	session := newSession()
	mustPost(t, session, validRound())

	err := session.WithdrawComment(999)

	assertRejected(t, err, review.RejectedNoSuchComment)
}

func TestAStepWithACommentIsFlaggedOtherwiseSeen(t *testing.T) {
	session := threeStepSession(t) // three steps, empty diff
	mustAdvance(t, session)        // visit Step 1 (seen)
	mustAdvance(t, session)        // visit Step 2
	// Flag Step 2.
	if _, err := session.RaiseComment(span(0, 1, 1), "fix"); err != nil {
		t.Fatal(err)
	}

	statuses := session.View().StepStatuses
	if statuses[0] != review.StepSeen {
		t.Errorf("expected Step 1 seen, got %v", statuses[0])
	}
	if statuses[1] != review.StepFlagged {
		t.Errorf("expected Step 2 flagged, got %v", statuses[1])
	}
	if statuses[2] != review.StepUnseen {
		t.Errorf("expected Step 3 unseen, got %v", statuses[2])
	}
}

func TestWithdrawingTheLastCommentReturnsAStepToSeen(t *testing.T) {
	session := threeStepSession(t)
	mustAdvance(t, session) // Step 1 seen
	comment, err := session.RaiseComment(span(0, 1, 1), "fix")
	if err != nil {
		t.Fatal(err)
	}
	if session.View().StepStatuses[0] != review.StepFlagged {
		t.Fatal("expected Step 1 flagged after raising")
	}

	if err := session.WithdrawComment(comment.ID); err != nil {
		t.Fatal(err)
	}

	if session.View().StepStatuses[0] != review.StepSeen {
		t.Error("expected Step 1 to return to seen after the last Comment was withdrawn")
	}
}

func TestFinishReportsEverythingTheAgentNeedsToReground(t *testing.T) {
	session := threeStepSession(t)
	mustAdvance(t, session)
	raise(t, session, 1, 1, "narrow this")

	if err := session.Finish(); err != nil {
		t.Fatalf("expected to finish, got %v", err)
	}

	results, err := session.Results()
	if err != nil {
		t.Fatal(err)
	}
	if !results.Finished {
		t.Error("expected the Round to report finished")
	}
	if len(results.Comments) != 1 || results.Comments[0].Note != "narrow this" {
		t.Errorf("expected the Comment in the results, got %+v", results.Comments)
	}
	if results.Brief.Goal == "" {
		t.Error("expected the Brief in the results so the agent can re-ground itself")
	}
	if len(results.StepReports) != 3 {
		t.Errorf("expected a report per Step, got %d", len(results.StepReports))
	}
}

func TestACommentNoteCanBeEdited(t *testing.T) {
	session := newSession()
	mustPost(t, session, validRound())
	mustAdvance(t, session)
	comment := raise(t, session, 20, 22, "original")

	if err := session.EditComment(comment.ID, "refined"); err != nil {
		t.Fatalf("expected to edit, got %v", err)
	}

	list := session.Comments()
	if list[0].Note != "refined" {
		t.Errorf("expected the note to be updated, got %q", list[0].Note)
	}
}

func TestEditingAnUnknownCommentIsRejected(t *testing.T) {
	session := newSession()
	mustPost(t, session, validRound())

	err := session.EditComment(999, "x")

	assertRejected(t, err, review.RejectedNoSuchComment)
}

func TestAFinishedRoundCanBeReopenedToAddMore(t *testing.T) {
	session := newSession()
	mustPost(t, session, validRound())
	mustAdvance(t, session)
	if err := session.Finish(); err != nil {
		t.Fatal(err)
	}

	// Adding is refused while finished.
	if _, err := session.RaiseComment(span(0, 20, 20), "late"); err == nil {
		t.Fatal("expected adding to be refused while finished")
	}

	if err := session.Reopen(); err != nil {
		t.Fatalf("expected to reopen, got %v", err)
	}
	if _, err := session.RaiseComment(span(0, 20, 20), "late"); err != nil {
		t.Fatalf("expected to add after reopening, got %v", err)
	}
	if session.View().Finished {
		t.Error("expected the view to report not finished after reopen")
	}
}

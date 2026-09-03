package review_test

import (
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

func raise(t *testing.T, s *review.Session, first, last int, note string) review.ChangeRequest {
	t.Helper()
	cr, err := s.RaiseChangeRequest(review.AnchorTarget{ExcerptIndex: 0, FirstLine: first, LastLine: last}, note)
	if err != nil {
		t.Fatalf("expected to raise a Change Request, got %v", err)
	}
	return cr
}

func TestARaisedChangeRequestCarriesItsAnchorAndNote(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough())
	mustAdvance(t, session)

	cr := raise(t, session, 20, 22, "cap this retry at 3 attempts")

	if cr.Note != "cap this retry at 3 attempts" {
		t.Errorf("unexpected note %q", cr.Note)
	}
	if cr.Anchor.File != "src/fetch.ts" || cr.Anchor.FirstLine != 20 {
		t.Errorf("unexpected anchor %s:%d", cr.Anchor.File, cr.Anchor.FirstLine)
	}
	if cr.Step != 1 {
		t.Errorf("expected the Change Request to record Step 1, got %d", cr.Step)
	}
}

func TestSeveralChangeRequestsCanBeRaisedOnOneStepAndListed(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough())
	mustAdvance(t, session)

	raise(t, session, 20, 20, "first")
	raise(t, session, 25, 25, "second")

	list := session.ChangeRequests()
	if len(list) != 2 {
		t.Fatalf("expected 2 Change Requests, got %d", len(list))
	}
	if list[0].ID == list[1].ID {
		t.Error("expected distinct IDs")
	}
}

func TestAChangeRequestCanBeWithdrawn(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough())
	mustAdvance(t, session)
	cr := raise(t, session, 20, 22, "reconsidered")

	if err := session.WithdrawChangeRequest(cr.ID); err != nil {
		t.Fatalf("expected to withdraw, got %v", err)
	}
	if len(session.ChangeRequests()) != 0 {
		t.Error("expected the Change Request to be gone")
	}
}

func TestWithdrawingAnUnknownChangeRequestIsRejected(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough())

	err := session.WithdrawChangeRequest(999)

	assertRejected(t, err, review.RejectedNoSuchChangeRequest)
}

func TestAStepWithAChangeRequestIsFlaggedOtherwiseSeen(t *testing.T) {
	session := threeStepSession(t) // three steps, empty diff
	mustAdvance(t, session)        // visit Step 1 (seen)
	mustAdvance(t, session)        // visit Step 2
	// Flag Step 2.
	if _, err := session.RaiseChangeRequest(review.AnchorTarget{ExcerptIndex: 0, FirstLine: 1, LastLine: 1}, "fix"); err != nil {
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

func TestWithdrawingTheLastChangeRequestReturnsAStepToSeen(t *testing.T) {
	session := threeStepSession(t)
	mustAdvance(t, session) // Step 1 seen
	cr, err := session.RaiseChangeRequest(review.AnchorTarget{ExcerptIndex: 0, FirstLine: 1, LastLine: 1}, "fix")
	if err != nil {
		t.Fatal(err)
	}
	if session.View().StepStatuses[0] != review.StepFlagged {
		t.Fatal("expected Step 1 flagged after raising")
	}

	if err := session.WithdrawChangeRequest(cr.ID); err != nil {
		t.Fatal(err)
	}

	if session.View().StepStatuses[0] != review.StepSeen {
		t.Error("expected Step 1 to return to seen after the last Change Request was withdrawn")
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
		t.Error("expected the Walkthrough to report finished")
	}
	if len(results.ChangeRequests) != 1 || results.ChangeRequests[0].Note != "narrow this" {
		t.Errorf("expected the Change Request in the results, got %+v", results.ChangeRequests)
	}
	if results.Brief.Ask == "" {
		t.Error("expected the Brief in the results so the agent can re-ground itself")
	}
	if len(results.StepReports) != 3 {
		t.Errorf("expected a report per Step, got %d", len(results.StepReports))
	}
}

func TestAChangeRequestNoteCanBeEdited(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough())
	mustAdvance(t, session)
	cr := raise(t, session, 20, 22, "original")

	if err := session.EditChangeRequest(cr.ID, "refined"); err != nil {
		t.Fatalf("expected to edit, got %v", err)
	}

	list := session.ChangeRequests()
	if list[0].Note != "refined" {
		t.Errorf("expected the note to be updated, got %q", list[0].Note)
	}
}

func TestEditingAnUnknownChangeRequestIsRejected(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough())

	err := session.EditChangeRequest(999, "x")

	assertRejected(t, err, review.RejectedNoSuchChangeRequest)
}

func TestAFinishedWalkthroughCanBeReopenedToAddMore(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough())
	mustAdvance(t, session)
	if err := session.Finish(); err != nil {
		t.Fatal(err)
	}

	// Adding is refused while finished.
	if _, err := session.RaiseChangeRequest(review.AnchorTarget{ExcerptIndex: 0, FirstLine: 20, LastLine: 20}, "late"); err == nil {
		t.Fatal("expected adding to be refused while finished")
	}

	if err := session.Reopen(); err != nil {
		t.Fatalf("expected to reopen, got %v", err)
	}
	if _, err := session.RaiseChangeRequest(review.AnchorTarget{ExcerptIndex: 0, FirstLine: 20, LastLine: 20}, "late"); err != nil {
		t.Fatalf("expected to add after reopening, got %v", err)
	}
	if session.View().Finished {
		t.Error("expected the view to report not finished after reopen")
	}
}

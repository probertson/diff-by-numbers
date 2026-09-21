package review_test

import (
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// minter returns successive ids, so a test can assert both what id a review is
// given and — by handing it more than one — that a Revision Round does not mint
// a second.
func minter(ids ...string) func() string {
	i := 0
	return func() string {
		id := ids[i]
		i++
		return id
	}
}

func labeledWalkthrough(label string, steps []review.Step) review.Walkthrough {
	w := appWalkthrough(steps, nil)
	w.Label = label
	return w
}

func TestANewReviewIsAssignedAnID(t *testing.T) {
	deriver := &roundDeriver{lines: changedApp(1, 3)}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver, review.WithIDMinter(minter("rev-1")))

	mustPost(t, session, appWalkthrough([]review.Step{appStep(1, 3)}, nil))

	if got := session.ReviewID(); got != "rev-1" {
		t.Errorf("expected the minted id rev-1, got %q", got)
	}
}

func TestARevisionRoundKeepsTheSameID(t *testing.T) {
	deriver := &roundDeriver{lines: changedApp(1, 3)}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver, review.WithIDMinter(minter("rev-1", "rev-2")))
	mustPost(t, session, appWalkthrough([]review.Step{appStep(1, 3)}, nil))
	handOffWithAComment(t, session)

	deriver.lines = changedApp(1, 4)
	mustPost(t, session, revising(appWalkthrough([]review.Step{appStep(4, 4)}, nil)))

	if got := session.ReviewID(); got != "rev-1" {
		t.Errorf("expected the id to persist across a Revision Round, got %q", got)
	}
}

func TestTheAgentLabelIsCarried(t *testing.T) {
	deriver := &roundDeriver{lines: changedApp(1, 3)}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver)

	mustPost(t, session, labeledWalkthrough("auth refactor", []review.Step{appStep(1, 3)}))

	if got := session.Label(); got != "auth refactor" {
		t.Errorf("expected the label to be carried, got %q", got)
	}
}

func TestALabelSurvivesARevisionThatOmitsIt(t *testing.T) {
	deriver := &roundDeriver{lines: changedApp(1, 3)}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver)
	mustPost(t, session, labeledWalkthrough("auth refactor", []review.Step{appStep(1, 3)}))
	handOffWithAComment(t, session)

	deriver.lines = changedApp(1, 4)
	mustPost(t, session, revising(appWalkthrough([]review.Step{appStep(4, 4)}, nil))) // no label

	if got := session.Label(); got != "auth refactor" {
		t.Errorf("expected the label to persist across a Revision Round, got %q", got)
	}
}

func TestBeforeAnyPostAReviewIsNeitherActiveNorConcluded(t *testing.T) {
	session := review.NewSession(&textResolver{text: map[string]string{}}, &roundDeriver{})

	if session.Active() {
		t.Error("expected no active review before a post")
	}
	if session.Concluded() {
		t.Error("expected nothing concluded before a post")
	}
}

func TestANewReviewIsActive(t *testing.T) {
	deriver := &roundDeriver{lines: changedApp(1, 3)}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver)

	mustPost(t, session, appWalkthrough([]review.Step{appStep(1, 3)}, nil))

	if !session.Active() {
		t.Error("expected a freshly posted review to be active")
	}
	if session.Concluded() {
		t.Error("expected a freshly posted review not to be concluded")
	}
}

func TestAZeroCommentFinishReadsAsConcluded(t *testing.T) {
	session, _ := finishRound1(t)

	if !session.Concluded() {
		t.Error("expected a finish with nothing raised to read as concluded")
	}
	if session.Active() {
		t.Error("expected a concluded review not to be active")
	}
}

func TestTheViewReportsConcluded(t *testing.T) {
	concluded, _ := finishRound1(t)
	if !concluded.View().Concluded {
		t.Error("a zero-Comment finish should report Concluded on the view")
	}

	awaiting, _ := finishRound1WithComment(t)
	if awaiting.View().Concluded {
		t.Error("a finish with a Comment outstanding should not report Concluded on the view")
	}
}

func TestAFinishWithCommentsIsNotConcluded(t *testing.T) {
	session, _ := finishRound1WithComment(t)

	if session.Concluded() {
		t.Error("expected a finish with a Comment not to be concluded")
	}
	if !session.Active() {
		t.Error("expected a review awaiting a Revision Round to still be active")
	}
}

func TestExplicitConcludeEndsAReviewEvenWithOpenComments(t *testing.T) {
	session, _ := finishRound1WithComment(t)

	if err := session.Conclude(session.ReviewID()); err != nil {
		t.Fatalf("expected conclude to succeed, got %v", err)
	}

	if !session.Concluded() {
		t.Error("expected the review to be concluded after an explicit conclude")
	}
	if session.Active() {
		t.Error("expected a concluded review not to be active")
	}
}

func TestConcludeIsIdempotent(t *testing.T) {
	session, _ := finishRound1WithComment(t)
	id := session.ReviewID()

	if err := session.Conclude(id); err != nil {
		t.Fatalf("expected the first conclude to succeed, got %v", err)
	}

	if err := session.Conclude(id); err != nil {
		t.Errorf("expected a second conclude to be a no-op, got %v", err)
	}
}

func TestConcludeRejectsAnUnknownReviewID(t *testing.T) {
	session, _ := finishRound1(t)

	err := session.Conclude("not-the-id")

	assertRejected(t, err, review.RejectedUnknownReview)
}

func TestConcludeWithoutAReviewIsRejected(t *testing.T) {
	session := review.NewSession(&textResolver{text: map[string]string{}}, &roundDeriver{})

	err := session.Conclude("anything")

	assertRejected(t, err, review.RejectedNoWalkthrough)
}

func TestReopenUnconcludesAZeroCommentFinish(t *testing.T) {
	session, _ := finishRound1(t)
	if !session.Concluded() {
		t.Fatal("precondition: expected a zero-Comment finish to be concluded")
	}

	if err := session.Reopen(); err != nil {
		t.Fatal(err)
	}

	if session.Concluded() {
		t.Error("expected reopen to un-conclude the review")
	}
	if !session.Active() {
		t.Error("expected a reopened review to be active again")
	}
}

func TestReopenUndoesAnExplicitConclude(t *testing.T) {
	session, _ := finishRound1WithComment(t)
	if err := session.Conclude(session.ReviewID()); err != nil {
		t.Fatal(err)
	}

	if err := session.Reopen(); err != nil {
		t.Fatal(err)
	}

	if session.Concluded() {
		t.Error("expected reopen to clear an explicit conclude")
	}
}

func TestAbandonLeavesNoActiveReview(t *testing.T) {
	deriver := &roundDeriver{lines: changedApp(1, 3)}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver)
	mustPost(t, session, appWalkthrough([]review.Step{appStep(1, 3)}, nil))

	if err := session.Abandon(); err != nil {
		t.Fatal(err)
	}

	if session.Active() {
		t.Error("expected no active review after abandon")
	}
}

// A round handed off with nothing raised ends the review: fetch_results calls it
// complete, and the next post is new work, not a Revision Round of the old. Scoped
// as a Revision Round, new work was pre-marked against the finished review and
// could escape coverage.
func TestAPostAfterAHandOffWithNothingRaisedStartsANewReview(t *testing.T) {
	deriver := &roundDeriver{lines: changedApp(1, 3)}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver, review.WithIDMinter(minter("rev-1", "rev-2")))
	mustPost(t, session, appWalkthrough([]review.Step{appStep(1, 3)}, nil))
	if err := session.Finish(); err != nil {
		t.Fatal(err)
	}

	// Only line 3 shown: in a Revision Round, 1 and 2 would be pre-marked and
	// this would pass.
	err := session.Post(appWalkthrough([]review.Step{appStep(3, 3)}, nil))

	assertRejected(t, err, review.RejectedUncoveredChanges)
	mustPost(t, session, appWalkthrough([]review.Step{appStep(1, 3)}, nil))
	if got := session.ReviewID(); got != "rev-2" {
		t.Errorf("expected a new review, got id %q", got)
	}
	if session.View().Round != 1 {
		t.Errorf("expected round 1 of the new review, got round %d", session.View().Round)
	}
}

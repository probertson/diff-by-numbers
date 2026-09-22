package review_test

import (
	"testing"
	"time"

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

func labeledRound(label string, steps []review.Step) review.Round {
	w := appRound(steps, nil)
	w.Label = label
	return w
}

func TestANewReviewIsAssignedAnID(t *testing.T) {
	deriver := &roundDeriver{lines: changedApp(1, 3)}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver, review.WithIDMinter(minter("rev-1")))

	mustPost(t, session, appRound([]review.Step{appStep(1, 3)}, nil))

	if got := session.ReviewID(); got != "rev-1" {
		t.Errorf("expected the minted id rev-1, got %q", got)
	}
}

func TestARevisionRoundKeepsTheSameID(t *testing.T) {
	deriver := &roundDeriver{lines: changedApp(1, 3)}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver, review.WithIDMinter(minter("rev-1", "rev-2")))
	mustPost(t, session, appRound([]review.Step{appStep(1, 3)}, nil))
	handOffWithAComment(t, session)

	deriver.lines = changedApp(1, 4)
	mustRevise(t, session, revising(appRound([]review.Step{appStep(4, 4)}, nil)))

	if got := session.ReviewID(); got != "rev-1" {
		t.Errorf("expected the id to persist across a Revision Round, got %q", got)
	}
}

func TestTheAgentLabelIsCarried(t *testing.T) {
	deriver := &roundDeriver{lines: changedApp(1, 3)}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver)

	mustPost(t, session, labeledRound("auth refactor", []review.Step{appStep(1, 3)}))

	if got := session.Label(); got != "auth refactor" {
		t.Errorf("expected the label to be carried, got %q", got)
	}
}

func TestALabelSurvivesARevisionThatOmitsIt(t *testing.T) {
	deriver := &roundDeriver{lines: changedApp(1, 3)}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver)
	mustPost(t, session, labeledRound("auth refactor", []review.Step{appStep(1, 3)}))
	handOffWithAComment(t, session)

	deriver.lines = changedApp(1, 4)
	mustRevise(t, session, revising(appRound([]review.Step{appStep(4, 4)}, nil))) // no label

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

	mustPost(t, session, appRound([]review.Step{appStep(1, 3)}, nil))

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

	assertRejected(t, err, review.RejectedNoRound)
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
	mustPost(t, session, appRound([]review.Step{appStep(1, 3)}, nil))

	if err := session.Abandon(); err != nil {
		t.Fatal(err)
	}

	if session.Active() {
		t.Error("expected no active review after abandon")
	}
}

// A row's age is measured from when the round on screen was accepted, so a
// Session reads the clock at that moment and nowhere else.
func TestAnOpenReviewIsTimedFromItsLatestRound(t *testing.T) {
	posted := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	clock := posted
	deriver := &roundDeriver{lines: changedApp(1, 3)}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver,
		review.WithClock(func() time.Time { return clock }))
	mustPost(t, session, appRound([]review.Step{appStep(1, 3)}, nil))
	handOffWithAComment(t, session)

	clock = posted.Add(time.Hour)
	deriver.lines = changedApp(1, 4)
	mustRevise(t, session, revising(appRound([]review.Step{appStep(4, 4)}, nil)))

	open, ok := session.Open()
	if !ok {
		t.Fatal("a review under way is open")
	}
	if !open.Posted.Equal(posted.Add(time.Hour)) {
		t.Errorf("expected the time of the Revision Round, got %v", open.Posted)
	}
	if open.Round != 2 || open.CommentCount != 0 {
		t.Errorf("expected round 2 with no Comments outstanding, got round %d with %d", open.Round, open.CommentCount)
	}
}

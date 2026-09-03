package review_test

import (
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// threeStepSession posts a Walkthrough of three Steps over an empty diff, so
// navigation can be exercised without coverage getting in the way.
func threeStepSession(t *testing.T) *review.Session {
	t.Helper()
	walkthrough := validWalkthrough()
	walkthrough.Steps = []review.Step{
		{Name: "First", Explanation: "one", Excerpts: oneExcerpt("a.ts")},
		{Name: "Second", Explanation: "two", Excerpts: oneExcerpt("b.ts")},
		{Name: "Third", Explanation: "three", Excerpts: oneExcerpt("c.ts")},
	}
	session := newSession()
	mustPost(t, session, walkthrough)
	return session
}

func oneExcerpt(file string) []review.Excerpt {
	return []review.Excerpt{{Repository: "/repos/argus-portal", File: file, Side: review.NewSide, FirstLine: 1, LastLine: 5}}
}

func TestBackReturnsToThePreviousStepAndStopsAtTheBrief(t *testing.T) {
	session := threeStepSession(t)
	mustAdvance(t, session)
	mustAdvance(t, session) // at Step 2

	if err := session.Back(); err != nil {
		t.Fatalf("expected to move back, got %v", err)
	}
	if p := session.View().Position; p != 1 {
		t.Fatalf("expected position 1 after back, got %d", p)
	}

	mustBack(t, session) // to Brief
	mustBack(t, session) // already at Brief; clamps
	if p := session.View().Position; p != 0 {
		t.Errorf("expected to clamp at the Brief (0), got %d", p)
	}
}

func TestGoToJumpsDirectlyToAnyStep(t *testing.T) {
	session := threeStepSession(t)

	if err := session.GoTo(3); err != nil {
		t.Fatalf("expected to jump to Step 3, got %v", err)
	}
	view := session.View()
	if view.Position != 3 || view.Step == nil || view.Step.Name != "Third" {
		t.Errorf("expected to be at Step 3 'Third', got position %d", view.Position)
	}
}

func TestGoToZeroReturnsToTheBrief(t *testing.T) {
	session := threeStepSession(t)
	mustAdvance(t, session)

	if err := session.GoTo(0); err != nil {
		t.Fatalf("expected to return to the Brief, got %v", err)
	}
	view := session.View()
	if view.Position != 0 || view.Step != nil {
		t.Errorf("expected to be at the Brief with no Step, got position %d", view.Position)
	}
}

func TestGoToOutOfRangeIsRejected(t *testing.T) {
	session := threeStepSession(t)

	for _, target := range []int{-1, 4, 99} {
		err := session.GoTo(target)
		assertRejected(t, err, review.RejectedNoSuchStep)
	}
}

func TestVisitingAStepRecordsItAsSeen(t *testing.T) {
	session := threeStepSession(t)

	// Jump to Step 2 directly; only it (not Step 1) should be seen.
	if err := session.GoTo(2); err != nil {
		t.Fatal(err)
	}

	seen := session.View().Seen
	if len(seen) != 3 {
		t.Fatalf("expected a seen flag per Step, got %d", len(seen))
	}
	if seen[0] {
		t.Error("expected Step 1 to be unseen — it was skipped")
	}
	if !seen[1] {
		t.Error("expected Step 2 to be seen")
	}
	if seen[2] {
		t.Error("expected Step 3 to be unseen")
	}
}

func TestTheBriefIsNotAStepAndIsNeverMarkedSeen(t *testing.T) {
	session := threeStepSession(t)

	// Sitting at the Brief, nothing is seen yet.
	for i, s := range session.View().Seen {
		if s {
			t.Errorf("expected no Step seen at the Brief, but Step %d was", i+1)
		}
	}
}

func mustBack(t *testing.T, session *review.Session) {
	t.Helper()
	if err := session.Back(); err != nil {
		t.Fatalf("expected to move back, got %v", err)
	}
}

package review_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// stubResolver fabricates deterministic lines so view tests need no filesystem.
type stubResolver struct{}

func (stubResolver) Resolve(e review.Excerpt) ([]review.Line, error) {
	lines := make([]review.Line, 0, e.LastLine-e.FirstLine+1)
	for n := e.FirstLine; n <= e.LastLine; n++ {
		lines = append(lines, review.Line{Number: n, Text: fmt.Sprintf("%s line %d", e.File, n)})
	}
	return lines, nil
}

func newSession() *review.Session {
	return review.NewSession(stubResolver{})
}

func TestTheViewBeforeAnyPostSaysNothingIsPosted(t *testing.T) {
	session := newSession()

	view := session.View()

	if view.Posted {
		t.Error("expected the view to report that nothing is posted")
	}
}

func TestTheViewOpensAtTheBrief(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough())

	view := session.View()

	if !view.Posted {
		t.Fatal("expected the view to report a posted Walkthrough")
	}
	if view.Position != 0 {
		t.Errorf("expected the view to open at the Brief (position 0), got %d", view.Position)
	}
	if view.Step != nil {
		t.Error("expected no Step to be in view at the Brief")
	}
	if view.Brief.Ask != "Add retry with backoff to the fetch layer" {
		t.Errorf("expected the Brief's ask, got %q", view.Brief.Ask)
	}
	if view.StepCount != 1 {
		t.Errorf("expected a count of 1 Step, got %d", view.StepCount)
	}
	if len(view.StepNames) != 1 || view.StepNames[0] != "Add the retrier" {
		t.Errorf("expected the Brief to list the Steps to come, got %v", view.StepNames)
	}
}

func TestAdvancingFromTheBriefShowsTheFirstStepResolved(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough())

	if err := session.Advance(); err != nil {
		t.Fatalf("expected to advance from the Brief, got %v", err)
	}

	view := session.View()
	if view.Position != 1 {
		t.Fatalf("expected position 1, got %d", view.Position)
	}
	step := view.Step
	if step == nil {
		t.Fatal("expected a Step in view")
	}
	if step.Number != 1 || step.Name != "Add the retrier" {
		t.Errorf("expected Step 1 'Add the retrier', got %d %q", step.Number, step.Name)
	}
	if step.Explanation != "A transport wrapper that retries idempotent requests" {
		t.Errorf("unexpected explanation %q", step.Explanation)
	}
	if len(step.Excerpts) != 1 {
		t.Fatalf("expected one Excerpt, got %d", len(step.Excerpts))
	}
	excerpt := step.Excerpts[0]
	if excerpt.Problem != "" {
		t.Fatalf("expected the Excerpt to resolve, got problem %q", excerpt.Problem)
	}
	if len(excerpt.Lines) != 23 {
		t.Fatalf("expected lines 12-34 to yield 23 lines, got %d", len(excerpt.Lines))
	}
	if excerpt.Lines[0].Number != 12 || excerpt.Lines[0].Text != "src/fetch.ts line 12" {
		t.Errorf("expected resolved content for line 12, got %+v", excerpt.Lines[0])
	}
}

func TestAdvancingStopsAtTheLastStep(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough())

	for range 5 {
		if err := session.Advance(); err != nil {
			t.Fatalf("expected advancing to clamp rather than fail, got %v", err)
		}
	}

	if view := session.View(); view.Position != 1 {
		t.Errorf("expected to stay at the last Step, got position %d", view.Position)
	}
}

func TestAdvancingWithNothingPostedIsRejected(t *testing.T) {
	session := newSession()

	err := session.Advance()

	assertRejected(t, err, review.RejectedNoWalkthrough)
}

func TestAFailedResolutionIsAProblemShownInPlaceOfCode(t *testing.T) {
	session := review.NewSession(failingResolver{})
	mustPost(t, session, validWalkthrough())

	if err := session.Advance(); err != nil {
		t.Fatal(err)
	}

	excerpt := session.View().Step.Excerpts[0]
	if len(excerpt.Lines) != 0 {
		t.Error("expected no lines when resolution failed")
	}
	if !strings.Contains(excerpt.Problem, "cannot read") {
		t.Errorf("expected the problem to be shown, got %q", excerpt.Problem)
	}
}

type failingResolver struct{}

func (failingResolver) Resolve(e review.Excerpt) ([]review.Line, error) {
	return nil, fmt.Errorf("cannot read %s", e.File)
}

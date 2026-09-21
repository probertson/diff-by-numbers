package review_test

import (
	"fmt"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// The tests in this file all work on one file of one repository, the same one
// validWalkthrough names, so a case reads as the line numbers it is about.
const (
	testRepository = "/repos/argus-portal"
	testFile       = "src/fetch.ts"
)

// changedOn is a run of Changed Lines on one side of the shared test file.
func changedOn(side review.Side, first, last int) []review.ChangedLine {
	var lines []review.ChangedLine
	for n := first; n <= last; n++ {
		lines = append(lines, lineAt(side, n))
	}
	return lines
}

func lineAt(side review.Side, n int) review.ChangedLine {
	return review.ChangedLine{Repository: testRepository, File: testFile, Side: side, Line: n}
}

// at is one Excerpt on the shared test file.
func at(side review.Side, first, last int) review.Excerpt {
	return review.Excerpt{Repository: testRepository, File: testFile, Side: side, FirstLine: first, LastLine: last}
}

// walkthroughShowing puts each group of Excerpts in a Step of its own, so a test
// can state the shape the absorption rules speak of: Excerpt order within a
// Step, and Step order between them.
func walkthroughShowing(groups ...[]review.Excerpt) review.Walkthrough {
	w := validWalkthrough()
	w.Steps = nil
	for i, group := range groups {
		w.Steps = append(w.Steps, review.Step{
			Name:        fmt.Sprintf("Section %d", i+1),
			Explanation: "what this section does and why",
			Excerpts:    group,
		})
	}
	return w
}

// storedRange reads back the range dbn kept for an Excerpt, one-based in both
// arguments. It goes through the view because that is what the Reviewer sees:
// the stored range and the rendered code are the same thing.
func storedRange(t *testing.T, session *review.Session, step, excerpt int) review.Excerpt {
	t.Helper()
	if err := session.GoTo(step); err != nil {
		t.Fatalf("could not go to Step %d: %v", step, err)
	}
	view := session.View()
	if view.Step == nil {
		t.Fatalf("Step %d has no view", step)
	}
	if len(view.Step.Excerpts) < excerpt {
		t.Fatalf("Step %d has %d Excerpts, wanted at least %d", step, len(view.Step.Excerpts), excerpt)
	}
	return view.Step.Excerpts[excerpt-1].Excerpt
}

func TestABlankLineBetweenTwoExcerptsIsAbsorbedByTheEarlierOne(t *testing.T) {
	// A new file listed as two sections, with the blank line separating them
	// left out. It is a Changed Line like any other, and would otherwise refuse
	// the post for a line nobody needs to be told to read.
	session := review.NewSession(stubResolver{}, fixedDeriver{
		lines:      changedOn(review.NewSide, 1, 10),
		whitespace: []review.ChangedLine{lineAt(review.NewSide, 6)},
	})

	err := session.Post(walkthroughShowing([]review.Excerpt{at(review.NewSide, 1, 5), at(review.NewSide, 7, 10)}))

	if err != nil {
		t.Fatalf("expected the Walkthrough to be accepted, got %v", err)
	}
	if got := storedRange(t, session, 1, 1); got.LastLine != 6 {
		t.Errorf("expected the earlier Excerpt to widen to 1-6, got %d-%d", got.FirstLine, got.LastLine)
	}
}

func TestTheEarlierStepWinsABlankLineBetweenTwoExcerpts(t *testing.T) {
	// Step 1 shows the later lines and Step 2 the earlier ones, so the Excerpt
	// nearer the blank line in the file is not the earlier one in the
	// Walkthrough. The rule is Step order, so Step 1 takes it.
	session := review.NewSession(stubResolver{}, fixedDeriver{
		lines:      changedOn(review.NewSide, 1, 10),
		whitespace: []review.ChangedLine{lineAt(review.NewSide, 6)},
	})

	err := session.Post(walkthroughShowing(
		[]review.Excerpt{at(review.NewSide, 7, 10)},
		[]review.Excerpt{at(review.NewSide, 1, 5)},
	))

	if err != nil {
		t.Fatalf("expected the Walkthrough to be accepted, got %v", err)
	}
	if got := storedRange(t, session, 1, 1); got.FirstLine != 6 {
		t.Errorf("expected Step 1's Excerpt to widen to 6-10, got %d-%d", got.FirstLine, got.LastLine)
	}
	if got := storedRange(t, session, 2, 1); got.LastLine != 5 {
		t.Errorf("expected Step 2's Excerpt to stay at 1-5, got %d-%d", got.FirstLine, got.LastLine)
	}
}

func TestARunOfSeveralBlankLinesIsAbsorbedWhole(t *testing.T) {
	session := review.NewSession(stubResolver{}, fixedDeriver{
		lines:      changedOn(review.NewSide, 1, 12),
		whitespace: changedOn(review.NewSide, 6, 8),
	})

	err := session.Post(walkthroughShowing([]review.Excerpt{at(review.NewSide, 1, 5), at(review.NewSide, 9, 12)}))

	if err != nil {
		t.Fatalf("expected the Walkthrough to be accepted, got %v", err)
	}
	if got := storedRange(t, session, 1, 1); got.LastLine != 8 {
		t.Errorf("expected the earlier Excerpt to widen to 1-8, got %d-%d", got.FirstLine, got.LastLine)
	}
}

func TestABlankLineTouchingNoExcerptStillHasToBeCovered(t *testing.T) {
	// Absorption is a widening of what is already shown, not an exemption: a
	// blank line on its own is still a change the Reviewer never saw, and blank
	// lines can carry meaning — a Markdown paragraph break, a YAML block scalar.
	session := review.NewSession(stubResolver{}, fixedDeriver{
		lines:      append(changedOn(review.NewSide, 1, 5), lineAt(review.NewSide, 20)),
		whitespace: []review.ChangedLine{lineAt(review.NewSide, 20)},
	})

	err := session.Post(walkthroughShowing([]review.Excerpt{at(review.NewSide, 1, 5)}))

	assertRejected(t, err, review.RejectedUncoveredChanges)
	assertDetailContains(t, err, "20")
}

func TestAnOldSideBlankLineIsAbsorbedByAnOldSideExcerpt(t *testing.T) {
	// Deletions are excerpted on the old side, and a deleted section's blank
	// separators are skipped just as readily as a new file's.
	session := review.NewSession(stubResolver{}, fixedDeriver{
		lines:      changedOn(review.OldSide, 1, 10),
		whitespace: []review.ChangedLine{lineAt(review.OldSide, 6)},
	})

	err := session.Post(walkthroughShowing([]review.Excerpt{at(review.OldSide, 1, 5), at(review.OldSide, 7, 10)}))

	if err != nil {
		t.Fatalf("expected the Walkthrough to be accepted, got %v", err)
	}
	if got := storedRange(t, session, 1, 1); got.LastLine != 6 {
		t.Errorf("expected the earlier Excerpt to widen to 1-6, got %d-%d", got.FirstLine, got.LastLine)
	}
}

func TestAnAbsorbedLineIsDrawnInTheStep(t *testing.T) {
	// The widened range is the stored one, so the absorbed line is rendered like
	// any other: dbn never counts a line as accounted for without showing it.
	session := review.NewSession(stubResolver{}, fixedDeriver{
		lines:      changedOn(review.NewSide, 1, 10),
		whitespace: []review.ChangedLine{lineAt(review.NewSide, 6)},
	})

	if err := session.Post(walkthroughShowing([]review.Excerpt{at(review.NewSide, 1, 5), at(review.NewSide, 7, 10)})); err != nil {
		t.Fatalf("expected the Walkthrough to be accepted, got %v", err)
	}
	if err := session.GoTo(1); err != nil {
		t.Fatalf("could not go to Step 1: %v", err)
	}

	drawn := session.View().Step.Excerpts[0].Lines

	if !drawsLine(drawn, 6) {
		t.Errorf("expected the absorbed line 6 to be drawn, got %v", drawn)
	}
}

func drawsLine(lines []review.Line, number int) bool {
	for _, line := range lines {
		if line.Number == number {
			return true
		}
	}
	return false
}

func TestALineAnAcknowledgementAlreadyCoversIsNotAbsorbed(t *testing.T) {
	// Absorption exists to save a post that would otherwise be refused, so it
	// widens only over lines nothing accounts for. A file covered wholesale by an
	// Acknowledgement has no such lines, and the Excerpt beside them stays as the
	// agent drew it.
	walkthrough := walkthroughShowing([]review.Excerpt{at(review.NewSide, 1, 5)})
	walkthrough.Steps[0].Acknowledgements = []review.Acknowledgement{{
		Repository: testRepository,
		Files:      []string{testFile},
		Reason:     "generated, and read as a whole or not at all",
	}}
	session := review.NewSession(stubResolver{}, fixedDeriver{
		lines:      changedOn(review.NewSide, 1, 10),
		whitespace: []review.ChangedLine{lineAt(review.NewSide, 6)},
	})

	err := session.Post(walkthrough)

	if err != nil {
		t.Fatalf("expected the Walkthrough to be accepted, got %v", err)
	}
	if got := storedRange(t, session, 1, 1); got.LastLine != 5 {
		t.Errorf("expected the Excerpt to stay at 1-5, got %d-%d", got.FirstLine, got.LastLine)
	}
}

func TestAbsorbedBlankLinesDoNotCountTowardTheBudget(t *testing.T) {
	// Thirty lines of code is exactly the budget. The three blank separators dbn
	// absorbed on the agent's behalf must not be what pushes it over, or the
	// widening would hand back a rejection with the other hand.
	blanks := []review.ChangedLine{lineAt(review.NewSide, 31), lineAt(review.NewSide, 32), lineAt(review.NewSide, 33)}
	session := review.NewSession(stubResolver{}, fixedDeriver{
		lines:      changedOn(review.NewSide, 1, 33),
		whitespace: blanks,
	})

	err := session.Post(walkthroughShowing([]review.Excerpt{at(review.NewSide, 1, 30), at(review.NewSide, 34, 34)}))

	if err != nil {
		t.Fatalf("expected 30 code lines plus 3 absorbed blanks to be within the budget, got %v", err)
	}
}

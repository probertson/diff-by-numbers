package review_test

import (
	"fmt"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// The budget exists to keep what a Step asks the Reviewer to read manageable. A
// line pre-marked as seen by a Revision Round has already been read, so counting
// it refuses a Step for work nobody has to do again.

// finishFirstRoundOver posts a first Round covering a range of app.ts and
// hands it off with a Comment, leaving those lines pre-marked for the Revision
// Round that follows. The first round carries a justification because its size
// is not what these tests are about.
func finishFirstRoundOver(t *testing.T, first, last int) (*review.Session, *roundDeriver) {
	t.Helper()
	deriver := &roundDeriver{lines: changedApp(first, last)}
	step := appStep(first, last)
	step.OversizeJustification = "the size of the first round is not what this is about"
	return keptOpenFirstRound(t, deriver, []review.Step{step}), deriver
}

// editedApp is a rewrite of app.ts: both sides of the same line numbers, paired
// so the before-side rides along with the after-side that replaced it.
func editedApp(first, last int) ([]review.ChangedLine, []review.Correspondence) {
	var lines []review.ChangedLine
	for n := first; n <= last; n++ {
		lines = append(lines,
			review.ChangedLine{File: "app.ts", Side: review.OldSide, Line: n},
			review.ChangedLine{File: "app.ts", Side: review.NewSide, Line: n})
	}
	return lines, []review.Correspondence{{
		File:     "app.ts",
		OldFirst: first, OldLast: last,
		NewFirst: first, NewLast: last,
	}}
}

// touching names the lines of app.ts the mapping should report as moved.
func touching(first, last int) map[string]bool {
	moved := map[string]bool{}
	for n := first; n <= last; n++ {
		moved[fmt.Sprintf("app.ts:%d", n)] = true
	}
	return moved
}

func TestABudgetCountsOnlyTheLinesARevisionRoundHasNotAlreadyShown(t *testing.T) {
	session, deriver := finishFirstRoundOver(t, 1, 40)
	deriver.lines = changedApp(1, 45)
	deriver.touched = touching(41, 45)

	err := session.Revise(session.ReviewID(), revising(appRound([]review.Step{appStep(1, 45)}, nil)))

	if err != nil {
		t.Fatalf("expected 40 already-read lines plus 5 new ones to count 5, got %v", err)
	}
}

func TestARevisionRoundIsStillRefusedForTooMuchNewReading(t *testing.T) {
	session, deriver := finishFirstRoundOver(t, 1, 3)
	deriver.lines = changedApp(1, 38)
	deriver.touched = touching(4, 38)

	err := session.Revise(session.ReviewID(), revising(appRound([]review.Step{appStep(1, 38)}, nil)))

	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, "35 changed lines")
}

func TestAStepOfNothingButAlreadyReadLinesCountsZero(t *testing.T) {
	// The case that prompted this: a Revision Round whose Step re-shows a
	// stretch of context around a fix, all of it already reviewed, refused as
	// oversized with no justification to give.
	session, deriver := finishFirstRoundOver(t, 1, 40)
	deriver.lines = changedApp(1, 41)
	deriver.touched = touching(41, 41)

	err := session.Revise(session.ReviewID(), revising(appRound([]review.Step{appStep(1, 40), appStep(41, 41)}, nil)))

	if err != nil {
		t.Fatalf("expected a Step of wholly already-read lines to count zero, got %v", err)
	}
}

func TestAnAlreadyShownBeforeSideLineIsFreeWhenItRidesAlong(t *testing.T) {
	// A rewrite's before-side rides along with the after-side that replaced it
	// and costs against the budget like any shown line. Once a round has shown
	// it, it costs nothing — the exemption has to reach the old side too, or a
	// Revision Round over a rewrite is refused for reading nobody has to redo.
	lines, pairs := editedApp(1, 20)
	deriver := &roundDeriver{lines: lines, correspondences: pairs}
	step := appStep(1, 20)
	step.OversizeJustification = "the size of the first round is not what this is about"
	session := keptOpenFirstRound(t, deriver, []review.Step{step})

	// Every new-side line moved, so only the old side — unmoved against an
	// unmoved merge-base — is pre-marked.
	deriver.touched = touching(1, 20)

	err := session.Revise(session.ReviewID(), revising(appRound([]review.Step{appStep(1, 20)}, nil)))

	if err != nil {
		t.Fatalf("expected the 20 already-shown before-side lines to be free, got %v", err)
	}
}

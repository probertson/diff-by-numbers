package review_test

import (
	"fmt"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

const revRepo = "/r"

// roundDeriver returns whatever Changed Lines it is currently set to, so a test
// can re-derive a different Change Set for a Revision Round on the same Session.
type roundDeriver struct{ lines []review.ChangedLine }

func (d *roundDeriver) Derive(repo review.Repository) (review.Derivation, error) {
	out := make([]review.ChangedLine, len(d.lines))
	for i, line := range d.lines {
		line.Repository = repo.Root
		out[i] = line
	}
	return review.Derivation{Lines: out}, nil
}

// textResolver resolves new-side lines from a map a test can mutate between
// rounds, defaulting to a stable per-line string. Editing an entry stands in for
// an edit on disk, so the same line number can carry different content per round.
type textResolver struct{ text map[string]string }

func (r *textResolver) Resolve(e review.Excerpt) ([]review.Line, error) {
	if e.Side == review.OldSide {
		return nil, fmt.Errorf("old side unavailable")
	}
	var lines []review.Line
	for n := e.FirstLine; n <= e.LastLine; n++ {
		key := fmt.Sprintf("%s:%d", e.File, n)
		text, ok := r.text[key]
		if !ok {
			text = fmt.Sprintf("%s line %d", e.File, n)
		}
		lines = append(lines, review.Line{Number: n, Text: text})
	}
	return lines, nil
}

func appStep(first, last int) review.Step {
	return review.Step{
		Name: "Step", Explanation: "e",
		Excerpts: []review.Excerpt{{Repository: revRepo, File: "app.ts", Side: review.NewSide, FirstLine: first, LastLine: last}},
	}
}

func appWalkthrough(steps []review.Step, dispositions []review.Disposition) review.Walkthrough {
	return review.Walkthrough{
		Brief:        review.Brief{Ask: "x", Approach: "y", Provenance: review.Provenance{Kind: review.ProvenanceStated, Citation: "s"}},
		ChangeSet:    review.ChangeSet{Repositories: []review.Repository{{Root: revRepo, Range: "main"}}},
		Steps:        steps,
		Dispositions: dispositions,
	}
}

func changedApp(first, last int) []review.ChangedLine {
	var lines []review.ChangedLine
	for n := first; n <= last; n++ {
		lines = append(lines, review.ChangedLine{File: "app.ts", Side: review.NewSide, Line: n})
	}
	return lines
}

// finishRound1 posts a first Walkthrough over app.ts:1-3 and finishes it, leaving
// the Session ready for a Revision Round. It returns the deriver and resolver so
// the caller can re-derive and edit content for round two.
func finishRound1(t *testing.T) (*review.Session, *roundDeriver, *textResolver) {
	t.Helper()
	deriver := &roundDeriver{lines: changedApp(1, 3)}
	resolver := &textResolver{text: map[string]string{}}
	session := review.NewSession(resolver, deriver)
	mustPost(t, session, appWalkthrough([]review.Step{appStep(1, 3)}, nil))
	if err := session.Finish(); err != nil {
		t.Fatalf("expected round 1 to finish, got %v", err)
	}
	return session, deriver, resolver
}

func TestARevisionRoundIsAcceptedAfterFinishAndScopedToWhatMoved(t *testing.T) {
	session, deriver, _ := finishRound1(t)

	// The fix added line 4; lines 1-3 are unchanged.
	deriver.lines = changedApp(1, 4)

	// Covering only the moved line is enough: 1-3 are pre-marked as shown.
	err := session.Post(appWalkthrough([]review.Step{appStep(4, 4)}, nil))

	if err != nil {
		t.Fatalf("expected a Revision Round covering only what moved to be accepted, got %v", err)
	}
	view := session.View()
	if view.Coverage.Total != 4 {
		t.Errorf("expected the full re-derived Change Set of 4 lines, got total %d", view.Coverage.Total)
	}
	if view.Coverage.Seen != 3 {
		t.Errorf("expected the 3 unchanged lines pre-marked as seen, got %d", view.Coverage.Seen)
	}
}

func TestARevisionRoundStillDemandsTheLinesThatMoved(t *testing.T) {
	session, deriver, _ := finishRound1(t)
	deriver.lines = changedApp(1, 4) // line 4 moved

	// Covering only a pre-shown line leaves the moved line 4 unaccounted for.
	err := session.Post(appWalkthrough([]review.Step{appStep(1, 1)}, nil))

	assertRejected(t, err, review.RejectedUncoveredChanges)
	assertDetailContains(t, err, "4")
}

func TestARevisionRoundDetectsAChangedLineByContentNotNumber(t *testing.T) {
	session, deriver, resolver := finishRound1(t)
	deriver.lines = changedApp(1, 3)    // the same line numbers
	resolver.text["app.ts:2"] = "MOVED" // but line 2's content changed

	// Line 2 moved, 1 and 3 did not: covering only line 2 suffices.
	if err := session.Post(appWalkthrough([]review.Step{appStep(2, 2)}, nil)); err != nil {
		t.Fatalf("expected covering the content-changed line to be accepted, got %v", err)
	}
	if seen := session.View().Coverage.Seen; seen != 2 {
		t.Errorf("expected lines 1 and 3 pre-marked as seen, got %d", seen)
	}
}

func TestAFirstWalkthroughRejectsDispositions(t *testing.T) {
	deriver := &roundDeriver{lines: changedApp(1, 3)}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver)

	err := session.Post(appWalkthrough([]review.Step{appStep(1, 3)},
		[]review.Disposition{{ChangeRequestID: 1, Status: review.DispositionAddressed}}))

	assertRejected(t, err, review.RejectedMalformedDisposition)
}

// finishRound1WithCR posts round 1, raises one Change Request, and finishes.
func finishRound1WithCR(t *testing.T) (*review.Session, *roundDeriver) {
	t.Helper()
	deriver := &roundDeriver{lines: changedApp(1, 3)}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver)
	mustPost(t, session, appWalkthrough([]review.Step{appStep(1, 3)}, nil))
	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}
	if _, err := session.RaiseChangeRequest(review.AnchorTarget{ExcerptIndex: 0, FirstLine: 2, LastLine: 2}, "please fix line 2"); err != nil {
		t.Fatalf("expected to raise a Change Request, got %v", err)
	}
	if err := session.Finish(); err != nil {
		t.Fatal(err)
	}
	return session, deriver
}

func TestARevisionRoundMustDisposeEveryPreviousChangeRequest(t *testing.T) {
	session, deriver := finishRound1WithCR(t)
	deriver.lines = changedApp(1, 4)

	err := session.Post(appWalkthrough([]review.Step{appStep(4, 4)}, nil)) // no dispositions

	assertRejected(t, err, review.RejectedMalformedDisposition)
	assertDetailContains(t, err, "1")
}

func TestADeclinedDispositionNeedsAReason(t *testing.T) {
	session, deriver := finishRound1WithCR(t)
	deriver.lines = changedApp(1, 4)

	err := session.Post(appWalkthrough([]review.Step{appStep(4, 4)},
		[]review.Disposition{{ChangeRequestID: 1, Status: review.DispositionDeclined}}))

	assertRejected(t, err, review.RejectedMalformedDisposition)
}

func TestADispositionForAnUnknownChangeRequestIsRejected(t *testing.T) {
	session, deriver := finishRound1WithCR(t)
	deriver.lines = changedApp(1, 4)

	err := session.Post(appWalkthrough([]review.Step{appStep(4, 4)},
		[]review.Disposition{
			{ChangeRequestID: 1, Status: review.DispositionAddressed},
			{ChangeRequestID: 99, Status: review.DispositionAddressed},
		}))

	assertRejected(t, err, review.RejectedMalformedDisposition)
	assertDetailContains(t, err, "99")
}

func TestDispositionsAreVisibleBeforeAnyCode(t *testing.T) {
	session, deriver := finishRound1WithCR(t)
	deriver.lines = changedApp(1, 4)
	mustPost(t, session, appWalkthrough([]review.Step{appStep(4, 4)},
		[]review.Disposition{{ChangeRequestID: 1, Status: review.DispositionDeclined, Reasoning: "the current behaviour is intended"}}))

	view := session.View() // position 0 — the Brief, before any code
	if len(view.Dispositions) != 1 {
		t.Fatalf("expected one disposition on the Brief, got %d", len(view.Dispositions))
	}
	if view.Dispositions[0].Status != review.DispositionDeclined || view.Dispositions[0].Reasoning == "" {
		t.Errorf("expected a declined disposition carrying its reasoning, got %+v", view.Dispositions[0])
	}
}

func TestADeclinedChangeRequestCanBeReRaised(t *testing.T) {
	session, deriver := finishRound1WithCR(t)
	deriver.lines = changedApp(1, 4)
	mustPost(t, session, appWalkthrough([]review.Step{appStep(4, 4)},
		[]review.Disposition{{ChangeRequestID: 1, Status: review.DispositionDeclined, Reasoning: "intended"}}))

	cr, err := session.ReRaise(1)

	if err != nil {
		t.Fatalf("expected to re-raise a declined Change Request, got %v", err)
	}
	if cr.Note != "please fix line 2" {
		t.Errorf("expected the re-raised note carried over, got %q", cr.Note)
	}
	if len(session.ChangeRequests()) != 1 {
		t.Errorf("expected the re-raised Change Request to stand in the new round, got %d", len(session.ChangeRequests()))
	}
}

func TestOnlyADeclinedChangeRequestCanBeReRaised(t *testing.T) {
	session, deriver := finishRound1WithCR(t)
	deriver.lines = changedApp(1, 4)
	mustPost(t, session, appWalkthrough([]review.Step{appStep(4, 4)},
		[]review.Disposition{{ChangeRequestID: 1, Status: review.DispositionAddressed}}))

	_, err := session.ReRaise(1)

	assertRejected(t, err, review.RejectedNoSuchChangeRequest)
}

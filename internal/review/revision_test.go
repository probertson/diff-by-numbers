package review_test

import (
	"fmt"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

const revRepo = "/r"

// roundDeriver returns whatever Changed Lines it is currently set to, so a test
// can re-derive a different Change Set for a Revision Round on the same Session.
//
// It also stands in for the git adapter's snapshot capability. A test says which
// atoms it moved since the previous round by setting `touched`; everything else
// is reported as sitting exactly where it was. That is the shape of the real
// mapping — a diff of two working-tree snapshots — without a repository on disk.
// The behaviour of the real thing is covered against real repositories in
// internal/git.
type roundDeriver struct {
	lines []review.ChangedLine
	// touched names what moved since the previous round: "file:line" for a
	// Changed Line, or a bare file name for an Opaque Change. Empty means
	// nothing moved, which is the ordinary case for a round that only answers
	// Comments.
	touched map[string]bool
	// renamed says what a file was called in the previous round, for the cases
	// that move one between rounds.
	renamed map[string]string
}

func (d *roundDeriver) Derive(repo review.Repository) (review.Derivation, error) {
	out := make([]review.ChangedLine, len(d.lines))
	for i, line := range d.lines {
		line.Repository = repo.Root
		out[i] = line
	}
	// A constant base: unchanged between rounds, so old-side lines map by
	// identity, which is what happens when nobody rebases mid-review.
	return review.Derivation{Lines: out, Base: "base"}, nil
}

func (d *roundDeriver) Snapshot(string) (string, error) { return "snapshot", nil }

func (d *roundDeriver) MapBetween(_, _, _ string) (review.RoundMapping, error) {
	return fakeMapping{touched: d.touched, renamed: d.renamed}, nil
}

// fakeMapping reports the atoms a test named as touched, and everything else as
// unmoved and in place.
type fakeMapping struct {
	touched map[string]bool
	renamed map[string]string
}

func (m fakeMapping) Lookup(file string, line int) (review.Position, bool) {
	if m.touched[fmt.Sprintf("%s:%d", file, line)] {
		return review.Position{}, false
	}
	return review.Position{File: file, Line: line}, true
}

func (m fakeMapping) Touched(file string) bool { return m.touched[file] }

// renamed maps a current file name to what it was called last round; empty
// means the file has not moved, which is the ordinary case.
func (m fakeMapping) PathIn(file string) string {
	if was, ok := m.renamed[file]; ok {
		return was
	}
	return file
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
		ChangeSet:    review.ChangeSet{Repositories: []review.Repository{{Root: revRepo, Base: "main"}}},
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
// the Session ready for a Revision Round. It returns the deriver so the caller
// can re-derive a different Change Set, and say what moved, for round two.
func finishRound1(t *testing.T) (*review.Session, *roundDeriver) {
	t.Helper()
	deriver := &roundDeriver{lines: changedApp(1, 3)}
	resolver := &textResolver{text: map[string]string{}}
	session := review.NewSession(resolver, deriver)
	mustPost(t, session, appWalkthrough([]review.Step{appStep(1, 3)}, nil))
	if err := session.Finish(); err != nil {
		t.Fatalf("expected round 1 to finish, got %v", err)
	}
	return session, deriver
}

func TestARevisionRoundIsAcceptedAfterFinishAndScopedToWhatMoved(t *testing.T) {
	session, deriver := finishRound1(t)

	// The fix added line 4; lines 1-3 are unchanged.
	deriver.lines = changedApp(1, 4)
	deriver.touched = map[string]bool{"app.ts:4": true}

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
	session, deriver := finishRound1(t)
	deriver.lines = changedApp(1, 4) // line 4 moved
	deriver.touched = map[string]bool{"app.ts:4": true}

	// Covering only a pre-shown line leaves the moved line 4 unaccounted for.
	err := session.Post(appWalkthrough([]review.Step{appStep(1, 1)}, nil))

	assertRejected(t, err, review.RejectedUncoveredChanges)
	assertDetailContains(t, err, "4")
}

func TestARevisionRoundDemandsOnlyTheLineTheMappingSaysWasTouched(t *testing.T) {
	session, deriver := finishRound1(t)
	deriver.lines = changedApp(1, 3) // the same line numbers
	deriver.touched = map[string]bool{"app.ts:2": true}

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
		[]review.Disposition{{CommentID: 1, Status: review.DispositionAddressed}}))

	assertRejected(t, err, review.RejectedMalformedDisposition)
}

// finishRound1WithComment posts round 1, raises one Comment, and finishes.
func finishRound1WithComment(t *testing.T) (*review.Session, *roundDeriver) {
	t.Helper()
	deriver := &roundDeriver{lines: changedApp(1, 3)}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver)
	mustPost(t, session, appWalkthrough([]review.Step{appStep(1, 3)}, nil))
	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}
	if _, err := session.RaiseComment(span(0, 2, 2), "please fix line 2"); err != nil {
		t.Fatalf("expected to raise a Comment, got %v", err)
	}
	if err := session.Finish(); err != nil {
		t.Fatal(err)
	}
	return session, deriver
}

func TestARevisionRoundMustDisposeEveryPreviousComment(t *testing.T) {
	session, deriver := finishRound1WithComment(t)
	deriver.lines = changedApp(1, 4)

	err := session.Post(appWalkthrough([]review.Step{appStep(4, 4)}, nil)) // no dispositions

	assertRejected(t, err, review.RejectedMalformedDisposition)
	assertDetailContains(t, err, "1")
}

func TestADeclinedDispositionNeedsAReason(t *testing.T) {
	session, deriver := finishRound1WithComment(t)
	deriver.lines = changedApp(1, 4)

	err := session.Post(appWalkthrough([]review.Step{appStep(4, 4)},
		[]review.Disposition{{CommentID: 1, Status: review.DispositionDeclined}}))

	assertRejected(t, err, review.RejectedMalformedDisposition)
}

func TestAnAnsweredDispositionNeedsAResponse(t *testing.T) {
	session, deriver := finishRound1WithComment(t)
	deriver.lines = changedApp(1, 4)

	err := session.Post(appWalkthrough([]review.Step{appStep(4, 4)},
		[]review.Disposition{{CommentID: 1, Status: review.DispositionAnswered}}))

	assertRejected(t, err, review.RejectedMalformedDisposition)
	assertDetailContains(t, err, "1")
}

func TestAnAnsweredDispositionCarriesItsResponse(t *testing.T) {
	session, deriver := finishRound1WithComment(t)
	deriver.lines = changedApp(1, 4)

	mustPost(t, session, appWalkthrough([]review.Step{appStep(4, 4)},
		[]review.Disposition{{CommentID: 1, Status: review.DispositionAnswered, Response: "it guards the retry loop"}}))

	got := session.View().Dispositions
	if len(got) != 1 || got[0].Status != review.DispositionAnswered || got[0].Response != "it guards the retry loop" {
		t.Errorf("expected an answered disposition carrying its response, got %+v", got)
	}
}

func TestAnAddressedDispositionMayCarryAResponse(t *testing.T) {
	session, deriver := finishRound1WithComment(t)
	deriver.lines = changedApp(1, 4)

	mustPost(t, session, appWalkthrough([]review.Step{appStep(4, 4)},
		[]review.Disposition{{CommentID: 1, Status: review.DispositionAddressed, Response: "fixed, and the twin in retry.ts too"}}))

	got := session.View().Dispositions
	if len(got) != 1 || got[0].Response != "fixed, and the twin in retry.ts too" {
		t.Errorf("expected the addressed disposition to keep its response, got %+v", got)
	}
}

func TestADispositionForAnUnknownCommentIsRejected(t *testing.T) {
	session, deriver := finishRound1WithComment(t)
	deriver.lines = changedApp(1, 4)

	err := session.Post(appWalkthrough([]review.Step{appStep(4, 4)},
		[]review.Disposition{
			{CommentID: 1, Status: review.DispositionAddressed},
			{CommentID: 99, Status: review.DispositionAddressed},
		}))

	assertRejected(t, err, review.RejectedMalformedDisposition)
	assertDetailContains(t, err, "99")
}

func TestDispositionsAreVisibleBeforeAnyCode(t *testing.T) {
	session, deriver := finishRound1WithComment(t)
	deriver.lines = changedApp(1, 4)
	mustPost(t, session, appWalkthrough([]review.Step{appStep(4, 4)},
		[]review.Disposition{{CommentID: 1, Status: review.DispositionDeclined, Response: "the current behaviour is intended"}}))

	view := session.View() // position 0 — the Brief, before any code
	if len(view.Dispositions) != 1 {
		t.Fatalf("expected one disposition on the Brief, got %d", len(view.Dispositions))
	}
	if view.Dispositions[0].Status != review.DispositionDeclined || view.Dispositions[0].Response == "" {
		t.Errorf("expected a declined disposition carrying its response, got %+v", view.Dispositions[0])
	}
}

func TestADeclinedCommentCanBeReRaised(t *testing.T) {
	session, deriver := finishRound1WithComment(t)
	deriver.lines = changedApp(1, 4)
	mustPost(t, session, appWalkthrough([]review.Step{appStep(4, 4)},
		[]review.Disposition{{CommentID: 1, Status: review.DispositionDeclined, Response: "intended"}}))

	comment, err := session.ReRaise(1, "")

	if err != nil {
		t.Fatalf("expected to re-raise a declined Comment, got %v", err)
	}
	if comment.Note != "please fix line 2" {
		t.Errorf("expected the re-raised note carried over, got %q", comment.Note)
	}
	// The previous round's Step number means nothing in this round; the re-raised
	// Comment is not tied to a current Step, so it cannot flag the wrong one.
	if comment.Step != 0 {
		t.Errorf("expected a re-raised Comment not to claim a current Step, got Step %d", comment.Step)
	}
	if len(session.Comments()) != 1 {
		t.Errorf("expected the re-raised Comment to stand in the new round, got %d", len(session.Comments()))
	}
}

// The safety property the old uniqueness rule bought, kept without it: a line
// the mapping says was touched is demanded, whatever its text happens to be.
// Positional matching gets this right by construction — there is no content to
// collide — where content matching had to disqualify every repeated line to be
// safe, and so disqualified most boilerplate in the bargain.
func TestABrandNewLineIsDemandedEvenWhereItsTextRepeats(t *testing.T) {
	deriver := &roundDeriver{lines: changedApp(1, 2)}
	resolver := &textResolver{text: map[string]string{"app.ts:1": "}", "app.ts:2": "keep"}}
	session := review.NewSession(resolver, deriver)
	mustPost(t, session, appWalkthrough([]review.Step{appStep(1, 2)}, nil))
	if err := session.Finish(); err != nil {
		t.Fatal(err)
	}

	// The fix adds a brand-new line 3 whose text collides with line 1's "}" —
	// which used to be enough to let it escape, and now is simply irrelevant.
	deriver.lines = changedApp(1, 3)
	resolver.text["app.ts:3"] = "}"
	deriver.touched = map[string]bool{"app.ts:3": true}

	err := session.Post(appWalkthrough([]review.Step{appStep(1, 1)}, nil))

	assertRejected(t, err, review.RejectedUncoveredChanges)
	assertDetailContains(t, err, "3")
}

func TestOnlyADeclinedCommentCanBeReRaised(t *testing.T) {
	session, deriver := finishRound1WithComment(t)
	deriver.lines = changedApp(1, 4)
	mustPost(t, session, appWalkthrough([]review.Step{appStep(4, 4)},
		[]review.Disposition{{CommentID: 1, Status: review.DispositionAddressed}}))

	_, err := session.ReRaise(1, "")

	assertRejected(t, err, review.RejectedNoSuchComment)
}

// failingSnapshotDeriver cannot snapshot, standing in for a repository whose
// index is unreadable or whose git call fails.
type failingSnapshotDeriver struct{ roundDeriver }

func (d *failingSnapshotDeriver) Snapshot(string) (string, error) {
	return "", fmt.Errorf("no snapshot for you")
}

// Pre-marking is an optimisation over the coverage guarantee, never a hole in
// it. When the snapshot cannot be taken, nothing is pre-marked and the round
// demands everything — the same conservative answer a daemon restart gives.
func TestAFailedSnapshotPreMarksNothing(t *testing.T) {
	deriver := &failingSnapshotDeriver{roundDeriver{lines: changedApp(1, 3)}}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver)
	mustPost(t, session, appWalkthrough([]review.Step{appStep(1, 3)}, nil))
	if err := session.Finish(); err != nil {
		t.Fatal(err)
	}

	// Round 2 moves nothing at all. With a working snapshot every line would be
	// pre-marked; without one the agent must show them again.
	err := session.Post(appWalkthrough([]review.Step{appStep(1, 1)}, nil))

	assertRejected(t, err, review.RejectedUncoveredChanges)
}

// roundWith opens a Revision Round whose single previous Comment got status.
func roundWith(t *testing.T, status review.DispositionStatus) *review.Session {
	t.Helper()
	session, deriver := finishRound1WithComment(t)
	deriver.lines = changedApp(1, 4)
	mustPost(t, session, appWalkthrough([]review.Step{appStep(4, 4)},
		[]review.Disposition{{CommentID: 1, Status: status, Response: "because"}}))
	return session
}

func TestAReRaisedCommentNamesTheResolutionItDisputes(t *testing.T) {
	session := roundWith(t, review.DispositionDeclined)

	comment, err := session.ReRaise(1, "")

	if err != nil {
		t.Fatalf("expected to re-raise, got %v", err)
	}
	if comment.ReRaisedFrom != 1 {
		t.Errorf("expected the re-raise to point back at Comment 1, got %d", comment.ReRaisedFrom)
	}
}

func TestAnAnsweredCommentCanBeReRaisedToo(t *testing.T) {
	// The code a question was about often is not in the next round, so re-raising
	// — which carries the Anchor — is the only way to follow up in place.
	session := roundWith(t, review.DispositionAnswered)

	_, err := session.ReRaise(1, "")

	if err != nil {
		t.Fatalf("expected an answered Comment to be re-raisable, got %v", err)
	}
}

func TestTheSameResolutionCannotBeReRaisedTwice(t *testing.T) {
	session := roundWith(t, review.DispositionDeclined)
	if _, err := session.ReRaise(1, ""); err != nil {
		t.Fatal(err)
	}

	_, err := session.ReRaise(1, "")

	assertRejected(t, err, review.RejectedAlreadyReRaised)
	if len(session.Comments()) != 1 {
		t.Errorf("a refused re-raise must not add a duplicate, got %d Comments", len(session.Comments()))
	}
}

func TestWithdrawingAReRaiseMakesTheResolutionRaisableAgain(t *testing.T) {
	session := roundWith(t, review.DispositionDeclined)
	first, err := session.ReRaise(1, "")
	if err != nil {
		t.Fatal(err)
	}

	if err := session.WithdrawComment(first.ID); err != nil {
		t.Fatal(err)
	}
	again, err := session.ReRaise(1, "")

	if err != nil {
		t.Fatalf("withdrawing the re-raise should free the resolution, got %v", err)
	}
	if again.ReRaisedFrom != 1 {
		t.Errorf("the second re-raise should point back too, got %d", again.ReRaisedFrom)
	}
}

func TestAReRaiseCarriesTheReviewersFollowUpWhenTheyWroteOne(t *testing.T) {
	session := roundWith(t, review.DispositionDeclined)

	withFollowUp, err := session.ReRaise(1, "I still think this is wrong, because …")

	if err != nil {
		t.Fatal(err)
	}
	if withFollowUp.Note != "I still think this is wrong, because …" {
		t.Errorf("expected the Reviewer's own words, got %q", withFollowUp.Note)
	}
	if withFollowUp.Anchor.Location() == "" {
		t.Error("a re-raise still carries the original Anchor, which is what puts it in place")
	}
}

func TestAReRaiseWithNoFollowUpKeepsTheOriginalNote(t *testing.T) {
	session := roundWith(t, review.DispositionDeclined)

	comment, err := session.ReRaise(1, "")

	if err != nil {
		t.Fatal(err)
	}
	if comment.Note != "please fix line 2" {
		t.Errorf("expected the original note carried over, got %q", comment.Note)
	}
}

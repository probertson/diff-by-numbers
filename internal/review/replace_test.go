package review_test

import (
	"fmt"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// A Round under review can be replaced in place — when the Reviewer asks
// for a change mid-review, or the agent sees its plan was wrong — without the
// hand-off a Revision Round needs.

// underReview posts a first round of app.ts 1-3 and moves the Reviewer into it.
func underReview(t *testing.T) (*review.Session, *roundDeriver) {
	t.Helper()
	deriver := &roundDeriver{lines: changedApp(1, 3)}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver, review.WithIDMinter(minter("rev-1", "rev-2")))
	mustPost(t, session, labeledRound("auth refactor", []review.Step{appStep(1, 3)}))
	mustAdvance(t, session)
	return session, deriver
}

func TestReplacingKeepsTheReviewAndCarriesItsCommentsOver(t *testing.T) {
	session, _ := underReview(t)
	raised := raise(t, session, 2, 2, "rename this")

	err := session.Replace("rev-1", appRound([]review.Step{appStep(1, 3)}, nil))

	if err != nil {
		t.Fatalf("expected the replacement to be accepted, got %v", err)
	}
	if got := session.ReviewID(); got != "rev-1" {
		t.Errorf("expected the review to keep its id, got %q", got)
	}
	if got := session.Label(); got != "auth refactor" {
		t.Errorf("expected the label kept when none is given, got %q", got)
	}
	comments := session.Comments()
	if len(comments) != 1 {
		t.Fatalf("expected the Comment carried over, got %+v", comments)
	}
	carried := comments[0]
	if carried.ID != raised.ID || carried.Note != "rename this" || carried.Anchor.Render() != raised.Anchor.Render() {
		t.Errorf("expected the Comment carried over as raised, got %+v", carried)
	}
	if carried.Step != 0 || !carried.CarriedOver {
		t.Errorf("a carried-over Comment belongs to no Step of the replacement, got Step %d, carried over %v", carried.Step, carried.CarriedOver)
	}
}

func TestACommentRaisedAfterAReplacementDoesNotReuseAnID(t *testing.T) {
	session, _ := underReview(t)
	first := raise(t, session, 2, 2, "one")
	if err := session.Replace("rev-1", appRound([]review.Step{appStep(1, 3)}, nil)); err != nil {
		t.Fatal(err)
	}
	mustAdvance(t, session)

	second := raise(t, session, 3, 3, "two")

	if second.ID == first.ID {
		t.Errorf("expected a fresh id for a Comment raised after the replacement, both are %d", first.ID)
	}
}

func TestReplacingStartsTheReviewerAgainFromTheOverview(t *testing.T) {
	session, _ := underReview(t)

	if err := session.Replace("rev-1", appRound([]review.Step{appStep(1, 3)}, nil)); err != nil {
		t.Fatal(err)
	}

	view := session.View()
	if view.Position != 0 {
		t.Errorf("expected the Reviewer back at the Overview, got position %d", view.Position)
	}
	for i, seen := range view.Seen {
		if seen {
			t.Errorf("expected Step %d unseen after the replacement", i+1)
		}
	}
	if !view.Replaced {
		t.Error("expected the view to say the Round on screen replaced another")
	}
}

func TestAReplacementMayGiveANewLabel(t *testing.T) {
	session, _ := underReview(t)

	if err := session.Replace("rev-1", labeledRound("auth refactor, take 2", []review.Step{appStep(1, 3)})); err != nil {
		t.Fatal(err)
	}

	if got := session.Label(); got != "auth refactor, take 2" {
		t.Errorf("expected the new label, got %q", got)
	}
}

func TestReplacingAnotherReviewIsRefused(t *testing.T) {
	session, _ := underReview(t)

	err := session.Replace("rev-9", appRound([]review.Step{appStep(1, 3)}, nil))

	assertRejected(t, err, review.RejectedUnknownReview)
}

func TestReplacingWithNothingUnderReviewIsRefused(t *testing.T) {
	session := review.NewSession(&textResolver{text: map[string]string{}}, &roundDeriver{lines: changedApp(1, 3)})

	err := session.Replace("rev-1", appRound([]review.Step{appStep(1, 3)}, nil))

	assertRejected(t, err, review.RejectedUnknownReview)
}

func TestReplacingAHandedOffRoundIsRefused(t *testing.T) {
	session, _ := underReview(t)
	raise(t, session, 2, 2, "a point") // so the hand-off leaves a Revision Round to post
	if err := session.Finish(); err != nil {
		t.Fatal(err)
	}

	err := session.Replace("rev-1", appRound([]review.Step{appStep(1, 3)}, nil))

	assertRejected(t, err, review.RejectedRoundHandedOff)
	assertDetailContains(t, err, "post a Revision Round instead")
}

func TestAReplacementIsValidatedLikeAnyPost(t *testing.T) {
	session, _ := underReview(t)

	err := session.Replace("rev-1", appRound([]review.Step{appStep(1, 2)}, nil)) // leaves line 3 out

	assertRejected(t, err, review.RejectedUncoveredChanges)
	if len(session.Comments()) != 0 || session.View().Replaced {
		t.Error("a refused replacement must leave the review as it was")
	}
}

func TestPostingOverAReviewUnderReviewNamesReplaces(t *testing.T) {
	session, _ := underReview(t)

	err := session.Post(appRound([]review.Step{appStep(1, 3)}, nil))

	assertRejected(t, err, review.RejectedWalkthroughActive)
	assertDetailContains(t, err, `replaces: "rev-1"`)
	assertDetailOmits(t, err, "abandon")
}

func TestPostingAfterAConcludedReviewStartsANewOne(t *testing.T) {
	session, _ := underReview(t)
	raise(t, session, 2, 2, "never answered")
	if err := session.Conclude("rev-1"); err != nil {
		t.Fatal(err)
	}

	err := session.Post(appRound([]review.Step{appStep(1, 3)}, nil))

	if err != nil {
		t.Fatalf("expected a concluded review to free the slot, got %v", err)
	}
	if got := session.ReviewID(); got != "rev-2" {
		t.Errorf("expected a new review id, got %q", got)
	}
	if len(session.Comments()) != 0 || len(session.Dispositions()) != 0 {
		t.Error("a new review starts with nothing carried from the concluded one")
	}
}

// revisedTwice runs a first round with one Comment and hands it off, then posts
// a Revision Round addressing it — the state a replacement of a Revision Round
// starts from.
func revisedTwice(t *testing.T, deriver review.Deriver) *review.Session {
	t.Helper()
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver)
	mustPost(t, session, appRound([]review.Step{appStep(1, 3)}, nil))
	mustAdvance(t, session)
	raise(t, session, 2, 2, "fix this")
	if err := session.Finish(); err != nil {
		t.Fatal(err)
	}
	addressed := []review.Disposition{{CommentID: 1, Status: review.DispositionAddressed}}
	mustPost(t, session, appRound([]review.Step{appStep(1, 3)}, addressed))
	return session
}

func TestReplacingARevisionRoundWithoutItsDispositionsIsRefused(t *testing.T) {
	session := revisedTwice(t, &roundDeriver{lines: changedApp(1, 3), touched: map[string]bool{"app.ts:2": true}})

	err := session.Replace(session.ReviewID(), appRound([]review.Step{appStep(1, 3)}, nil))

	assertRejected(t, err, review.RejectedMalformedDisposition)
}

func TestReplacingARevisionRoundTakesItsNewDispositions(t *testing.T) {
	session := revisedTwice(t, &roundDeriver{lines: changedApp(1, 3), touched: map[string]bool{"app.ts:2": true}})
	answered := []review.Disposition{{CommentID: 1, Status: review.DispositionAnswered, Response: "kept, see the note"}}

	err := session.Replace(session.ReviewID(), appRound([]review.Step{appStep(1, 3)}, answered))

	if err != nil {
		t.Fatalf("expected the replacement with dispositions accepted, got %v", err)
	}
	if got := session.Dispositions(); len(got) != 1 || got[0].Status != review.DispositionAnswered {
		t.Errorf("expected the replacement's disposition to stand, got %+v", got)
	}
}

// stagedDeriver snapshots to s1, s2, s3 … in turn, and reports line 2 of app.ts
// as touched only between the first snapshot and a later one. Scoping against
// the first round demands line 2; scoping against any later one would not.
type stagedDeriver struct {
	fixedDeriver
	taken int
}

func (d *stagedDeriver) Derive(repo review.Repository) (review.Derivation, error) {
	derivation, err := d.fixedDeriver.Derive(repo)
	derivation.Base = "base"
	return derivation, err
}

func (d *stagedDeriver) Snapshot(string) (string, error) {
	d.taken++
	return fmt.Sprintf("s%d", d.taken), nil
}

func (d *stagedDeriver) MapBetween(_, from, _ string) (review.RoundMapping, error) {
	if from == "s1" {
		return fakeMapping{touched: map[string]bool{"app.ts:2": true}}, nil
	}
	return fakeMapping{}, nil
}

func TestReplacingARevisionRoundScopesItAgainstThePreviousAcceptedRound(t *testing.T) {
	// Line 2 moved since the first round, so the Revision Round must show it,
	// and so must anything replacing that Revision Round: the Reviewer has not
	// read line 2 as it now stands, whatever the replaced Round showed.
	deriver := &stagedDeriver{fixedDeriver: fixedDeriver{lines: changedApp(1, 3)}}
	session := revisedTwice(t, deriver)
	addressed := []review.Disposition{{CommentID: 1, Status: review.DispositionAddressed}}

	err := session.Replace(session.ReviewID(), appRound([]review.Step{appStep(1, 1)}, addressed))

	assertRejected(t, err, review.RejectedUncoveredChanges)
}

func TestReplacingAReviewHandedOffWithNothingRaisedPointsAtANewReview(t *testing.T) {
	session, _ := underReview(t)
	if err := session.Finish(); err != nil {
		t.Fatal(err)
	}

	err := session.Replace("rev-1", appRound([]review.Step{appStep(1, 3)}, nil))

	assertRejected(t, err, review.RejectedUnknownReview)
	assertDetailContains(t, err, "post without replaces to start a new review")
}

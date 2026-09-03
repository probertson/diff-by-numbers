package review_test

import (
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

func TestAnAnchorCarriesEverythingNeededToPasteIntoChat(t *testing.T) {
	session := newSession() // stubResolver fabricates "<file> line N"
	mustPost(t, session, validWalkthrough())
	mustAdvance(t, session) // Step 1: src/fetch.ts new 12-34

	anchor, err := session.Anchor(review.AnchorTarget{ExcerptIndex: 0, FirstLine: 20, LastLine: 22})
	if err != nil {
		t.Fatalf("expected an Anchor, got %v", err)
	}

	if anchor.File != "src/fetch.ts" || anchor.FirstLine != 20 || anchor.LastLine != 22 {
		t.Errorf("unexpected location: %s:%d-%d", anchor.File, anchor.FirstLine, anchor.LastLine)
	}
	if anchor.StepName != "Add the retrier" {
		t.Errorf("expected the Step name, got %q", anchor.StepName)
	}
	if len(anchor.Lines) != 3 {
		t.Fatalf("expected 3 lines of code, got %d", len(anchor.Lines))
	}

	rendered := anchor.Render()
	// Self-contained: an agent reading only this must know repo, file, lines,
	// which Step, and see the code.
	for _, want := range []string{"argus-portal", "src/fetch.ts", "20", "22", "Add the retrier", "src/fetch.ts line 21"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("expected the rendered Anchor to contain %q\n---\n%s", want, rendered)
		}
	}
}

func TestAnchoringAtTheBriefIsRejected(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough())

	_, err := session.Anchor(review.AnchorTarget{ExcerptIndex: 0, FirstLine: 20, LastLine: 22})

	assertRejected(t, err, review.RejectedNoSuchStep)
}

func TestAnchorMustNameAnExcerptInTheStep(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough())
	mustAdvance(t, session)

	_, err := session.Anchor(review.AnchorTarget{ExcerptIndex: 9, FirstLine: 20, LastLine: 22})

	assertRejected(t, err, review.RejectedBadSelection)
}

func TestAnchorRangeMustLieWithinTheExcerpt(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough()) // excerpt is 12-34
	mustAdvance(t, session)

	cases := map[string]review.AnchorTarget{
		"before the excerpt": {ExcerptIndex: 0, FirstLine: 5, LastLine: 15},
		"after the excerpt":  {ExcerptIndex: 0, FirstLine: 30, LastLine: 40},
		"inverted":           {ExcerptIndex: 0, FirstLine: 25, LastLine: 20},
	}
	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := session.Anchor(target)
			assertRejected(t, err, review.RejectedBadSelection)
		})
	}
}

func TestAnchorReportsWhenTheCodeCannotBeRead(t *testing.T) {
	session := review.NewSession(failingResolver{}, emptyDeriver{})
	walkthrough := validWalkthrough()
	walkthrough.Steps[0].Excerpts[0].Side = review.OldSide // post skips old-side resolution
	mustPost(t, session, walkthrough)
	mustAdvance(t, session)

	_, err := session.Anchor(review.AnchorTarget{ExcerptIndex: 0, FirstLine: 20, LastLine: 22})

	if err == nil {
		t.Fatal("expected anchoring to fail when the code cannot be read")
	}
}

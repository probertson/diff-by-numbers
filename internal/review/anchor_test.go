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

	anchor, err := session.Anchor(span(0, 20, 22))
	if err != nil {
		t.Fatalf("expected an Anchor, got %v", err)
	}

	if anchor.File != "src/fetch.ts" {
		t.Errorf("unexpected file: %s", anchor.File)
	}
	want := []review.AnchorSegment{{Side: review.NewSide, FirstLine: 20, LastLine: 22}}
	if len(anchor.Segments) != 1 || anchor.Segments[0] != want[0] {
		t.Errorf("expected the one-sided Anchor to be a single segment %+v, got %+v", want, anchor.Segments)
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
	for _, want := range []string{"argus-portal", "src/fetch.ts", "after 20-22", "Add the retrier", "src/fetch.ts line 21"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("expected the rendered Anchor to contain %q\n---\n%s", want, rendered)
		}
	}
}

func TestAnchoringAtTheBriefIsRejected(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough())

	_, err := session.Anchor(span(0, 20, 22))

	assertRejected(t, err, review.RejectedNoSuchStep)
}

func TestAnchorMustNameAnExcerptInTheStep(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough())
	mustAdvance(t, session)

	_, err := session.Anchor(span(9, 20, 22))

	assertRejected(t, err, review.RejectedBadSelection)
}

func TestAnchorEndpointsMustNameRowsTheExcerptShows(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough()) // excerpt is 12-34
	mustAdvance(t, session)

	cases := map[string]review.AnchorTarget{
		"before the excerpt": span(0, 5, 15),
		"after the excerpt":  span(0, 30, 40),
		"wrong side":         sideSpan(0, review.AnchorEndpoint{Side: review.OldSide, Line: 20}, review.AnchorEndpoint{Side: review.OldSide, Line: 22}),
	}
	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := session.Anchor(target)

			assertRejected(t, err, review.RejectedBadSelection)
		})
	}
}

func TestAnchorEndsAreRowsNotBounds(t *testing.T) {
	// The Reviewer can select upwards, so which end was reached first says nothing
	// about which line comes first.
	session := newSession()
	mustPost(t, session, validWalkthrough())
	mustAdvance(t, session)

	anchor, err := session.Anchor(span(0, 22, 20))

	if err != nil {
		t.Fatalf("expected an upward selection to anchor, got %v", err)
	}
	want := review.AnchorSegment{Side: review.NewSide, FirstLine: 20, LastLine: 22}
	if len(anchor.Segments) != 1 || anchor.Segments[0] != want {
		t.Errorf("expected the run to be ordered %+v, got %+v", want, anchor.Segments)
	}
}

func TestAnchorReportsWhenTheCodeCannotBeRead(t *testing.T) {
	session := review.NewSession(failingResolver{}, emptyDeriver{})
	walkthrough := validWalkthrough()
	walkthrough.Steps[0].Excerpts[0].Side = review.OldSide // post skips old-side resolution
	mustPost(t, session, walkthrough)
	mustAdvance(t, session)

	_, err := session.Anchor(span(0, 20, 22))

	if err == nil {
		t.Fatal("expected anchoring to fail when the code cannot be read")
	}
}

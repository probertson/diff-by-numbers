package review_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// sideResolver returns content tagged with the side it was read from, so a test
// can tell an injected before-line from an after-line.
type sideResolver struct{}

func (sideResolver) Resolve(e review.Excerpt) ([]review.Line, error) {
	side := "after"
	if e.Side == review.OldSide {
		side = "before"
	}
	var lines []review.Line
	for n := e.FirstLine; n <= e.LastLine; n++ {
		lines = append(lines, review.Line{Number: n, Text: fmt.Sprintf("%s %s:%d", side, e.File, n)})
	}
	return lines, nil
}

func TestANewSideExcerptRendersBeforeThenAfterInterleaved(t *testing.T) {
	// old line 2 became new lines 2-3; lines 1 and 4 are unchanged reference.
	deriver := pairingDeriver{
		lines: []review.ChangedLine{
			{File: "src/fetch.ts", Side: review.OldSide, Line: 2},
			{File: "src/fetch.ts", Side: review.NewSide, Line: 2},
			{File: "src/fetch.ts", Side: review.NewSide, Line: 3},
		},
		correspondences: []review.Correspondence{
			{File: "src/fetch.ts", OldFirst: 2, OldLast: 2, NewFirst: 2, NewLast: 3},
		},
	}
	session := review.NewSession(sideResolver{}, deriver)
	mustPost(t, session, beforeAfterWalkthrough(1, 4))
	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}

	lines := session.View().Step.Excerpts[0].Lines

	type row struct {
		side    review.Side
		changed bool
	}
	got := make([]row, len(lines))
	for i, l := range lines {
		got[i] = row{l.Side, l.Changed}
	}
	want := []row{
		{review.NewSide, false}, // reference line 1
		{review.OldSide, true},  // the before it replaced
		{review.NewSide, true},  // after line 2
		{review.NewSide, true},  // after line 3
		{review.NewSide, false}, // reference line 4
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d unified rows, got %d: %+v", len(want), len(got), lines)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d: expected %+v, got %+v (text %q)", i, want[i], got[i], lines[i].Text)
		}
	}
}

// pairingDeriver states both the Changed Lines and the cross-side correspondence
// git found, so a test can exercise point-once ridealong without a filesystem.
type pairingDeriver struct {
	lines           []review.ChangedLine
	correspondences []review.Correspondence
}

func (d pairingDeriver) Derive(repo review.Repository) (review.Derivation, error) {
	lines := make([]review.ChangedLine, len(d.lines))
	for i, l := range d.lines {
		l.Repository = repo.Root
		lines[i] = l
	}
	corrs := make([]review.Correspondence, len(d.correspondences))
	for i, c := range d.correspondences {
		c.Repository = repo.Root
		corrs[i] = c
	}
	return review.Derivation{Lines: lines, Correspondences: corrs}, nil
}

// beforeAfterWalkthrough is a one-Step Walkthrough over a single new-side Excerpt
// on src/fetch.ts, so a test can point at the after-side and see whether the
// before-side is accounted for.
func beforeAfterWalkthrough(first, last int) review.Walkthrough {
	w := validWalkthrough()
	w.Steps = []review.Step{{
		Name:        "The edit",
		Explanation: "reworked the fetch",
		Excerpts: []review.Excerpt{
			{Repository: "/repos/argus-portal", File: "src/fetch.ts", Side: review.NewSide, FirstLine: first, LastLine: last},
		},
	}}
	return w
}

func TestPointingAtTheAfterSideAccountsForTheBeforeItReplaced(t *testing.T) {
	// git found a modification: old lines 20-22 were replaced by new lines 20-24.
	deriver := pairingDeriver{
		lines: []review.ChangedLine{
			{File: "src/fetch.ts", Side: review.OldSide, Line: 20},
			{File: "src/fetch.ts", Side: review.OldSide, Line: 21},
			{File: "src/fetch.ts", Side: review.OldSide, Line: 22},
			{File: "src/fetch.ts", Side: review.NewSide, Line: 20},
			{File: "src/fetch.ts", Side: review.NewSide, Line: 21},
			{File: "src/fetch.ts", Side: review.NewSide, Line: 22},
			{File: "src/fetch.ts", Side: review.NewSide, Line: 23},
			{File: "src/fetch.ts", Side: review.NewSide, Line: 24},
		},
		correspondences: []review.Correspondence{
			{File: "src/fetch.ts", OldFirst: 20, OldLast: 22, NewFirst: 20, NewLast: 24},
		},
	}
	session := review.NewSession(stubResolver{}, deriver)

	// The agent points once, at the after-side only.
	err := session.Post(beforeAfterWalkthrough(20, 24))

	if err != nil {
		t.Fatalf("expected pointing at the after-side to account for the before it replaced, got %v", err)
	}
}

func TestADeletionShownAsAnOldSideExcerptIsAccepted(t *testing.T) {
	// A pure deletion, placed deliberately in a Step as an old-side Excerpt — the
	// agent's choice to show a removal rather than acknowledge it.
	deriver := pairingDeriver{
		lines: []review.ChangedLine{
			{File: "src/fetch.ts", Side: review.OldSide, Line: 40},
			{File: "src/fetch.ts", Side: review.OldSide, Line: 41},
		},
		correspondences: []review.Correspondence{
			{File: "src/fetch.ts", OldFirst: 40, OldLast: 41},
		},
	}
	session := review.NewSession(sideResolver{}, deriver)
	w := validWalkthrough()
	w.Steps = []review.Step{{
		Name:        "Drop the dead retry path",
		Explanation: "the legacy retry is gone",
		Excerpts: []review.Excerpt{
			{Repository: "/repos/argus-portal", File: "src/fetch.ts", Side: review.OldSide, FirstLine: 40, LastLine: 41},
		},
	}}

	if err := session.Post(w); err != nil {
		t.Fatalf("expected an old-side deletion Step to be accepted, got %v", err)
	}
	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}

	lines := session.View().Step.Excerpts[0].Lines
	if len(lines) != 2 {
		t.Fatalf("expected the two removed lines to render, got %d", len(lines))
	}
	for _, l := range lines {
		if l.Side != review.OldSide || !l.Changed {
			t.Errorf("expected a removed before-side line, got %+v", l)
		}
	}
}

func TestAStandaloneRemovalIsNotRiddenAlongAndMustBePlaced(t *testing.T) {
	// A pure deletion: old lines 40-42 removed, nothing added there. It has no
	// after-side to point at, so showing an unrelated edit does not cover it.
	deriver := pairingDeriver{
		lines: []review.ChangedLine{
			{File: "src/fetch.ts", Side: review.NewSide, Line: 20},
			{File: "src/fetch.ts", Side: review.OldSide, Line: 40},
			{File: "src/fetch.ts", Side: review.OldSide, Line: 41},
			{File: "src/fetch.ts", Side: review.OldSide, Line: 42},
		},
		correspondences: []review.Correspondence{
			{File: "src/fetch.ts", NewFirst: 20, NewLast: 20},
			{File: "src/fetch.ts", OldFirst: 40, OldLast: 42},
		},
	}
	session := review.NewSession(stubResolver{}, deriver)

	err := session.Post(beforeAfterWalkthrough(20, 20))

	assertRejected(t, err, review.RejectedUncoveredChanges)
	assertDetailContains(t, err, "40")
}

func TestAnchoringABeforeSideRowReadsTheBeforeNotTheAfter(t *testing.T) {
	// old line 2 became new lines 2-3; a before-side row and an after-side row both
	// carry the number 2, so the Anchor must read the side it was asked for.
	deriver := pairingDeriver{
		lines: []review.ChangedLine{
			{File: "src/fetch.ts", Side: review.OldSide, Line: 2},
			{File: "src/fetch.ts", Side: review.NewSide, Line: 2},
			{File: "src/fetch.ts", Side: review.NewSide, Line: 3},
		},
		correspondences: []review.Correspondence{
			{File: "src/fetch.ts", OldFirst: 2, OldLast: 2, NewFirst: 2, NewLast: 3},
		},
	}
	session := review.NewSession(sideResolver{}, deriver)
	mustPost(t, session, beforeAfterWalkthrough(1, 4))
	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}

	before, err := session.Anchor(review.AnchorTarget{ExcerptIndex: 0, FirstLine: 2, LastLine: 2, Side: review.OldSide})
	if err != nil {
		t.Fatalf("expected to anchor the before-side row, got %v", err)
	}
	if before.Side != review.OldSide {
		t.Errorf("expected the Anchor to record the before-side, got %q", before.Side)
	}
	if len(before.Lines) != 1 || !strings.Contains(before.Lines[0].Text, "before") {
		t.Errorf("expected the before-side content, got %+v", before.Lines)
	}

	after, err := session.Anchor(review.AnchorTarget{ExcerptIndex: 0, FirstLine: 2, LastLine: 2, Side: review.NewSide})
	if err != nil {
		t.Fatalf("expected to anchor the after-side row, got %v", err)
	}
	if len(after.Lines) != 1 || !strings.Contains(after.Lines[0].Text, "after") {
		t.Errorf("expected the after-side content for the same line number, got %+v", after.Lines)
	}
}

// recordingResolver notes whether it was handed a Change Set, to prove a rejected
// Post never repoints a resolver away from the Walkthrough still on screen.
type recordingResolver struct {
	stubResolver
	uses int
}

func (r *recordingResolver) UseChangeSet(review.ChangeSet) { r.uses++ }

func TestARejectedPostDoesNotRepointTheResolver(t *testing.T) {
	resolver := &recordingResolver{}
	// A changed line at 100 sits outside validWalkthrough's only Excerpt (12-34),
	// so the Post is rejected for uncovered changes.
	lines := append(changed("src/fetch.ts", 20, 22), review.ChangedLine{File: "src/fetch.ts", Side: review.NewSide, Line: 100})
	session := review.NewSession(resolver, fixedDeriver{lines: lines})

	err := session.Post(validWalkthrough())

	assertRejected(t, err, review.RejectedUncoveredChanges)
	if resolver.uses != 0 {
		t.Errorf("a rejected Post must not hand the resolver new ranges, but it was called %d time(s)", resolver.uses)
	}
}

func TestAnAcceptedPostRepointsTheResolver(t *testing.T) {
	resolver := &recordingResolver{}
	session := review.NewSession(resolver, fixedDeriver{lines: changed("src/fetch.ts", 20, 25)})

	if err := session.Post(validWalkthrough()); err != nil {
		t.Fatalf("expected the Walkthrough to be accepted, got %v", err)
	}

	if resolver.uses != 1 {
		t.Errorf("an accepted Post should hand the resolver its ranges exactly once, got %d", resolver.uses)
	}
}

func TestABudgetCountsTheBeforeLinesThatRideAlong(t *testing.T) {
	// A modification of 20 old lines replaced by 20 new lines. Pointing at the
	// after-side alone renders 40 changed rows, over the budget of 30, so an
	// unjustified Step is rejected as oversized.
	var lines []review.ChangedLine
	for n := 1; n <= 20; n++ {
		lines = append(lines,
			review.ChangedLine{File: "src/fetch.ts", Side: review.OldSide, Line: n},
			review.ChangedLine{File: "src/fetch.ts", Side: review.NewSide, Line: n},
		)
	}
	deriver := pairingDeriver{
		lines: lines,
		correspondences: []review.Correspondence{
			{File: "src/fetch.ts", OldFirst: 1, OldLast: 20, NewFirst: 1, NewLast: 20},
		},
	}
	session := review.NewSession(stubResolver{}, deriver)

	err := session.Post(beforeAfterWalkthrough(1, 20))

	assertRejected(t, err, review.RejectedOversizedStep)
}

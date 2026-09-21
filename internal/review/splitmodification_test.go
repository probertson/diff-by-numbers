package review_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// A genuine rewrite arrives from git as one modification: an old range replaced
// by a new range, with no finer pairing inside it. When the Authoring Agent
// splits the new side across Steps by idea, dbn cannot work out which old lines
// belong to which Step and must not guess — the agent says so with old-side
// Excerpts.

// sixLineRewrite is old lines 1-6 replaced by new lines 1-6 of src/fetch.ts, as
// one modification.
func sixLineRewrite() pairingDeriver {
	var lines []review.ChangedLine
	for n := 1; n <= 6; n++ {
		lines = append(lines,
			review.ChangedLine{File: testFile, Side: review.OldSide, Line: n},
			review.ChangedLine{File: testFile, Side: review.NewSide, Line: n})
	}
	return pairingDeriver{
		lines: lines,
		correspondences: []review.Correspondence{
			{File: testFile, OldFirst: 1, OldLast: 6, NewFirst: 1, NewLast: 6},
		},
	}
}

// renderedRows flattens everything a Step draws into one list of "side N" rows,
// in the order the Reviewer meets them. A pointer row is rendered as "→ text",
// since what matters about it is that it is a signpost and not content.
func renderedRows(t *testing.T, session *review.Session, step int) []string {
	t.Helper()
	if err := session.GoTo(step); err != nil {
		t.Fatalf("could not go to Step %d: %v", step, err)
	}
	view := session.View()
	if view.Step == nil {
		t.Fatalf("Step %d has no view", step)
	}
	var rows []string
	for _, excerpt := range view.Step.Excerpts {
		if excerpt.Problem != "" {
			t.Fatalf("Step %d could not render: %s", step, excerpt.Problem)
		}
		for _, line := range excerpt.Lines {
			if line.Signpost {
				rows = append(rows, "→ "+line.Text)
				continue
			}
			rows = append(rows, fmt.Sprintf("%s %d", line.Side, line.Number))
		}
	}
	return rows
}

func assertRenderedRows(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Errorf("expected rows\n  %s\ngot\n  %s", strings.Join(want, " | "), strings.Join(got, " | "))
	}
}

func TestEachStepOfASplitRewriteShowsTheOldLinesItClaims(t *testing.T) {
	session := review.NewSession(sideResolver{}, sixLineRewrite())

	err := session.Post(walkthroughShowing(
		[]review.Excerpt{at(review.NewSide, 1, 3), at(review.OldSide, 1, 3)},
		[]review.Excerpt{at(review.NewSide, 4, 6), at(review.OldSide, 4, 6)},
	))

	if err != nil {
		t.Fatalf("expected the split rewrite to be accepted, got %v", err)
	}
	assertRenderedRows(t, renderedRows(t, session, 1), []string{"old 1", "old 2", "old 3", "new 1", "new 2", "new 3"})
	assertRenderedRows(t, renderedRows(t, session, 2), []string{"old 4", "old 5", "old 6", "new 4", "new 5", "new 6"})
}

func TestWithNoOldSideExcerptsTheWholeBeforeSideStaysWithTheFirstStep(t *testing.T) {
	// Unchanged behaviour where the agent says nothing, plus the signpost that
	// tells the second Step's reader where its before-side went.
	session := review.NewSession(sideResolver{}, sixLineRewrite())

	err := session.Post(walkthroughShowing(
		[]review.Excerpt{at(review.NewSide, 1, 3)},
		[]review.Excerpt{at(review.NewSide, 4, 6)},
	))

	if err != nil {
		t.Fatalf("expected the split rewrite to be accepted, got %v", err)
	}
	assertRenderedRows(t, renderedRows(t, session, 1), []string{
		"old 1", "old 2", "old 3", "old 4", "old 5", "old 6", "new 1", "new 2", "new 3",
	})
	assertRenderedRows(t, renderedRows(t, session, 2), []string{
		"→ ⋯ replaces old 1-6, shown in Step 1", "new 4", "new 5", "new 6",
	})
}

func TestAPartlyClaimedBeforeSideLeavesTheRemainderWithTheFirstStep(t *testing.T) {
	session := review.NewSession(sideResolver{}, sixLineRewrite())

	err := session.Post(walkthroughShowing(
		[]review.Excerpt{at(review.NewSide, 1, 3)},
		[]review.Excerpt{at(review.NewSide, 4, 6), at(review.OldSide, 5, 6)},
	))

	if err != nil {
		t.Fatalf("expected the partly claimed rewrite to be accepted, got %v", err)
	}
	assertRenderedRows(t, renderedRows(t, session, 1), []string{
		"old 1", "old 2", "old 3", "old 4", "new 1", "new 2", "new 3",
	})
	assertRenderedRows(t, renderedRows(t, session, 2), []string{"old 5", "old 6", "new 4", "new 5", "new 6"})
}

func TestAnOldLineClaimedByTwoStepsIsShownInBoth(t *testing.T) {
	session := review.NewSession(sideResolver{}, sixLineRewrite())

	err := session.Post(walkthroughShowing(
		[]review.Excerpt{at(review.NewSide, 1, 3), at(review.OldSide, 1, 6)},
		[]review.Excerpt{at(review.NewSide, 4, 6), at(review.OldSide, 4, 6)},
	))

	if err != nil {
		t.Fatalf("expected the doubly claimed rewrite to be accepted, got %v", err)
	}
	assertRenderedRows(t, renderedRows(t, session, 1), []string{
		"old 1", "old 2", "old 3", "old 4", "old 5", "old 6", "new 1", "new 2", "new 3",
	})
	assertRenderedRows(t, renderedRows(t, session, 2), []string{"old 4", "old 5", "old 6", "new 4", "new 5", "new 6"})
}

func TestAnOldSideExcerptSpanningAnEditAndADeletionRendersBoth(t *testing.T) {
	// One Excerpt, two jobs: lines 1-3 say which part of the rewrite this Step
	// replaced, and lines 5-6 are a deletion nothing replaced. The first is drawn
	// beside its replacement, the second stands on its own, and neither appears
	// twice.
	deriver := pairingDeriver{
		lines: []review.ChangedLine{
			{File: testFile, Side: review.OldSide, Line: 1},
			{File: testFile, Side: review.OldSide, Line: 2},
			{File: testFile, Side: review.OldSide, Line: 3},
			{File: testFile, Side: review.OldSide, Line: 5},
			{File: testFile, Side: review.OldSide, Line: 6},
			{File: testFile, Side: review.NewSide, Line: 1},
			{File: testFile, Side: review.NewSide, Line: 2},
			{File: testFile, Side: review.NewSide, Line: 3},
		},
		correspondences: []review.Correspondence{
			{File: testFile, OldFirst: 1, OldLast: 3, NewFirst: 1, NewLast: 3},
			{File: testFile, OldFirst: 5, OldLast: 6},
		},
	}
	session := review.NewSession(sideResolver{}, deriver)

	err := session.Post(walkthroughShowing([]review.Excerpt{at(review.NewSide, 1, 3), at(review.OldSide, 1, 6)}))

	if err != nil {
		t.Fatalf("expected the mixed Excerpt to be accepted, got %v", err)
	}
	assertRenderedRows(t, renderedRows(t, session, 1), []string{
		"old 1", "old 2", "old 3", "new 1", "new 2", "new 3", // the rewrite, interleaved
		"old 4", "old 5", "old 6", // the deletion, standalone, with its reference line
	})
}

func TestASplitRewriteLeavesNoCoverageHole(t *testing.T) {
	session := review.NewSession(sideResolver{}, sixLineRewrite())
	if err := session.Post(walkthroughShowing(
		[]review.Excerpt{at(review.NewSide, 1, 3)},
		[]review.Excerpt{at(review.NewSide, 4, 6), at(review.OldSide, 5, 6)},
	)); err != nil {
		t.Fatalf("expected the split rewrite to be accepted, got %v", err)
	}

	if err := session.GoTo(2); err != nil {
		t.Fatal(err)
	}
	coverage := session.View().Coverage

	if coverage.Total != 12 {
		t.Errorf("expected all 12 atoms of the rewrite, got %d", coverage.Total)
	}
	if coverage.Seen != coverage.Total {
		t.Errorf("expected every atom seen by the last Step, got %d of %d", coverage.Seen, coverage.Total)
	}
}

func TestAnOldLineClaimedTwiceCostsBothStepsTheirBudget(t *testing.T) {
	// A doubly claimed before-side is drawn in both Steps, so both pay for it.
	// Twenty-five old lines each, plus ten new, puts each Step over on its own.
	var lines []review.ChangedLine
	for n := 1; n <= 25; n++ {
		lines = append(lines, review.ChangedLine{File: testFile, Side: review.OldSide, Line: n})
	}
	for n := 1; n <= 20; n++ {
		lines = append(lines, review.ChangedLine{File: testFile, Side: review.NewSide, Line: n})
	}
	session := review.NewSession(sideResolver{}, pairingDeriver{
		lines: lines,
		correspondences: []review.Correspondence{
			{File: testFile, OldFirst: 1, OldLast: 25, NewFirst: 1, NewLast: 20},
		},
	})

	err := session.Post(walkthroughShowing(
		[]review.Excerpt{at(review.NewSide, 1, 10), at(review.OldSide, 1, 25)},
		[]review.Excerpt{at(review.NewSide, 11, 20), at(review.OldSide, 1, 25)},
	))

	assertRejected(t, err, review.RejectedOversizedStep)
	for _, want := range []string{"Step 1", "Step 2", "35"} {
		assertDetailContains(t, err, want)
	}
}

func TestASelectionSpanningAnAssignedBeforeSideCarriesTheRightRows(t *testing.T) {
	session := review.NewSession(sideResolver{}, sixLineRewrite())
	if err := session.Post(walkthroughShowing(
		[]review.Excerpt{at(review.NewSide, 1, 3)},
		[]review.Excerpt{at(review.NewSide, 4, 6), at(review.OldSide, 5, 6)},
	)); err != nil {
		t.Fatalf("expected the split rewrite to be accepted, got %v", err)
	}
	if err := session.GoTo(2); err != nil {
		t.Fatal(err)
	}

	// Step 2 draws old 5, old 6, new 4, new 5, new 6.
	anchor, err := session.Anchor(review.AnchorTarget{
		Start: review.AnchorEndpoint{Side: review.OldSide, Line: 6},
		End:   review.AnchorEndpoint{Side: review.NewSide, Line: 5},
	})

	if err != nil {
		t.Fatalf("expected the selection to anchor, got %v", err)
	}
	want := []review.AnchorSegment{
		{Side: review.OldSide, FirstLine: 6, LastLine: 6},
		{Side: review.NewSide, FirstLine: 4, LastLine: 5},
	}
	if fmt.Sprint(anchor.Segments) != fmt.Sprint(want) {
		t.Errorf("expected segments %v, got %v", want, anchor.Segments)
	}
}

func TestTheSignpostRowCannotBeSelected(t *testing.T) {
	session := review.NewSession(sideResolver{}, sixLineRewrite())
	if err := session.Post(walkthroughShowing(
		[]review.Excerpt{at(review.NewSide, 1, 3)},
		[]review.Excerpt{at(review.NewSide, 4, 6)},
	)); err != nil {
		t.Fatalf("expected the split rewrite to be accepted, got %v", err)
	}
	if err := session.GoTo(2); err != nil {
		t.Fatal(err)
	}

	// Step 2's only old-side row is the signpost standing at old line 1.
	_, err := session.Anchor(review.AnchorTarget{
		Start: review.AnchorEndpoint{Side: review.OldSide, Line: 1},
		End:   review.AnchorEndpoint{Side: review.NewSide, Line: 4},
	})

	assertRejected(t, err, review.RejectedBadSelection)
}

func TestAStepShowingOneRewriteInTwoExcerptsDrawsItsBeforeSideOnce(t *testing.T) {
	// Both Excerpts show part of the same rewrite. The before-side belongs to the
	// Step, not to each range in it, so drawing it per Excerpt would show the
	// same removal twice and put more on screen than the budget was told about.
	session := review.NewSession(sideResolver{}, sixLineRewrite())

	err := session.Post(walkthroughShowing(
		[]review.Excerpt{at(review.NewSide, 1, 3), at(review.NewSide, 4, 6)},
	))

	if err != nil {
		t.Fatalf("expected the two-Excerpt Step to be accepted, got %v", err)
	}
	assertRenderedRows(t, renderedRows(t, session, 1), []string{
		"old 1", "old 2", "old 3", "old 4", "old 5", "old 6",
		"new 1", "new 2", "new 3", "new 4", "new 5", "new 6",
	})
}

func TestASignpostNamesEveryStepHoldingPartOfTheBeforeSide(t *testing.T) {
	// The rewrite's before-side is split between two Steps, and the Step in the
	// middle holds none of it. Naming only the first would send the Reviewer to
	// a Step that has half of what they are looking for.
	var lines []review.ChangedLine
	for n := 1; n <= 9; n++ {
		lines = append(lines,
			review.ChangedLine{File: testFile, Side: review.OldSide, Line: n},
			review.ChangedLine{File: testFile, Side: review.NewSide, Line: n})
	}
	session := review.NewSession(sideResolver{}, pairingDeriver{
		lines:           lines,
		correspondences: []review.Correspondence{{File: testFile, OldFirst: 1, OldLast: 9, NewFirst: 1, NewLast: 9}},
	})

	err := session.Post(walkthroughShowing(
		[]review.Excerpt{at(review.NewSide, 1, 3), at(review.OldSide, 1, 3)},
		[]review.Excerpt{at(review.NewSide, 4, 6)},
		[]review.Excerpt{at(review.NewSide, 7, 9), at(review.OldSide, 4, 9)},
	))

	if err != nil {
		t.Fatalf("expected the three-way split to be accepted, got %v", err)
	}
	assertRenderedRows(t, renderedRows(t, session, 2), []string{
		"→ ⋯ replaces old 1-9, shown in Steps 1 and 3", "new 4", "new 5", "new 6",
	})
}

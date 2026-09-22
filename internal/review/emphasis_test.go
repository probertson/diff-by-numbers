package review_test

import (
	"fmt"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// Within an edit, a removed line and the added line matched with it carry the
// ranges of runes that changed, so the Reviewer is pointed at a one-word edit
// (#81).

func emphasisOf(lines []review.Line) []string {
	var out []string
	for _, line := range lines {
		out = append(out, fmt.Sprintf("%s%d:%v", line.Side, line.Number, line.Emphasis))
	}
	return out
}

func TestARemovedLineAndTheLineInItsPlaceCarryWhatChanged(t *testing.T) {
	// Old line 2 became new lines 2-3; "before …:2" matches "after …:2".
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
	mustPost(t, session, beforeAfterRound(1, 4))
	mustAdvance(t, session)

	got := emphasisOf(session.View().Step.Excerpts[0].Lines)

	want := []string{"new1:[]", "old2:[{0 6}]", "new2:[{0 5}]", "new3:[]", "new4:[]"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("expected only the matched lines' first words emphasised:\nwant %v\ngot  %v", want, got)
	}
}

func TestSinceThePreviousRoundAnEditedLineCarriesWhatChanged(t *testing.T) {
	deriver := &roundsDeriver{
		fixedDeriver: fixedDeriver{lines: changedApp(1, 2)},
		touched:      map[string]bool{"app.ts:2": true},
		edits:        map[string][]review.RoundEdit{"app.ts": {{OldFirst: 2, OldCount: 1, NewFirst: 2, NewCount: 1}}},
	}
	resolver := roundTextResolver{first: []string{"call()", "retry(transport, 3)"}, second: []string{"call()", "retry(transport, 5)"}}
	session := review.NewSession(resolver, deriver)
	mustPost(t, session, appRound([]review.Step{appStep(1, 2)}, nil))
	handOffWithAComment(t, session)
	mustPost(t, session, revising(appRound([]review.Step{appStep(1, 2)}, nil)))
	mustAdvance(t, session)

	got := emphasisOf(session.View().Step.Excerpts[0].Lines)

	want := []string{"new1:[]", "previous2:[{17 18}]", "new2:[{17 18}]"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("expected the number emphasised on both rows:\nwant %v\ngot  %v", want, got)
	}
}

func TestNothingIsMatchedAcrossTwoEdits(t *testing.T) {
	// Two adjacent edits: old 2 became new 2, and old 3 was removed outright
	// with new 3 added in a separate edit. "before …:3" would match "after …:3"
	// on its words, but they belong to different edits.
	deriver := pairingDeriver{
		lines: []review.ChangedLine{
			{File: "src/fetch.ts", Side: review.OldSide, Line: 2},
			{File: "src/fetch.ts", Side: review.NewSide, Line: 2},
			{File: "src/fetch.ts", Side: review.OldSide, Line: 3},
			{File: "src/fetch.ts", Side: review.NewSide, Line: 3},
		},
		correspondences: []review.Correspondence{
			{File: "src/fetch.ts", OldFirst: 2, OldLast: 2, NewFirst: 2, NewLast: 2},
			{File: "src/fetch.ts", OldFirst: 3, OldLast: 3},
			{File: "src/fetch.ts", NewFirst: 3, NewLast: 3},
		},
	}
	session := review.NewSession(sideResolver{}, deriver)
	w := beforeAfterRound(1, 4)
	w.Steps[0].Excerpts = append(w.Steps[0].Excerpts,
		review.Excerpt{Repository: "/repos/argus-portal", File: "src/fetch.ts", Side: review.OldSide, FirstLine: 3, LastLine: 3})
	mustPost(t, session, w)
	mustAdvance(t, session)

	step := session.View().Step
	got := append(emphasisOf(step.Excerpts[0].Lines), emphasisOf(step.Excerpts[1].Lines)...)

	want := []string{"new1:[]", "old2:[{0 6}]", "new2:[{0 5}]", "new3:[]", "new4:[]", "old3:[]"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("expected only the one edit's lines matched:\nwant %v\ngot  %v", want, got)
	}
}

func TestAnEditShownInTwoRangesIsMatchedWhole(t *testing.T) {
	// One edit replaced old 2-3 with new 2-3, and the Step shows it through two
	// ranges. The removed lines are drawn above the first range, but each new
	// line still carries what changed against the line it took the place of.
	deriver := pairingDeriver{
		lines: []review.ChangedLine{
			{File: "src/fetch.ts", Side: review.OldSide, Line: 2},
			{File: "src/fetch.ts", Side: review.OldSide, Line: 3},
			{File: "src/fetch.ts", Side: review.NewSide, Line: 2},
			{File: "src/fetch.ts", Side: review.NewSide, Line: 3},
		},
		correspondences: []review.Correspondence{
			{File: "src/fetch.ts", OldFirst: 2, OldLast: 3, NewFirst: 2, NewLast: 3},
		},
	}
	session := review.NewSession(sideResolver{}, deriver)
	w := beforeAfterRound(1, 2)
	w.Steps[0].Excerpts = append(w.Steps[0].Excerpts,
		review.Excerpt{Repository: "/repos/argus-portal", File: "src/fetch.ts", Side: review.NewSide, FirstLine: 3, LastLine: 4})
	mustPost(t, session, w)
	mustAdvance(t, session)

	second := session.View().Step.Excerpts[1].Lines

	if len(second) == 0 || second[0].Number != 3 || fmt.Sprint(second[0].Emphasis) != "[{0 5}]" {
		t.Errorf("expected new line 3 in the second range emphasised against old line 3, got %+v", second)
	}
}

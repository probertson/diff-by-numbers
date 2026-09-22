package review_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// A Revision Round is shaded, by default, by what moved since the previous round
// rather than since the merge-base (#44): an all-new file the agent did not
// touch again should not read as all-new every round.

// roundsDeriver snapshots to s1, s2 … in turn, and reports the same edits and
// touched lines between any two of them. A test sets what moved between rounds
// before posting the later one.
type roundsDeriver struct {
	fixedDeriver
	taken   int
	touched map[string]bool
	edits   map[string][]review.RoundEdit
}

func (d *roundsDeriver) Derive(repo review.Repository) (review.Derivation, error) {
	derivation, err := d.fixedDeriver.Derive(repo)
	derivation.Base = "base"
	return derivation, err
}

func (d *roundsDeriver) Snapshot(string) (string, error) {
	d.taken++
	return fmt.Sprintf("s%d", d.taken), nil
}

func (d *roundsDeriver) MapBetween(_, _, _ string) (review.RoundMapping, error) {
	return fakeMapping{touched: d.touched, edits: d.edits}, nil
}

// secondRound posts app.ts 1-6 as a first round, hands it off, and posts it
// again as a Revision Round with whatever the deriver has been told moved.
func secondRound(t *testing.T, deriver *roundsDeriver, resolver review.Resolver) *review.Session {
	t.Helper()
	session := review.NewSession(resolver, deriver)
	mustPost(t, session, appRound([]review.Step{appStep(1, 6)}, nil))
	handOffWithAComment(t, session)
	mustRevise(t, session, revising(appRound([]review.Step{appStep(1, 6)}, nil)))
	return session
}

func TestARevisionRoundOpensShowingWhatMovedSinceThePreviousRound(t *testing.T) {
	session := secondRound(t, &roundsDeriver{fixedDeriver: fixedDeriver{lines: changedApp(1, 6)}}, &textResolver{text: map[string]string{}})

	view := session.View()

	if view.Round != 2 || view.PreviousRound != 1 || !view.SincePreviousRound {
		t.Errorf("expected round 2 shown since round 1, got round %d, previous %d, since %v", view.Round, view.PreviousRound, view.SincePreviousRound)
	}
}

func TestTheReviewerCanSwitchToAllChangesUnderReviewAndBack(t *testing.T) {
	session := secondRound(t, &roundsDeriver{fixedDeriver: fixedDeriver{lines: changedApp(1, 6)}}, &textResolver{text: map[string]string{}})

	if err := session.ToggleSincePreviousRound(); err != nil {
		t.Fatal(err)
	}
	all := session.View().SincePreviousRound
	if err := session.ToggleSincePreviousRound(); err != nil {
		t.Fatal(err)
	}
	since := session.View().SincePreviousRound

	if all || !since {
		t.Errorf("expected the toggle to show all changes, then only those since round 1; got %v then %v", all, since)
	}
}

func TestTheFirstRoundHasNoPreviousRoundToCompareWith(t *testing.T) {
	session := review.NewSession(&textResolver{text: map[string]string{}}, &roundsDeriver{fixedDeriver: fixedDeriver{lines: changedApp(1, 6)}})
	mustPost(t, session, appRound([]review.Step{appStep(1, 6)}, nil))

	err := session.ToggleSincePreviousRound()

	view := session.View()
	if view.Round != 1 || view.PreviousRound != 0 || view.SincePreviousRound {
		t.Errorf("expected round 1 with nothing to compare with, got %+v", view)
	}
	assertRejected(t, err, review.RejectedNoPreviousRound)
}

func TestAReplacementKeepsItsRoundNumber(t *testing.T) {
	session := secondRound(t, &roundsDeriver{fixedDeriver: fixedDeriver{lines: changedApp(1, 6)}}, &textResolver{text: map[string]string{}})

	if err := session.Replace(session.ReviewID(), revising(appRound([]review.Step{appStep(1, 6)}, nil))); err != nil {
		t.Fatal(err)
	}

	if view := session.View(); view.Round != 2 || view.PreviousRound != 1 {
		t.Errorf("expected the replacement still round 2, compared with round 1, got %d and %d", view.Round, view.PreviousRound)
	}
}

// roundTextResolver reads each round's app.ts from the snapshot it names:
// "s1" is the first round's text, anything else the second's.
type roundTextResolver struct{ first, second []string }

func (r roundTextResolver) Resolve(e review.Excerpt, round review.RoundSource) ([]review.Line, error) {
	text := r.second
	if round.Snapshots[e.Repository] == "s1" {
		text = r.first
	}
	if e.Side == review.OldSide {
		return nil, fmt.Errorf("no merge-base in this test")
	}
	var lines []review.Line
	for n := e.FirstLine; n <= e.LastLine; n++ {
		lines = append(lines, review.Line{Number: n, Text: text[n-1]})
	}
	return lines, nil
}

// row is one rendered row, reduced to what the shading is about.
type row struct {
	side    review.Side
	number  int
	text    string
	changed bool
}

func shadedRows(lines []review.Line) []row {
	var out []row
	for _, line := range lines {
		out = append(out, row{line.Side, line.Number, line.Text, line.Changed})
	}
	return out
}

// Round 1's app.ts is a–f, all new. Round 2 rewrites c, withdraws e and adds g.
func editedSinceRoundOne(t *testing.T) *review.Session {
	t.Helper()
	deriver := &roundsDeriver{
		fixedDeriver: fixedDeriver{lines: changedApp(1, 6)},
		touched:      map[string]bool{"app.ts:3": true, "app.ts:6": true},
		edits: map[string][]review.RoundEdit{"app.ts": {
			{OldFirst: 3, OldCount: 1, NewFirst: 3, NewCount: 1},
			{OldFirst: 5, OldCount: 1, NewFirst: 4, NewCount: 0},
			{OldFirst: 6, OldCount: 0, NewFirst: 6, NewCount: 1},
		}},
	}
	resolver := roundTextResolver{
		first:  []string{"a", "b", "c", "d", "e", "f"},
		second: []string{"a", "b", "C", "d", "f", "g"},
	}
	session := secondRound(t, deriver, resolver)
	mustAdvance(t, session)
	return session
}

func TestSinceThePreviousRoundOnlyWhatMovedIsShaded(t *testing.T) {
	session := editedSinceRoundOne(t)

	got := shadedRows(session.View().Step.Excerpts[0].Lines)

	want := []row{
		{review.NewSide, 1, "a", false},
		{review.NewSide, 2, "b", false},
		{review.PreviousSide, 3, "c", true},
		{review.NewSide, 3, "C", true},
		{review.NewSide, 4, "d", false},
		{review.PreviousSide, 5, "e", true},
		{review.NewSide, 5, "f", false},
		{review.NewSide, 6, "g", true},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("unexpected rows:\nwant %v\ngot  %v", want, got)
	}
}

func TestAllChangesUnderReviewAreShadedAgainstTheMergeBase(t *testing.T) {
	session := editedSinceRoundOne(t)
	if err := session.ToggleSincePreviousRound(); err != nil {
		t.Fatal(err)
	}

	got := shadedRows(session.View().Step.Excerpts[0].Lines)

	for _, r := range got {
		if r.side != review.NewSide || !r.changed {
			t.Fatalf("expected every line of the all-new file shaded as new, got %v", got)
		}
	}
	if len(got) != 6 {
		t.Errorf("expected the six lines alone, got %v", got)
	}
}

func TestTheOverviewListsEveryLineWithdrawnSinceThePreviousRound(t *testing.T) {
	session := editedSinceRoundOne(t)

	withdrawn := session.View().Withdrawn

	want := []review.Withdrawal{{Repository: revRepo, File: "app.ts", After: 4, PreviousFirst: 5, Lines: []string{"e"}}}
	if fmt.Sprint(withdrawn) != fmt.Sprint(want) {
		t.Errorf("expected %+v, got %+v", want, withdrawn)
	}
}

func TestAFileWithdrawnEntirelyIsStillListed(t *testing.T) {
	// A file added in round 1 and removed in round 2 was never in the merge-base
	// and is not in the working tree, so no Changed Line will ever name it.
	deriver := &roundsDeriver{
		fixedDeriver: fixedDeriver{lines: changedApp(1, 6)},
		edits:        map[string][]review.RoundEdit{"scratch.ts": {{OldFirst: 1, OldCount: 2, NewFirst: 0, NewCount: 0}}},
	}
	// The fake reads every file alike, so round 1's text is long enough for the
	// app.ts it also posted; scratch.ts's two lines are its first two.
	resolver := roundTextResolver{first: []string{"x", "y", "c", "d", "e", "f"}, second: []string{"a", "b", "c", "d", "e", "f"}}
	session := secondRound(t, deriver, resolver)

	withdrawn := session.View().Withdrawn

	want := []review.Withdrawal{{Repository: revRepo, File: "scratch.ts", After: 0, PreviousFirst: 1, Lines: []string{"x", "y"}}}
	if fmt.Sprint(withdrawn) != fmt.Sprint(want) {
		t.Errorf("expected %+v, got %+v", want, withdrawn)
	}
}

func TestAStepWithNothingNewOrEditedSinceThePreviousRoundIsMarkedUnchanged(t *testing.T) {
	deriver := &roundsDeriver{
		fixedDeriver: fixedDeriver{lines: changedApp(1, 6)},
		touched:      map[string]bool{"app.ts:5": true},
		edits:        map[string][]review.RoundEdit{"app.ts": {{OldFirst: 5, OldCount: 1, NewFirst: 5, NewCount: 1}}},
	}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver)
	split := appRound([]review.Step{appStep(1, 3), appStep(4, 6)}, nil)
	mustPost(t, session, split)
	handOffWithAComment(t, session)
	mustRevise(t, session, revising(split))

	unchanged := session.View().UnchangedSincePrevious

	if fmt.Sprint(unchanged) != "[true false]" {
		t.Errorf("expected only the first Step unchanged since round 1, got %v", unchanged)
	}
}

func TestAFirstRoundHasNoWithdrawalsOrUnchangedMarkers(t *testing.T) {
	session := review.NewSession(&textResolver{text: map[string]string{}}, &roundsDeriver{fixedDeriver: fixedDeriver{lines: changedApp(1, 6)}})
	mustPost(t, session, appRound([]review.Step{appStep(1, 6)}, nil))

	view := session.View()

	if view.Withdrawn != nil || view.UnchangedSincePrevious != nil {
		t.Errorf("a first round has nothing to compare with, got %+v and %v", view.Withdrawn, view.UnchangedSincePrevious)
	}
}

func TestAnAnchorOverAWithdrawnRowQuotesItAsThePreviousRoundHadIt(t *testing.T) {
	session := editedSinceRoundOne(t)

	anchor, err := session.Anchor(review.AnchorTarget{
		ExcerptIndex: 0,
		Start:        review.AnchorEndpoint{Side: review.PreviousSide, Line: 5},
		End:          review.AnchorEndpoint{Side: review.NewSide, Line: 5},
	})

	if err != nil {
		t.Fatal(err)
	}
	rendered := anchor.Render()
	header, _, _ := strings.Cut(rendered, "\n")
	if !strings.Contains(header, "app.ts — round 1 5 — after 5") || !strings.Contains(header, "(includes lines removed since round 1)") {
		t.Errorf("expected the header to place and flag the withdrawn line, got %q", header)
	}
	if !strings.Contains(rendered, "- r1     5 | e\n") {
		t.Errorf("expected the withdrawn line quoted with its round, got:\n%s", rendered)
	}
}

// withdrawnBelow posts a Revision Round of app.ts in the given Steps, over the
// given Changed Lines, with line e of round 1 withdrawn from the gap below round
// 2's line 4.
func withdrawnBelow(t *testing.T, lines []review.ChangedLine, steps ...review.Step) *review.Session {
	t.Helper()
	deriver := &roundsDeriver{
		fixedDeriver: fixedDeriver{lines: lines},
		edits:        map[string][]review.RoundEdit{"app.ts": {{OldFirst: 5, OldCount: 1, NewFirst: 4, NewCount: 0}}},
	}
	resolver := roundTextResolver{first: []string{"a", "b", "c", "d", "e", "f"}, second: []string{"a", "b", "c", "d", "f", "g"}}
	session := review.NewSession(resolver, deriver)
	w := appRound(steps, nil)
	mustPost(t, session, w)
	handOffWithAComment(t, session)
	mustRevise(t, session, revising(w))
	return session
}

func previousRowsIn(v review.ExcerptView) []string {
	var out []string
	for _, line := range v.Lines {
		if line.Side == review.PreviousSide {
			out = append(out, line.Text)
		}
	}
	return out
}

func TestAWithdrawalJustBelowAnExcerptIsDrawnAtItsFoot(t *testing.T) {
	session := withdrawnBelow(t, changedApp(1, 6), appStep(1, 4), appStep(5, 6))
	mustAdvance(t, session)

	first := session.View().Step.Excerpts[0]
	mustAdvance(t, session)
	second := session.View().Step.Excerpts[0]

	if got := previousRowsIn(first); fmt.Sprint(got) != "[e]" || first.Lines[len(first.Lines)-1].Text != "e" {
		t.Errorf("expected e drawn below the first Step's last line, got %+v", first.Lines)
	}
	if got := previousRowsIn(second); len(got) != 0 {
		t.Errorf("the gap sits above the second Step's first line, which does not draw it; got %v", got)
	}
}

func TestAWithdrawalBetweenExcerptsIsListedButNotDrawn(t *testing.T) {
	// Line 4 is unchanged, so no Step needs to show it.
	session := withdrawnBelow(t, append(changedApp(1, 3), changedApp(5, 6)...), appStep(1, 3), appStep(5, 6))

	view := session.View()
	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}
	first := session.View().Step.Excerpts[0]
	if err := session.GoTo(2); err != nil {
		t.Fatal(err)
	}
	second := session.View().Step.Excerpts[0]

	if len(previousRowsIn(first))+len(previousRowsIn(second)) != 0 {
		t.Errorf("a gap no Excerpt shows the edge of is for the Overview alone, got %v and %v", first.Lines, second.Lines)
	}
	if len(view.Withdrawn) != 1 || view.Withdrawn[0].Lines[0] != "e" {
		t.Errorf("expected the withdrawal listed for the Overview, got %+v", view.Withdrawn)
	}
}

func TestAStepShowingAWithdrawalIsNotMarkedUnchanged(t *testing.T) {
	session := withdrawnBelow(t, changedApp(1, 6), appStep(1, 4), appStep(5, 6))

	unchanged := session.View().UnchangedSincePrevious

	if fmt.Sprint(unchanged) != "[false true]" {
		t.Errorf("expected the Step drawing the withdrawal to read as changed, got %v", unchanged)
	}
}

func TestAnEditIsDrawnOnceInAStepThatShowsItInTwoRanges(t *testing.T) {
	// c and d were rewritten as one edit; the Step shows 1-3 and 4-6, so both
	// of its ranges show part of the replacement.
	deriver := &roundsDeriver{
		fixedDeriver: fixedDeriver{lines: changedApp(1, 6)},
		touched:      map[string]bool{"app.ts:3": true, "app.ts:4": true},
		edits:        map[string][]review.RoundEdit{"app.ts": {{OldFirst: 3, OldCount: 2, NewFirst: 3, NewCount: 2}}},
	}
	resolver := roundTextResolver{first: []string{"a", "b", "c", "d", "e", "f"}, second: []string{"a", "b", "C", "D", "e", "f"}}
	session := review.NewSession(resolver, deriver)
	twoRanges := review.Step{Name: "Both", Explanation: "e", Excerpts: []review.Excerpt{
		{Repository: revRepo, File: "app.ts", Side: review.NewSide, FirstLine: 1, LastLine: 3},
		{Repository: revRepo, File: "app.ts", Side: review.NewSide, FirstLine: 4, LastLine: 6},
	}}
	w := appRound([]review.Step{twoRanges}, nil)
	mustPost(t, session, w)
	handOffWithAComment(t, session)
	mustRevise(t, session, revising(w))
	mustAdvance(t, session)

	step := session.View().Step

	if got := previousRowsIn(step.Excerpts[0]); fmt.Sprint(got) != "[c d]" {
		t.Errorf("expected the replaced lines above the first range, got %v", got)
	}
	if got := previousRowsIn(step.Excerpts[1]); len(got) != 0 {
		t.Errorf("expected them drawn once in the Step, not again in its second range; got %v", got)
	}
}

// oldSideResolver answers a before-side read too, which the deletion test needs.
type oldSideResolver struct{ roundTextResolver }

func (r oldSideResolver) Resolve(e review.Excerpt, round review.RoundSource) ([]review.Line, error) {
	if e.Side != review.OldSide {
		return r.roundTextResolver.Resolve(e, round)
	}
	var lines []review.Line
	for n := e.FirstLine; n <= e.LastLine; n++ {
		lines = append(lines, review.Line{Number: n, Text: fmt.Sprintf("gone %d", n)})
	}
	return lines, nil
}

func TestSinceThePreviousRoundADeletionItAlreadyHadIsPlain(t *testing.T) {
	// Merge-base line 7 was deleted in round 1 and still is; line 8 is deleted
	// only now.
	lines := append(changedApp(1, 6),
		review.ChangedLine{File: "app.ts", Side: review.OldSide, Line: 7})
	deriver := &roundsDeriver{fixedDeriver: fixedDeriver{lines: lines}}
	resolver := oldSideResolver{roundTextResolver{first: []string{"a", "b", "c", "d", "e", "f"}, second: []string{"a", "b", "c", "d", "e", "f"}}}
	session := review.NewSession(resolver, deriver)
	deletion := review.Step{Name: "Deletion", Explanation: "e", Excerpts: []review.Excerpt{
		{Repository: revRepo, File: "app.ts", Side: review.OldSide, FirstLine: 7, LastLine: 8},
	}}
	mustPost(t, session, appRound([]review.Step{appStep(1, 6), deletion}, nil))
	handOffWithAComment(t, session)
	deriver.lines = append(lines, review.ChangedLine{File: "app.ts", Side: review.OldSide, Line: 8})
	mustRevise(t, session, revising(appRound([]review.Step{appStep(1, 6), deletion}, nil)))
	if err := session.GoTo(2); err != nil {
		t.Fatal(err)
	}

	got := shadedRows(session.View().Step.Excerpts[0].Lines)

	if len(got) != 2 || got[0].changed || !got[1].changed {
		t.Errorf("expected line 7, deleted last round too, plain and line 8 shaded, got %v", got)
	}
}

// failingMapper snapshots, but cannot compare two rounds.
type failingMapper struct{ roundsDeriver }

func (d *failingMapper) MapBetween(_, _, _ string) (review.RoundMapping, error) {
	return nil, fmt.Errorf("no such tree")
}

func TestARoundThatCannotBeComparedOffersNoComparison(t *testing.T) {
	deriver := &failingMapper{roundsDeriver{fixedDeriver: fixedDeriver{lines: changedApp(1, 6)}}}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver)
	mustPost(t, session, appRound([]review.Step{appStep(1, 6)}, nil))
	handOffWithAComment(t, session)
	mustRevise(t, session, revising(appRound([]review.Step{appStep(1, 6)}, nil)))

	view := session.View()

	if view.Round != 2 || view.PreviousRound != 0 || view.SincePreviousRound {
		t.Errorf("expected round 2 with nothing it could be compared with, got %+v", view)
	}
}

// renameResolver serves round 1's old.ts, and fills anything else of round 1
// with "x", so a previous-round row read from the wrong name shows as one.
type renameResolver struct{}

func (renameResolver) Resolve(e review.Excerpt, round review.RoundSource) ([]review.Line, error) {
	text := []string{"a", "B", "c"}
	if round.Snapshots[e.Repository] == "s1" {
		text = []string{"x", "x", "x"}
		if e.File == "old.ts" {
			text = []string{"a", "b", "c"}
		}
	}
	var lines []review.Line
	for n := e.FirstLine; n <= e.LastLine; n++ {
		lines = append(lines, review.Line{Number: n, Text: text[n-1]})
	}
	return lines, nil
}

// renamingDeriver reports app.ts as old.ts renamed since round 1.
type renamingDeriver struct{ roundsDeriver }

func (d *renamingDeriver) MapBetween(_, _, _ string) (review.RoundMapping, error) {
	return fakeMapping{touched: d.touched, edits: d.edits, renamed: map[string]string{"app.ts": "old.ts"}}, nil
}

func TestALineRewrittenInAFileRenamedSinceThePreviousRoundIsReadFromItsOldName(t *testing.T) {
	deriver := &renamingDeriver{roundsDeriver{
		fixedDeriver: fixedDeriver{lines: changedApp(1, 3)},
		touched:      map[string]bool{"app.ts:2": true},
		edits:        map[string][]review.RoundEdit{"app.ts": {{OldFirst: 2, OldCount: 1, NewFirst: 2, NewCount: 1}}},
	}}
	session := review.NewSession(renameResolver{}, deriver)
	mustPost(t, session, appRound([]review.Step{appStep(1, 3)}, nil))
	handOffWithAComment(t, session)
	mustRevise(t, session, revising(appRound([]review.Step{appStep(1, 3)}, nil)))
	mustAdvance(t, session)

	got := previousRowsIn(session.View().Step.Excerpts[0])

	if fmt.Sprint(got) != "[b]" {
		t.Errorf("expected line 2 as round 1 had it, read from old.ts, got %v", got)
	}
}

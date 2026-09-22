package review_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// sideResolver returns content tagged with the side it was read from, so a test
// can tell an injected before-line from an after-line.
type sideResolver struct{}

func (sideResolver) Resolve(e review.Excerpt, _ review.RoundSource) ([]review.Line, error) {
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
	mustPost(t, session, beforeAfterRound(1, 4))
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

// beforeAfterRound is a one-Step Round over a single new-side Excerpt
// on src/fetch.ts, so a test can point at the after-side and see whether the
// before-side is accounted for.
func beforeAfterRound(first, last int) review.Round {
	w := validRound()
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
	err := session.Post(beforeAfterRound(20, 24))

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
	w := validRound()
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

	err := session.Post(beforeAfterRound(20, 20))

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
	mustPost(t, session, beforeAfterRound(1, 4))
	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}

	before, err := session.Anchor(sideSpan(0, oldRow(2), oldRow(2)))
	if err != nil {
		t.Fatalf("expected to anchor the before-side row, got %v", err)
	}
	if len(before.Segments) != 1 || before.Segments[0].Side != review.OldSide {
		t.Errorf("expected the Anchor to record the before-side, got %+v", before.Segments)
	}
	if len(before.Lines) != 1 || !strings.Contains(before.Lines[0].Text, "before") {
		t.Errorf("expected the before-side content, got %+v", before.Lines)
	}

	after, err := session.Anchor(sideSpan(0, newRow(2), newRow(2)))
	if err != nil {
		t.Fatalf("expected to anchor the after-side row, got %v", err)
	}
	if len(after.Lines) != 1 || !strings.Contains(after.Lines[0].Text, "after") {
		t.Errorf("expected the after-side content for the same line number, got %+v", after.Lines)
	}
}

func TestAnAnchorMaySpanARemovalAndItsReplacement(t *testing.T) {
	// The Reviewer's point is about the edit, not about one half of it: selecting
	// from the removed line through the lines that replaced it must anchor both (#57).
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
	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}

	anchor, err := session.Anchor(sideSpan(0, oldRow(2), newRow(3)))

	if err != nil {
		t.Fatalf("expected a selection crossing the sides to anchor, got %v", err)
	}
	want := []review.AnchorSegment{
		{Side: review.OldSide, FirstLine: 2, LastLine: 2},
		{Side: review.NewSide, FirstLine: 2, LastLine: 3},
	}
	if len(anchor.Segments) != len(want) {
		t.Fatalf("expected %d segments, got %+v", len(want), anchor.Segments)
	}
	for i := range want {
		if anchor.Segments[i] != want[i] {
			t.Errorf("segment %d: expected %+v, got %+v", i, want[i], anchor.Segments[i])
		}
	}
	if got := anchor.Location(); got != "src/fetch.ts — before 2 — after 2-3" {
		t.Errorf("expected the location to name both sides, got %q", got)
	}
	// Markers come per line, so the removal reads as a removal beside its replacement.
	rendered := anchor.Render()
	for _, want := range []string{"-     2 | before src/fetch.ts:2", "+     2 | after src/fetch.ts:2", "+     3 | after src/fetch.ts:3"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("expected the rendered Anchor to contain %q\n---\n%s", want, rendered)
		}
	}
}

// unreadableBeforeResolver reads the after-side but cannot reach the before-side,
// as a shallow checkout or a base commit missing the file would leave it.
type unreadableBeforeResolver struct{ sideResolver }

func (r unreadableBeforeResolver) Resolve(e review.Excerpt, round review.RoundSource) ([]review.Line, error) {
	if e.Side == review.OldSide {
		return nil, errors.New("no such blob")
	}
	return r.sideResolver.Resolve(e, round)
}

func TestAnAnchorRefusesToQuoteCodeThatCouldNotBeRead(t *testing.T) {
	// An unreadable before-side still draws a row saying so, because a line that
	// rode along must not vanish. Selecting over it must not turn that notice into
	// quoted source in the text an agent will act on.
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
	session := review.NewSession(unreadableBeforeResolver{}, deriver)
	mustPost(t, session, beforeAfterRound(1, 4))
	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}

	_, err := session.Anchor(sideSpan(0, oldRow(2), newRow(3)))

	if err == nil {
		t.Fatal("expected anchoring over an unreadable before-side to be refused")
	}
	if !strings.Contains(err.Error(), "could not read") {
		t.Errorf("expected the refusal to say the code could not be read, got %v", err)
	}
}

func TestAnAnchorSpanningTwoEditsKeepsEachRemovedRunApart(t *testing.T) {
	// Two edits in one Excerpt: old 2 became new 2, old 5 became new 5. A selection
	// running from the first removal to the second replacement covers two separate
	// before-side runs, which a single before-range could not express.
	deriver := pairingDeriver{
		lines: []review.ChangedLine{
			{File: "src/fetch.ts", Side: review.OldSide, Line: 2},
			{File: "src/fetch.ts", Side: review.NewSide, Line: 2},
			{File: "src/fetch.ts", Side: review.OldSide, Line: 5},
			{File: "src/fetch.ts", Side: review.NewSide, Line: 5},
		},
		correspondences: []review.Correspondence{
			{File: "src/fetch.ts", OldFirst: 2, OldLast: 2, NewFirst: 2, NewLast: 2},
			{File: "src/fetch.ts", OldFirst: 5, OldLast: 5, NewFirst: 5, NewLast: 5},
		},
	}
	session := review.NewSession(sideResolver{}, deriver)
	mustPost(t, session, beforeAfterRound(1, 6))
	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}

	anchor, err := session.Anchor(sideSpan(0, oldRow(2), newRow(5)))

	if err != nil {
		t.Fatalf("expected a selection across two edits to anchor, got %v", err)
	}
	want := []review.AnchorSegment{
		{Side: review.OldSide, FirstLine: 2, LastLine: 2},
		{Side: review.NewSide, FirstLine: 2, LastLine: 4},
		{Side: review.OldSide, FirstLine: 5, LastLine: 5},
		{Side: review.NewSide, FirstLine: 5, LastLine: 5},
	}
	if len(anchor.Segments) != len(want) {
		t.Fatalf("expected %d segments, got %+v", len(want), anchor.Segments)
	}
	for i := range want {
		if anchor.Segments[i] != want[i] {
			t.Errorf("segment %d: expected %+v, got %+v", i, want[i], anchor.Segments[i])
		}
	}
	// The two removed lines are not a range, so the location lists them apart while
	// the after-side rows either side of them read as the one run they are.
	if got := anchor.Location(); got != "src/fetch.ts — before 2, 5 — after 2-5" {
		t.Errorf("unexpected location: %q", got)
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

	err := session.Post(beforeAfterRound(1, 20))

	assertRejected(t, err, review.RejectedOversizedStep)
}

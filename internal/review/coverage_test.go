package review_test

import (
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// fixedDeriver returns the same Changed Lines for any repository, so a test can
// state exactly what git "found" without a filesystem.
type fixedDeriver struct{ lines []review.ChangedLine }

func (d fixedDeriver) Derive(repo review.Repository) ([]review.ChangedLine, error) {
	out := make([]review.ChangedLine, len(d.lines))
	for i, l := range d.lines {
		l.Repository = repo.Root
		out[i] = l
	}
	return out, nil
}

func changed(file string, first, last int) []review.ChangedLine {
	var lines []review.ChangedLine
	for n := first; n <= last; n++ {
		lines = append(lines, review.ChangedLine{File: file, Side: review.NewSide, Line: n})
	}
	return lines
}

func sessionDeriving(lines []review.ChangedLine) *review.Session {
	return review.NewSession(stubResolver{}, fixedDeriver{lines: lines})
}

func TestAPlanCoveringEveryChangedLineIsAccepted(t *testing.T) {
	// validWalkthrough's one Excerpt is src/fetch.ts:12-34 (new). Derive lines
	// wholly inside it.
	session := sessionDeriving(changed("src/fetch.ts", 20, 25))

	err := session.Post(validWalkthrough())

	if err != nil {
		t.Fatalf("expected the plan to be accepted, got %v", err)
	}
}

func TestAPlanLeavingChangedLinesUnshownIsRejectedNamingThem(t *testing.T) {
	// A changed line at 100 sits outside the only Excerpt (12-34).
	lines := append(changed("src/fetch.ts", 20, 22), review.ChangedLine{File: "src/fetch.ts", Side: review.NewSide, Line: 100})
	session := sessionDeriving(lines)

	err := session.Post(validWalkthrough())

	assertRejected(t, err, review.RejectedUncoveredChanges)
	assertDetailContains(t, err, "100")
}

func TestADeletedLineOnTheOldSideMustBeCoveredToo(t *testing.T) {
	// A deletion the new-side Excerpt cannot cover.
	session := sessionDeriving([]review.ChangedLine{{File: "src/fetch.ts", Side: review.OldSide, Line: 8}})

	err := session.Post(validWalkthrough())

	assertRejected(t, err, review.RejectedUncoveredChanges)
}

func TestAChangedLineMayBeCoveredByMoreThanOneExcerpt(t *testing.T) {
	walkthrough := validWalkthrough()
	// Two Excerpts overlapping the same changed line — allowed, not a complaint.
	walkthrough.Steps[0].Excerpts = append(walkthrough.Steps[0].Excerpts, review.Excerpt{
		Repository: "/repos/argus-portal", File: "src/fetch.ts", Side: review.NewSide, FirstLine: 20, LastLine: 40,
	})
	session := sessionDeriving(changed("src/fetch.ts", 20, 25))

	if err := session.Post(walkthrough); err != nil {
		t.Fatalf("expected overlapping coverage to be accepted, got %v", err)
	}
}

func TestAStepOverTheBudgetWithoutJustificationIsRejected(t *testing.T) {
	walkthrough := validWalkthrough()
	// One Excerpt spanning many lines, all of them changed.
	walkthrough.Steps[0].Excerpts[0] = review.Excerpt{
		Repository: "/repos/argus-portal", File: "big.ts", Side: review.NewSide, FirstLine: 1, LastLine: 200,
	}
	session := sessionDeriving(changed("big.ts", 1, 40)) // 40 changed lines > 30

	err := session.Post(walkthrough)

	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, "40")
}

func TestAnOversizedStepWithAJustificationIsAccepted(t *testing.T) {
	walkthrough := validWalkthrough()
	walkthrough.Steps[0].Excerpts[0] = review.Excerpt{
		Repository: "/repos/argus-portal", File: "big.ts", Side: review.NewSide, FirstLine: 1, LastLine: 200,
	}
	walkthrough.Steps[0].OversizeJustification = "the state machine only makes sense whole"
	session := sessionDeriving(changed("big.ts", 1, 40))

	if err := session.Post(walkthrough); err != nil {
		t.Fatalf("expected a justified oversized Step to be accepted, got %v", err)
	}
}

func TestReferenceLinesDoNotCountTowardTheBudget(t *testing.T) {
	walkthrough := validWalkthrough()
	// A 200-line Excerpt, but only 10 lines are actually changed; the rest are
	// reference context and must not push the Step over budget.
	walkthrough.Steps[0].Excerpts[0] = review.Excerpt{
		Repository: "/repos/argus-portal", File: "big.ts", Side: review.NewSide, FirstLine: 1, LastLine: 200,
	}
	session := sessionDeriving(changed("big.ts", 5, 14)) // 10 changed lines

	if err := session.Post(walkthrough); err != nil {
		t.Fatalf("expected reference lines to be free, got %v", err)
	}
}

func TestLiveCoverageCountsChangedLinesSeenSoFar(t *testing.T) {
	walkthrough := validWalkthrough()
	walkthrough.Steps = []review.Step{
		{
			Name: "First", Explanation: "first",
			Excerpts: []review.Excerpt{{Repository: "/repos/argus-portal", File: "a.ts", Side: review.NewSide, FirstLine: 1, LastLine: 10}},
		},
		{
			Name: "Second", Explanation: "second",
			Excerpts: []review.Excerpt{{Repository: "/repos/argus-portal", File: "b.ts", Side: review.NewSide, FirstLine: 1, LastLine: 10}},
		},
	}
	lines := append(changed("a.ts", 1, 3), changed("b.ts", 1, 4)...) // 3 + 4 = 7 total
	session := sessionDeriving(lines)
	mustPost(t, session, walkthrough)

	atBrief := session.View()
	if atBrief.Coverage.Total != 7 || atBrief.Coverage.Seen != 0 {
		t.Fatalf("at the Brief expected 0/7, got %d/%d", atBrief.Coverage.Seen, atBrief.Coverage.Total)
	}

	mustAdvance(t, session)
	afterFirst := session.View()
	if afterFirst.Coverage.Seen != 3 {
		t.Errorf("after Step 1 expected 3 seen, got %d", afterFirst.Coverage.Seen)
	}

	mustAdvance(t, session)
	afterSecond := session.View()
	if afterSecond.Coverage.Seen != 7 {
		t.Errorf("after Step 2 expected all 7 seen, got %d", afterSecond.Coverage.Seen)
	}
}

func mustAdvance(t *testing.T, session *review.Session) {
	t.Helper()
	if err := session.Advance(); err != nil {
		t.Fatalf("expected to advance, got %v", err)
	}
}

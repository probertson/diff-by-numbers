package review_test

import (
	"fmt"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// fixedDeriver returns the same Changed Lines and Opaque Changes for any
// repository, so a test can state exactly what git "found" without a filesystem.
type fixedDeriver struct {
	lines  []review.ChangedLine
	opaque []review.OpaqueChange
}

func (d fixedDeriver) Derive(repo review.Repository) (review.Derivation, error) {
	lines := make([]review.ChangedLine, len(d.lines))
	for i, l := range d.lines {
		l.Repository = repo.Root
		lines[i] = l
	}
	opaque := make([]review.OpaqueChange, len(d.opaque))
	for i, o := range d.opaque {
		o.Repository = repo.Root
		opaque[i] = o
	}
	return review.Derivation{Lines: lines, Opaque: opaque}, nil
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

func TestUncoveredChangesRejectionPluralizesTheCount(t *testing.T) {
	// The rejection reaches the agent as MCP detail, so its count must read
	// properly: "1 change", never the lazy "change(s)".
	session := sessionDeriving([]review.ChangedLine{{File: "src/fetch.ts", Side: review.NewSide, Line: 100}})

	err := session.Post(validWalkthrough())

	assertRejected(t, err, review.RejectedUncoveredChanges)
	assertDetailContains(t, err, "1 change")
	assertDetailOmits(t, err, "change(s)")
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

func TestResolvedLinesAreMarkedChangedOrReference(t *testing.T) {
	walkthrough := validWalkthrough()
	// Excerpt covers 12-34; only 20-22 actually changed. The rest are reference.
	session := sessionDeriving(changed("src/fetch.ts", 20, 22))
	mustPost(t, session, walkthrough)
	mustAdvance(t, session)

	lines := session.View().Step.Excerpts[0].Lines
	changedNumbers := map[int]bool{}
	for _, line := range lines {
		if line.Changed {
			changedNumbers[line.Number] = true
		}
	}

	for _, n := range []int{20, 21, 22} {
		if !changedNumbers[n] {
			t.Errorf("expected line %d to be marked changed", n)
		}
	}
	for _, n := range []int{12, 19, 23, 34} {
		if changedNumbers[n] {
			t.Errorf("expected line %d to be reference, not changed", n)
		}
	}
}

// bigStep is one Step of a budget test: what it is called, and how many changed
// lines it shows.
type bigStep struct {
	name  string
	lines int
}

// oversizedWalkthrough builds a Walkthrough whose Steps each show one file, and
// the Changed Lines that make those Steps the size they claim to be. Both come
// from here so the two halves cannot drift: a test that derived the file names
// itself would pass for the wrong reason the moment this helper renamed them.
func oversizedWalkthrough(steps ...bigStep) (review.Walkthrough, []review.ChangedLine) {
	walkthrough := validWalkthrough()
	walkthrough.Steps = nil
	var lines []review.ChangedLine
	for i, step := range steps {
		file := fmt.Sprintf("big%d.ts", i+1)
		walkthrough.Steps = append(walkthrough.Steps, review.Step{
			Name:        step.name,
			Explanation: "why this Step exists, in the reviewer's terms",
			Excerpts: []review.Excerpt{{
				Repository: "/repos/argus-portal", File: file,
				Side: review.NewSide, FirstLine: 1, LastLine: 200,
			}},
		})
		lines = append(lines, changed(file, 1, step.lines)...)
	}
	return walkthrough, lines
}

// An Authoring Agent used to find oversized Steps one rejected post at a time,
// re-sending the whole Walkthrough — Brief, every Step, every Excerpt — to learn
// about the next one. All of them are present on the first attempt, so all of
// them are reported on the first attempt.
func TestEveryOversizedStepIsReportedInOneRejection(t *testing.T) {
	walkthrough, lines := oversizedWalkthrough(
		bigStep{"Wire the flag through", 31},
		bigStep{"New state on the model, and a new mode", 51},
		bigStep{"Tests for the new mode", 44},
	)
	session := sessionDeriving(lines)

	err := session.Post(walkthrough)

	assertRejected(t, err, review.RejectedOversizedStep)
	// Asserted as one string rather than three `contains` checks, so the Steps
	// have to be listed in Step order and each has to carry its own name and
	// count — an order-blind check would pass on a reversed list.
	assertDetailContains(t, err, `3 Steps are over the budget of 30 changed lines and give no justification:
  Step 1 ("Wire the flag through") shows 31
  Step 2 ("New state on the model, and a new mode") shows 51
  Step 3 ("Tests for the new mode") shows 44`)
}

// Twenty Steps in, "Step 6" alone means counting positions in a JSON array to
// find the one to fix.
func TestAnOversizedStepIsNamedNotJustNumbered(t *testing.T) {
	walkthrough, lines := oversizedWalkthrough(bigStep{"New state on the model, and a new mode", 51})
	session := sessionDeriving(lines)

	err := session.Post(walkthrough)

	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, `Step 1 ("New state on the model, and a new mode")`)
}

func TestAJustifiedStepIsLeftOutOfTheOversizedList(t *testing.T) {
	walkthrough, lines := oversizedWalkthrough(
		bigStep{"Justified and huge", 90},
		bigStep{"Unjustified and huge", 44},
	)
	walkthrough.Steps[0].OversizeJustification = "the state machine only makes sense whole"
	session := sessionDeriving(lines)

	err := session.Post(walkthrough)

	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, "Unjustified and huge")
	assertDetailOmits(t, err, "Justified and huge")
	assertDetailOmits(t, err, "90")
}

// One offender keeps the single-sentence form: a list of one reads worse.
func TestASingleOversizedStepUsesTheOneLineForm(t *testing.T) {
	walkthrough, lines := oversizedWalkthrough(bigStep{"Only me", 51})
	session := sessionDeriving(lines)

	err := session.Post(walkthrough)

	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, "over the budget of 30, and gives no justification")
	assertDetailOmits(t, err, "\n")
}

func TestStepsWithinTheBudgetAreNotRejected(t *testing.T) {
	walkthrough, lines := oversizedWalkthrough(
		bigStep{"Comfortably small", 10},
		bigStep{"Right on the budget", 30},
	)
	session := sessionDeriving(lines)

	if err := session.Post(walkthrough); err != nil {
		t.Fatalf("nothing is over the budget, so nothing should be rejected, got %v", err)
	}
}

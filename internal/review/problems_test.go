package review_test

import (
	"errors"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// assertProblems checks the exact reasons a rejection carries, in order.
func assertProblems(t *testing.T, err error, want ...review.RejectionReason) {
	t.Helper()
	var rejection *review.Rejection
	if !errors.As(err, &rejection) {
		t.Fatalf("expected a structured rejection, got %v", err)
	}
	var got []review.RejectionReason
	for _, problem := range rejection.Problems {
		got = append(got, problem.Reason)
	}
	if len(got) != len(want) {
		t.Fatalf("expected problems %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected problems %v, got %v", want, got)
		}
	}
}

// An agent used to find one fault per post, re-sending the whole Round
// each time. Four posts to get one accepted was ordinary. Everything a stage-2
// check can see is present on the first attempt, so it is all reported at once.
func TestEveryStageTwoProblemIsReportedTogether(t *testing.T) {
	walkthrough := validRound()
	// One oversized Step that also fails to cover everything, plus an
	// Acknowledgement claiming a file with nothing in it.
	walkthrough.Steps[0].Excerpts[0] = review.Excerpt{
		Repository: "/repos/argus-portal", File: "big.ts", Side: review.NewSide, FirstLine: 1, LastLine: 200,
	}
	walkthrough.Steps[0].Acknowledgements = []review.Acknowledgement{
		{Repository: "/repos/argus-portal", Files: []string{"untouched.ts"}, Reason: "nothing here"},
	}
	var lines []review.ChangedLine
	lines = append(lines, changed("big.ts", 1, 40)...)      // oversized
	lines = append(lines, changed("elsewhere.ts", 1, 3)...) // uncovered
	session := sessionDeriving(lines)

	err := session.Post(walkthrough)

	assertProblems(t, err,
		review.RejectedOversizedStep,
		review.RejectedEmptyAcknowledgement,
		review.RejectedUncoveredChanges)
}

// Stage 1 stops: the later checks mean nothing without a well-formed
// Round, so reporting guesses from them would be noise.
func TestAStructuralFailureIsReportedOnItsOwn(t *testing.T) {
	walkthrough := validRound()
	walkthrough.Brief.Goal = "" // structural
	walkthrough.Steps[0].Excerpts[0] = review.Excerpt{
		Repository: "/repos/argus-portal", File: "big.ts", Side: review.NewSide, FirstLine: 1, LastLine: 200,
	}
	session := sessionDeriving(changed("big.ts", 1, 40)) // would also be oversized

	err := session.Post(walkthrough)

	assertProblems(t, err, review.RejectedMalformedBrief)
}

// Derivation is stage 1 too: with no ledger there is nothing to check coverage
// or the budget against.
func TestADerivationFailureStopsBeforeStageTwo(t *testing.T) {
	walkthrough := validRound()
	session := review.NewSession(stubResolver{}, failingDeriver{})

	err := session.Post(walkthrough)

	assertProblems(t, err, review.RejectedDerivationFailed)
}

type failingDeriver struct{}

func (failingDeriver) Derive(review.Repository) (review.Derivation, error) {
	return review.Derivation{}, errors.New("no such ref")
}

// The old message showed five atoms and "and 259 more", which named a fraction
// of the work and made the agent post again to discover the rest. It now lists
// everything, grouped and collapsed so that a long list stays readable.
func TestUncoveredChangesAreGroupedByFileAndSideWithRangesCollapsed(t *testing.T) {
	walkthrough := validRound()
	// A Step that covers nothing of what follows.
	walkthrough.Steps[0].Excerpts[0] = review.Excerpt{
		Repository: "/repos/argus-portal", File: "covered.ts", Side: review.NewSide, FirstLine: 1, LastLine: 1,
	}
	var lines []review.ChangedLine
	lines = append(lines, changed("covered.ts", 1, 1)...)
	lines = append(lines, changed("CLAUDE.md", 16, 18)...)
	lines = append(lines, review.ChangedLine{File: "CLAUDE.md", Side: review.NewSide, Line: 40})
	lines = append(lines, oldSide("CONTEXT.md", 125, 131)...)
	session := sessionDeriving(lines)

	err := session.Post(walkthrough)

	assertRejected(t, err, review.RejectedUncoveredChanges)
	// Consecutive lines collapse to a range, a lone line stands alone, and the
	// two sides of a file are kept apart.
	assertDetailContains(t, err, "CLAUDE.md")
	assertDetailContains(t, err, "new 16-18, 40")
	assertDetailContains(t, err, "CONTEXT.md")
	assertDetailContains(t, err, "old 125-131")
	assertDetailOmits(t, err, "and 3 more")
}

// oldSide is changed() for removals, which have no working-tree counterpart.
func oldSide(file string, first, last int) []review.ChangedLine {
	var lines []review.ChangedLine
	for n := first; n <= last; n++ {
		lines = append(lines, review.ChangedLine{File: file, Side: review.OldSide, Line: n})
	}
	return lines
}

// A safety valve, not a budget: collapsing consecutive lines means a real
// branch never approaches it, but a pathological Change Set of scattered single
// lines should not produce a message nobody can read.
func TestAVeryLongUncoveredListIsCappedWithACount(t *testing.T) {
	walkthrough := validRound()
	walkthrough.Steps[0].Excerpts[0] = review.Excerpt{
		Repository: "/repos/argus-portal", File: "covered.ts", Side: review.NewSide, FirstLine: 1, LastLine: 1,
	}
	lines := changed("covered.ts", 1, 1)
	// 300 non-consecutive lines, so nothing collapses: 300 separate ranges.
	for n := 1; n <= 600; n += 2 {
		lines = append(lines, review.ChangedLine{File: "scattered.ts", Side: review.NewSide, Line: n})
	}
	session := sessionDeriving(lines)

	err := session.Post(walkthrough)

	assertRejected(t, err, review.RejectedUncoveredChanges)
	assertDetailContains(t, err, "… and 200 more ranges in 1 file")
	// The count in the header is of everything, not of what was listed.
	assertDetailContains(t, err, "accounts for 300 changed lines")
}

// conclude refuses through the same shape, so an agent has one way to read a
// refusal rather than two.
func TestConcludeRefusesWithProblemsToo(t *testing.T) {
	session := newSession()
	mustPost(t, session, validRound())

	err := session.Conclude("not-the-review-under-review")

	assertProblems(t, err, review.RejectedUnknownReview)
}

// An Opaque Change has no lines, so a message that counts only lines reported
// "0 changed lines in 0 files" and then listed one anyway.
func TestAnUncoveredOpaqueChangeIsCountedAsOne(t *testing.T) {
	walkthrough := validRound()
	walkthrough.Steps[0].Excerpts[0] = review.Excerpt{
		Repository: "/repos/argus-portal", File: "covered.ts", Side: review.NewSide, FirstLine: 1, LastLine: 1,
	}
	session := review.NewSession(stubResolver{}, fixedDeriver{
		lines:  changed("covered.ts", 1, 1),
		opaque: []review.OpaqueChange{{File: "logo.png", Kind: review.OpaqueBinary}},
	})

	err := session.Post(walkthrough)

	assertRejected(t, err, review.RejectedUncoveredChanges)
	assertDetailContains(t, err, "accounts for 1 Opaque Change in 1 file")
	assertDetailOmits(t, err, "0 changed lines")
}

func TestLinesAndOpaqueChangesAreCountedSeparately(t *testing.T) {
	walkthrough := validRound()
	walkthrough.Steps[0].Excerpts[0] = review.Excerpt{
		Repository: "/repos/argus-portal", File: "covered.ts", Side: review.NewSide, FirstLine: 1, LastLine: 1,
	}
	session := review.NewSession(stubResolver{}, fixedDeriver{
		lines:  append(changed("covered.ts", 1, 1), changed("missed.ts", 1, 3)...),
		opaque: []review.OpaqueChange{{File: "logo.png", Kind: review.OpaqueBinary}},
	})

	err := session.Post(walkthrough)

	assertDetailContains(t, err, "3 changed lines and 1 Opaque Change in 2 files")
}

// The count beside the truncation is of the files that were actually cut, not
// of every file with something uncovered.
func TestTheTruncationCountsOnlyTheFilesItCut(t *testing.T) {
	walkthrough := validRound()
	walkthrough.Steps[0].Excerpts[0] = review.Excerpt{
		Repository: "/repos/argus-portal", File: "covered.ts", Side: review.NewSide, FirstLine: 1, LastLine: 1,
	}
	lines := changed("covered.ts", 1, 1)
	// Rows are listed in name order, so the small file is listed in full and the
	// scattered one absorbs — and overruns — the rest of the cap. Exactly one
	// file is therefore cut, though two have uncovered lines.
	lines = append(lines, changed("a-small.ts", 1, 2)...)
	for n := 1; n <= 300; n += 2 {
		lines = append(lines, review.ChangedLine{File: "z-scattered.ts", Side: review.NewSide, Line: n})
	}
	session := sessionDeriving(lines)

	err := session.Post(walkthrough)

	assertRejected(t, err, review.RejectedUncoveredChanges)
	assertDetailContains(t, err, "… and 51 more ranges in 1 file")
	// The small file was listed in full rather than swallowed by the cap.
	assertDetailContains(t, err, "a-small.ts")
}

// ADR-0009: a Change Set may span repositories, and coverage aggregates across
// all of them. Two same-named files must not merge into one row, which would
// interleave line numbers belonging to different trees.
func TestSameNamedFilesInDifferentRepositoriesAreKeptApart(t *testing.T) {
	walkthrough := validRound()
	walkthrough.ChangeSet.Repositories = []review.Repository{
		{Root: "/repos/one", Base: "main"},
		{Root: "/repos/two", Base: "main"},
	}
	walkthrough.Steps[0].Excerpts[0] = review.Excerpt{
		Repository: "/repos/one", File: "covered.ts", Side: review.NewSide, FirstLine: 1, LastLine: 1,
	}
	// fixedDeriver stamps its own Repository per repository, so each gets both.
	session := sessionDeriving(append(changed("covered.ts", 1, 1), changed("app.ts", 5, 5)...))

	err := session.Post(walkthrough)

	assertRejected(t, err, review.RejectedUncoveredChanges)
	assertDetailContains(t, err, "/repos/one/app.ts")
	assertDetailContains(t, err, "/repos/two/app.ts")
}

package review_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

func validRound() review.Round {
	return review.Round{
		Brief: review.Brief{
			Goal:     "Add retry with backoff to the fetch layer",
			Approach: "Wrap the transport in a retrier, then thread the policy through callers",
		},
		ChangeSet: review.ChangeSet{
			Repositories: []review.Repository{
				{Root: "/repos/argus-portal", Base: "merge-base"},
			},
		},
		Steps: []review.Step{
			{
				Name:        "Add the retrier",
				Explanation: "A transport wrapper that retries idempotent requests",
				Excerpts: []review.Excerpt{
					{Repository: "/repos/argus-portal", File: "src/fetch.ts", Side: review.NewSide, FirstLine: 12, LastLine: 34},
				},
			},
		},
	}
}

func TestAPostedRoundIsAcceptedAndAwaitsTheReviewer(t *testing.T) {
	session := newSession()

	err := session.Post(validRound())

	if err != nil {
		t.Fatalf("expected the Round to be accepted, got %v", err)
	}
	results, err := session.Results()
	if err != nil {
		t.Fatalf("expected results to be readable, got %v", err)
	}
	if results.Finished {
		t.Error("expected the Round not to be finished")
	}
	if len(results.Comments) != 0 {
		t.Errorf("expected no Comments, got %d", len(results.Comments))
	}
}

func TestASecondRoundIsRejectedWhileOneIsActive(t *testing.T) {
	session := newSession()
	mustPost(t, session, validRound())

	err := session.Post(validRound())

	assertRejected(t, err, review.RejectedWalkthroughActive)
}

// span is the everyday Reviewer selection: a run on the Excerpt's own side, from
// one of its lines to another. sideSpan names the side explicitly, for a run in a
// unified diff where a before-row and an after-row can share a line number.
func span(excerpt, first, last int) review.AnchorTarget {
	return review.AnchorTarget{
		ExcerptIndex: excerpt,
		Start:        review.AnchorEndpoint{Line: first},
		End:          review.AnchorEndpoint{Line: last},
	}
}

func sideSpan(excerpt int, start review.AnchorEndpoint, end review.AnchorEndpoint) review.AnchorTarget {
	return review.AnchorTarget{ExcerptIndex: excerpt, Start: start, End: end}
}

func oldRow(line int) review.AnchorEndpoint {
	return review.AnchorEndpoint{Side: review.OldSide, Line: line}
}

func newRow(line int) review.AnchorEndpoint {
	return review.AnchorEndpoint{Side: review.NewSide, Line: line}
}

func mustPost(t *testing.T, session *review.Session, w review.Round) {
	t.Helper()
	if err := session.Post(w); err != nil {
		t.Fatalf("expected the Round to be accepted, got %v", err)
	}
}

func assertRejected(t *testing.T, err error, want review.RejectionReason) {
	t.Helper()
	var rejection *review.Rejection
	if !errors.As(err, &rejection) {
		t.Fatalf("expected a structured rejection, got %v", err)
	}
	if !rejection.Has(want) {
		t.Errorf("expected a %q problem, got %v", want, reasonsOf(rejection))
	}
	for _, problem := range rejection.Problems {
		if problem.Detail == "" {
			t.Errorf("the %q problem names nothing to fix", problem.Reason)
		}
	}
}

func TestARoundOverAnEmptyChangeSetIsRejected(t *testing.T) {
	session := newSession()
	walkthrough := validRound()
	walkthrough.ChangeSet.Repositories = nil

	err := session.Post(walkthrough)

	assertRejected(t, err, review.RejectedEmptyChangeSet)
}

func TestAMalformedBriefIsRejectedNamingTheProblem(t *testing.T) {
	cases := map[string]func(*review.Round){
		"no goal": func(w *review.Round) {
			w.Brief.Goal = ""
		},
		"no approach": func(w *review.Round) {
			w.Brief.Approach = ""
		},
	}

	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			session := newSession()
			walkthrough := validRound()
			breakIt(&walkthrough)

			err := session.Post(walkthrough)

			assertRejected(t, err, review.RejectedMalformedBrief)
		})
	}
}

func TestAMalformedStepIsRejectedNamingTheProblem(t *testing.T) {
	cases := map[string]func(*review.Round){
		"no Steps at all": func(w *review.Round) {
			w.Steps = nil
		},
		"Step with no name": func(w *review.Round) {
			w.Steps[0].Name = ""
		},
		"Step with no explanation": func(w *review.Round) {
			w.Steps[0].Explanation = ""
		},
		"Step with no Excerpts": func(w *review.Round) {
			w.Steps[0].Excerpts = nil
		},
		"Excerpt ending before it starts": func(w *review.Round) {
			w.Steps[0].Excerpts[0].FirstLine = 40
			w.Steps[0].Excerpts[0].LastLine = 12
		},
		"Excerpt starting before line one": func(w *review.Round) {
			w.Steps[0].Excerpts[0].FirstLine = 0
		},
		"Excerpt with no file": func(w *review.Round) {
			w.Steps[0].Excerpts[0].File = ""
		},
		"Excerpt with an unqualified side": func(w *review.Round) {
			w.Steps[0].Excerpts[0].Side = ""
		},
		"Excerpt naming a repository outside the Change Set": func(w *review.Round) {
			w.Steps[0].Excerpts[0].Repository = "/repos/somewhere-else"
		},
	}

	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			session := newSession()
			walkthrough := validRound()
			breakIt(&walkthrough)

			err := session.Post(walkthrough)

			assertRejected(t, err, review.RejectedMalformedStep)
		})
	}
}

func TestDumpRendersThePostedRoundAsText(t *testing.T) {
	session := newSession()
	mustPost(t, session, validRound())

	dump := session.Dump()

	for _, want := range []string{
		"Add retry with backoff to the fetch layer",
		"Wrap the transport in a retrier",
		"Add the retrier",
		"A transport wrapper that retries idempotent requests",
		"src/fetch.ts",
		"12",
		"34",
	} {
		if !strings.Contains(dump, want) {
			t.Errorf("expected the dump to mention %q\n--- dump ---\n%s", want, dump)
		}
	}
}

func TestDumpSaysSoWhenNoRoundIsPosted(t *testing.T) {
	session := newSession()

	dump := session.Dump()

	if !strings.Contains(dump, "no Round") {
		t.Errorf("expected the dump to say no Round is posted, got %q", dump)
	}
}

func TestDumpCountsASingleStepInTheSingular(t *testing.T) {
	session := newSession()
	mustPost(t, session, validRound())

	dump := session.Dump()

	if !strings.Contains(dump, "1 Step:") {
		t.Errorf("expected a single Step to be counted in the singular\n--- dump ---\n%s", dump)
	}
}

func TestARoundCanBePostedOnceTheLastIsAbandoned(t *testing.T) {
	session := newSession()
	mustPost(t, session, validRound())

	if err := session.Abandon(); err != nil {
		t.Fatalf("expected the Round to be abandoned, got %v", err)
	}

	if err := session.Post(validRound()); err != nil {
		t.Fatalf("expected a new Round to be accepted after abandoning, got %v", err)
	}
}

func TestAbandoningWithNothingPostedIsRejected(t *testing.T) {
	session := newSession()

	err := session.Abandon()

	assertRejected(t, err, review.RejectedNoRound)
}

func TestResultsReportThatNothingIsPostedRatherThanUnfinished(t *testing.T) {
	session := newSession()

	results, err := session.Results()

	if err != nil {
		t.Fatalf("expected results to be readable, got %v", err)
	}
	if results.Posted {
		t.Error("expected results to report that no Round is posted")
	}
}

func TestResultsReportAPostedRoundAsPosted(t *testing.T) {
	session := newSession()
	mustPost(t, session, validRound())

	results, err := session.Results()

	if err != nil {
		t.Fatalf("expected results to be readable, got %v", err)
	}
	if !results.Posted {
		t.Error("expected results to report the Round as posted")
	}
}

func TestAnExcerptFileEscapingItsRepositoryIsRejected(t *testing.T) {
	cases := map[string]string{
		"absolute path":            "/etc/passwd",
		"parent traversal":         "../../etc/passwd",
		"traversal after a prefix": "src/../../../etc/passwd",
		"bare parent":              "..",
	}

	for name, file := range cases {
		t.Run(name, func(t *testing.T) {
			session := newSession()
			walkthrough := validRound()
			walkthrough.Steps[0].Excerpts[0].File = file

			err := session.Post(walkthrough)

			assertRejected(t, err, review.RejectedMalformedStep)
		})
	}
}

func TestAnExcerptFileInsideItsRepositoryIsAccepted(t *testing.T) {
	for _, file := range []string{"src/fetch.ts", "src/../src/fetch.ts", "./src/fetch.ts"} {
		t.Run(file, func(t *testing.T) {
			session := newSession()
			walkthrough := validRound()
			walkthrough.Steps[0].Excerpts[0].File = file

			if err := session.Post(walkthrough); err != nil {
				t.Fatalf("expected %q to be accepted, got %v", file, err)
			}
		})
	}
}

func TestAChangeSetNamingABlankOrRelativeRepositoryIsRejected(t *testing.T) {
	cases := map[string]review.Repository{
		"blank root":    {Root: "", Base: "merge-base"},
		"relative root": {Root: "repos/argus-portal", Base: "merge-base"},
	}

	for name, repository := range cases {
		t.Run(name, func(t *testing.T) {
			session := newSession()
			walkthrough := validRound()
			walkthrough.ChangeSet.Repositories = []review.Repository{repository}
			walkthrough.Steps[0].Excerpts[0].Repository = repository.Root

			err := session.Post(walkthrough)

			assertRejected(t, err, review.RejectedEmptyChangeSet)
		})
	}
}

// The field is a base ref, not a range: dbn reviews from the merge-base of that
// ref and HEAD to the working tree. Agents wrote "HEAD~1..HEAD" into it, which
// git read as one ref of that name and failed to resolve — surfacing as a raw
// merge-base error, or a bare derivation_failed, neither of which says what is
// actually wrong. It is caught before any git call now.
func TestABaseThatIsARangeIsRejectedForWhatItIs(t *testing.T) {
	for _, base := range []string{"HEAD~1..HEAD", "849fb87..HEAD", "main...feature"} {
		t.Run(base, func(t *testing.T) {
			session := newSession()
			walkthrough := validRound()
			walkthrough.ChangeSet.Repositories[0].Base = base

			err := session.Post(walkthrough)

			assertRejected(t, err, review.RejectedMalformedBase)
			assertDetailContains(t, err, "base takes a single ref, not A..B")
			assertDetailContains(t, err, "use HEAD~1")
		})
	}
}

func TestAMissingBaseIsRejectedAsAMalformedBase(t *testing.T) {
	session := newSession()
	walkthrough := validRound()
	walkthrough.ChangeSet.Repositories[0].Base = ""

	err := session.Post(walkthrough)

	assertRejected(t, err, review.RejectedMalformedBase)
	assertDetailContains(t, err, "names no base")
}

// The check runs before any git call, so a deriver that would have failed is
// never reached — the agent is told what is wrong with the ref, not what git
// made of it.
func TestAnOrdinaryRefStillDerives(t *testing.T) {
	session := sessionDeriving(changed("src/fetch.ts", 20, 25))
	walkthrough := validRound()
	walkthrough.ChangeSet.Repositories[0].Base = "main"

	if err := session.Post(walkthrough); err != nil {
		t.Fatalf("a plain ref is exactly what base is for, got %v", err)
	}
}

func assertDetailContains(t *testing.T, err error, want string) {
	t.Helper()
	var rejection *review.Rejection
	if !errors.As(err, &rejection) {
		t.Fatalf("expected a structured rejection, got %v", err)
	}
	if !strings.Contains(rejection.Error(), want) {
		t.Errorf("expected the rejection to mention %q, got %q", want, rejection.Error())
	}
}

func assertDetailOmits(t *testing.T, err error, unwanted string) {
	t.Helper()
	var rejection *review.Rejection
	if !errors.As(err, &rejection) {
		t.Fatalf("expected a structured rejection, got %v", err)
	}
	if strings.Contains(rejection.Error(), unwanted) {
		t.Errorf("expected the rejection not to contain %q, got %q", unwanted, rejection.Error())
	}
}

func TestEachAcceptedRoundIsANewPosting(t *testing.T) {
	// A surface that remembers how the Reviewer left each Step must forget it when
	// a different Round takes the screen, so it needs to see that happen.
	session := newSession()
	mustPost(t, session, validRound())
	first := session.View().Posting

	_ = session.Post(validRound()) // rejected: one is already under review
	rejected := session.View().Posting
	if err := session.Finish(); err != nil {
		t.Fatal(err)
	}
	mustPost(t, session, validRound()) // a new review: the hand-off raised nothing
	revised := session.View().Posting

	if first == 0 || rejected != first {
		t.Errorf("expected a rejected post to leave the posting at %d, got %d", first, rejected)
	}
	if revised == first {
		t.Errorf("expected a Revision Round to be a new posting, still %d", revised)
	}
}

// reasonsOf lists what a rejection complained about, for a failure message that
// says what actually came back rather than only what did not.
func reasonsOf(rejection *review.Rejection) []review.RejectionReason {
	var reasons []review.RejectionReason
	for _, problem := range rejection.Problems {
		reasons = append(reasons, problem.Reason)
	}
	return reasons
}

package review_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

func validWalkthrough() review.Walkthrough {
	return review.Walkthrough{
		Brief: review.Brief{
			Ask:      "Add retry with backoff to the fetch layer",
			Approach: "Wrap the transport in a retrier, then thread the policy through callers",
			Provenance: review.Provenance{
				Kind:     review.ProvenanceStated,
				Citation: "session 51e67df2, prompt 3",
			},
		},
		ChangeSet: review.ChangeSet{
			Repositories: []review.Repository{
				{Root: "/repos/argus-portal", Range: "merge-base"},
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

func TestAPostedWalkthroughIsAcceptedAndAwaitsTheReviewer(t *testing.T) {
	session := newSession()

	err := session.Post(validWalkthrough())

	if err != nil {
		t.Fatalf("expected the Walkthrough to be accepted, got %v", err)
	}
	results, err := session.Results()
	if err != nil {
		t.Fatalf("expected results to be readable, got %v", err)
	}
	if results.Finished {
		t.Error("expected the Walkthrough not to be finished")
	}
	if len(results.ChangeRequests) != 0 {
		t.Errorf("expected no Change Requests, got %d", len(results.ChangeRequests))
	}
}

func TestASecondWalkthroughIsRejectedWhileOneIsActive(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough())

	err := session.Post(validWalkthrough())

	assertRejected(t, err, review.RejectedWalkthroughActive)
}

func mustPost(t *testing.T, session *review.Session, w review.Walkthrough) {
	t.Helper()
	if err := session.Post(w); err != nil {
		t.Fatalf("expected the Walkthrough to be accepted, got %v", err)
	}
}

func assertRejected(t *testing.T, err error, want review.RejectionReason) {
	t.Helper()
	var rejection *review.Rejection
	if !errors.As(err, &rejection) {
		t.Fatalf("expected a structured rejection, got %v", err)
	}
	if rejection.Reason != want {
		t.Errorf("expected rejection reason %q, got %q (%s)", want, rejection.Reason, rejection.Detail)
	}
	if rejection.Detail == "" {
		t.Error("expected the rejection to name the problem, got an empty detail")
	}
}

func TestAWalkthroughOverAnEmptyChangeSetIsRejected(t *testing.T) {
	session := newSession()
	walkthrough := validWalkthrough()
	walkthrough.ChangeSet.Repositories = nil

	err := session.Post(walkthrough)

	assertRejected(t, err, review.RejectedEmptyChangeSet)
}

func TestAMalformedBriefIsRejectedNamingTheProblem(t *testing.T) {
	cases := map[string]func(*review.Walkthrough){
		"no ask": func(w *review.Walkthrough) {
			w.Brief.Ask = ""
		},
		"no approach": func(w *review.Walkthrough) {
			w.Brief.Approach = ""
		},
		"no provenance declared": func(w *review.Walkthrough) {
			w.Brief.Provenance.Kind = ""
		},
		"stated provenance without a citation": func(w *review.Walkthrough) {
			w.Brief.Provenance = review.Provenance{Kind: review.ProvenanceStated, Citation: ""}
		},
	}

	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			session := newSession()
			walkthrough := validWalkthrough()
			breakIt(&walkthrough)

			err := session.Post(walkthrough)

			assertRejected(t, err, review.RejectedMalformedBrief)
		})
	}
}

func TestInferredProvenanceNeedsNoCitation(t *testing.T) {
	session := newSession()
	walkthrough := validWalkthrough()
	walkthrough.Brief.Provenance = review.Provenance{Kind: review.ProvenanceInferred}

	err := session.Post(walkthrough)

	if err != nil {
		t.Fatalf("expected inferred Provenance to be accepted without a citation, got %v", err)
	}
}

func TestAMalformedStepIsRejectedNamingTheProblem(t *testing.T) {
	cases := map[string]func(*review.Walkthrough){
		"no Steps at all": func(w *review.Walkthrough) {
			w.Steps = nil
		},
		"Step with no name": func(w *review.Walkthrough) {
			w.Steps[0].Name = ""
		},
		"Step with no explanation": func(w *review.Walkthrough) {
			w.Steps[0].Explanation = ""
		},
		"Step with no Excerpts": func(w *review.Walkthrough) {
			w.Steps[0].Excerpts = nil
		},
		"Excerpt ending before it starts": func(w *review.Walkthrough) {
			w.Steps[0].Excerpts[0].FirstLine = 40
			w.Steps[0].Excerpts[0].LastLine = 12
		},
		"Excerpt starting before line one": func(w *review.Walkthrough) {
			w.Steps[0].Excerpts[0].FirstLine = 0
		},
		"Excerpt with no file": func(w *review.Walkthrough) {
			w.Steps[0].Excerpts[0].File = ""
		},
		"Excerpt with an unqualified side": func(w *review.Walkthrough) {
			w.Steps[0].Excerpts[0].Side = ""
		},
		"Excerpt naming a repository outside the Change Set": func(w *review.Walkthrough) {
			w.Steps[0].Excerpts[0].Repository = "/repos/somewhere-else"
		},
	}

	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			session := newSession()
			walkthrough := validWalkthrough()
			breakIt(&walkthrough)

			err := session.Post(walkthrough)

			assertRejected(t, err, review.RejectedMalformedStep)
		})
	}
}

func TestDumpRendersThePostedWalkthroughAsText(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough())

	dump := session.Dump()

	for _, want := range []string{
		"Add retry with backoff to the fetch layer",
		"Wrap the transport in a retrier",
		"stated",
		"session 51e67df2, prompt 3",
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

func TestDumpSaysSoWhenNoWalkthroughIsPosted(t *testing.T) {
	session := newSession()

	dump := session.Dump()

	if !strings.Contains(dump, "no Walkthrough") {
		t.Errorf("expected the dump to say no Walkthrough is posted, got %q", dump)
	}
}

func TestDumpCountsASingleStepInTheSingular(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough())

	dump := session.Dump()

	if !strings.Contains(dump, "1 Step:") {
		t.Errorf("expected a single Step to be counted in the singular\n--- dump ---\n%s", dump)
	}
}

func TestAWalkthroughCanBePostedOnceTheLastIsAbandoned(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough())

	if err := session.Abandon(); err != nil {
		t.Fatalf("expected the Walkthrough to be abandoned, got %v", err)
	}

	if err := session.Post(validWalkthrough()); err != nil {
		t.Fatalf("expected a new Walkthrough to be accepted after abandoning, got %v", err)
	}
}

func TestAbandoningWithNothingPostedIsRejected(t *testing.T) {
	session := newSession()

	err := session.Abandon()

	assertRejected(t, err, review.RejectedNoWalkthrough)
}

func TestResultsReportThatNothingIsPostedRatherThanUnfinished(t *testing.T) {
	session := newSession()

	results, err := session.Results()

	if err != nil {
		t.Fatalf("expected results to be readable, got %v", err)
	}
	if results.Posted {
		t.Error("expected results to report that no Walkthrough is posted")
	}
}

func TestResultsReportAPostedWalkthroughAsPosted(t *testing.T) {
	session := newSession()
	mustPost(t, session, validWalkthrough())

	results, err := session.Results()

	if err != nil {
		t.Fatalf("expected results to be readable, got %v", err)
	}
	if !results.Posted {
		t.Error("expected results to report the Walkthrough as posted")
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
			walkthrough := validWalkthrough()
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
			walkthrough := validWalkthrough()
			walkthrough.Steps[0].Excerpts[0].File = file

			if err := session.Post(walkthrough); err != nil {
				t.Fatalf("expected %q to be accepted, got %v", file, err)
			}
		})
	}
}

func TestAChangeSetNamingABlankOrRelativeRepositoryIsRejected(t *testing.T) {
	cases := map[string]review.Repository{
		"blank root":    {Root: "", Range: "merge-base"},
		"relative root": {Root: "repos/argus-portal", Range: "merge-base"},
		"blank range":   {Root: "/repos/argus-portal", Range: ""},
	}

	for name, repository := range cases {
		t.Run(name, func(t *testing.T) {
			session := newSession()
			walkthrough := validWalkthrough()
			walkthrough.ChangeSet.Repositories = []review.Repository{repository}
			walkthrough.Steps[0].Excerpts[0].Repository = repository.Root

			err := session.Post(walkthrough)

			assertRejected(t, err, review.RejectedEmptyChangeSet)
		})
	}
}

func assertDetailContains(t *testing.T, err error, want string) {
	t.Helper()
	var rejection *review.Rejection
	if !errors.As(err, &rejection) {
		t.Fatalf("expected a structured rejection, got %v", err)
	}
	if !strings.Contains(rejection.Detail, want) {
		t.Errorf("expected the rejection detail to mention %q, got %q", want, rejection.Detail)
	}
}

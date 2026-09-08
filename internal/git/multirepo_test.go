package git_test

import (
	"errors"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
	"github.com/probertson/diff-by-numbers/internal/workingtree"
)

// repoOn makes a repo whose default branch is named `branch`, with app.ts
// committed and a feature branch checked out ready for changes. Two repos with
// different default branches exercise the per-repository range of a Change Set.
func repoOn(t *testing.T, branch, initial string) string {
	t.Helper()
	root := t.TempDir()
	run(t, root, "init", "-q", "-b", branch)
	write(t, root, "app.ts", initial)
	run(t, root, "add", ".")
	run(t, root, "commit", "-qm", "initial")
	run(t, root, "checkout", "-q", "-b", "feature")
	return root
}

// TestAWalkthroughSpansTwoRepositoriesWithDifferentDefaults derives a Change Set
// across two repositories whose default branches differ, and drives it through a
// real review Session. dbn is never told a "session root" and runs git only in
// the roots the Change Set names — so the two repos need share no parent, and the
// test's own working directory is irrelevant.
func TestAWalkthroughSpansTwoRepositoriesWithDifferentDefaults(t *testing.T) {
	// Both repos have a file at the same relative path, so coverage can only pass
	// if it distinguishes them by repository.
	portal := repoOn(t, "main", "one\ntwo\n")
	write(t, portal, "app.ts", "one\ntwo\nthree\n") // adds new-side line 3

	service := repoOn(t, "trunk", "alpha\n")
	write(t, service, "app.ts", "alpha\nbeta\ngamma\n") // adds new-side lines 2, 3

	walkthrough := review.Walkthrough{
		Brief: review.Brief{
			Ask:        "Change both repositories at once",
			Approach:   "A Step per repository",
			Provenance: review.Provenance{Kind: review.ProvenanceStated, Citation: "session x"},
		},
		ChangeSet: review.ChangeSet{Repositories: []review.Repository{
			{Root: portal, Range: "main"},
			{Root: service, Range: "trunk"},
		}},
		Steps: []review.Step{
			{
				Name: "The portal change", Explanation: "portal app.ts gains a line",
				Excerpts: []review.Excerpt{{Repository: portal, File: "app.ts", Side: review.NewSide, FirstLine: 1, LastLine: 3}},
			},
			{
				Name: "The service change", Explanation: "service app.ts gains two lines",
				Excerpts: []review.Excerpt{{Repository: service, File: "app.ts", Side: review.NewSide, FirstLine: 1, LastLine: 3}},
			},
		},
	}

	session := review.NewSession(workingtree.NewResolver(), git.NewDeriver())

	if err := session.Post(walkthrough); err != nil {
		t.Fatalf("expected a two-repository Walkthrough to be accepted, got %v", err)
	}

	view := session.View()
	if len(view.Repositories) != 2 {
		t.Errorf("expected 2 repositories under review, got %d", len(view.Repositories))
	}
	// 1 changed line in the portal + 2 in the service, aggregated.
	if view.Coverage.Total != 3 {
		t.Errorf("expected coverage aggregated across both repositories to be 3, got %d", view.Coverage.Total)
	}
}

func TestCoverageDistinguishesSameNamedFilesAcrossRepositories(t *testing.T) {
	portal := repoOn(t, "main", "one\ntwo\n")
	write(t, portal, "app.ts", "one\ntwo\nthree\n")

	service := repoOn(t, "trunk", "alpha\n")
	write(t, service, "app.ts", "alpha\nbeta\n") // the service's app.ts also changed

	// Both Steps excerpt the *portal's* app.ts; the service's change is covered by
	// nothing, even though a same-named file was shown.
	walkthrough := review.Walkthrough{
		Brief: review.Brief{
			Ask: "x", Approach: "y",
			Provenance: review.Provenance{Kind: review.ProvenanceStated, Citation: "s"},
		},
		ChangeSet: review.ChangeSet{Repositories: []review.Repository{
			{Root: portal, Range: "main"},
			{Root: service, Range: "trunk"},
		}},
		Steps: []review.Step{{
			Name: "Only the portal", Explanation: "misses the service entirely",
			Excerpts: []review.Excerpt{{Repository: portal, File: "app.ts", Side: review.NewSide, FirstLine: 1, LastLine: 3}},
		}},
	}

	err := review.NewSession(workingtree.NewResolver(), git.NewDeriver()).Post(walkthrough)

	var rejection *review.Rejection
	if !errors.As(err, &rejection) || rejection.Reason != review.RejectedUncoveredChanges {
		t.Fatalf("expected the service's change to be uncovered, got %v", err)
	}
}

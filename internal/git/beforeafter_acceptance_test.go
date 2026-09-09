package git_test

import (
	"testing"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
	"github.com/probertson/diff-by-numbers/internal/workingtree"
)

// TestPointingAtTheAfterSideShowsTheBeforeEndToEnd drives the whole stack of #23:
// a real modification, a Walkthrough that points only at the after-side, and a
// Session whose resolver reads the before-side from git — proving the coverage
// ridealong and the unified render work against real git, not just fakes.
func TestPointingAtTheAfterSideShowsTheBeforeEndToEnd(t *testing.T) {
	root := newRepo(t) // app.ts = one/two/three committed on main
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\nTWO\nthree\n") // line 2 edited

	resolver := workingtree.NewResolver()
	session := review.NewSession(resolver, git.NewDeriver())
	walkthrough := review.Walkthrough{
		Brief: review.Brief{
			Ask:        "Rework line two",
			Approach:   "Point at the after-side; the before rides along",
			Provenance: review.Provenance{Kind: review.ProvenanceStated, Citation: "session xyz"},
		},
		ChangeSet: review.ChangeSet{Repositories: []review.Repository{{Root: root, Range: "main"}}},
		Steps: []review.Step{{
			Name:        "The edit",
			Explanation: "two became TWO",
			Excerpts: []review.Excerpt{
				{Repository: root, File: "app.ts", Side: review.NewSide, FirstLine: 1, LastLine: 3},
			},
		}},
	}

	// Pointing once at the after-side must account for the before-side it replaced.
	if err := session.Post(walkthrough); err != nil {
		t.Fatalf("expected the after-side plan to be accepted, got %v", err)
	}
	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}

	lines := session.View().Step.Excerpts[0].Lines
	var foundBefore, foundAfter bool
	for _, l := range lines {
		if l.Side == review.OldSide && l.Text == "two" {
			foundBefore = true
		}
		if l.Side == review.NewSide && l.Number == 2 && l.Text == "TWO" {
			foundAfter = true
		}
	}
	if !foundBefore {
		t.Errorf("expected the before-side \"two\" read from git to render, got %+v", lines)
	}
	if !foundAfter {
		t.Errorf("expected the after-side \"TWO\" to render, got %+v", lines)
	}
}

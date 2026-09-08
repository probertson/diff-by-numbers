package git_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
	"github.com/probertson/diff-by-numbers/internal/workingtree"
)

// TestEditingAFileMidReviewMakesItsStepStale drives real components — the git
// deriver, the working-tree resolver's hasher, and the review core — to confirm
// that editing a file after a Walkthrough is accepted turns its Step stale, while
// an untouched Step keeps rendering.
func TestEditingAFileMidReviewMakesItsStepStale(t *testing.T) {
	root := newRepo(t) // commits app.ts = "one\ntwo\nthree\n" on main
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\n") // new-side line 4
	write(t, root, "other.ts", "x\ny\n")
	run(t, root, "add", ".")

	walkthrough := review.Walkthrough{
		Brief: review.Brief{
			Ask: "x", Approach: "y",
			Provenance: review.Provenance{Kind: review.ProvenanceStated, Citation: "s"},
		},
		ChangeSet: review.ChangeSet{Repositories: []review.Repository{{Root: root, Range: "main"}}},
		Steps: []review.Step{
			{Name: "app", Explanation: "app change", Excerpts: []review.Excerpt{{Repository: root, File: "app.ts", Side: review.NewSide, FirstLine: 1, LastLine: 4}}},
			{Name: "other", Explanation: "other change", Excerpts: []review.Excerpt{{Repository: root, File: "other.ts", Side: review.NewSide, FirstLine: 1, LastLine: 2}}},
		},
	}

	session := review.NewSession(workingtree.NewResolver(), git.NewDeriver())
	if err := session.Post(walkthrough); err != nil {
		t.Fatalf("expected the Walkthrough to be accepted, got %v", err)
	}

	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}
	if session.View().Step.Stale {
		t.Fatal("expected a freshly-accepted Step to render")
	}

	// The Reviewer (or the agent) edits app.ts while the review is open.
	if err := os.WriteFile(filepath.Join(root, "app.ts"), []byte("one\ntwo\nthree\nFOUR EDITED\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}
	if !session.View().Step.Stale {
		t.Error("expected the app.ts Step to be stale after the file was edited")
	}

	if err := session.GoTo(2); err != nil {
		t.Fatal(err)
	}
	if session.View().Step.Stale {
		t.Error("expected the other.ts Step, untouched, to still render")
	}
}

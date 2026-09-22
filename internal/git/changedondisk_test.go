package git_test

import (
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
	"github.com/probertson/diff-by-numbers/internal/workingtree"
)

// TestEditingAFileMidReviewLeavesThePostedVersionOnScreen drives real components
// — the git deriver, the working-tree resolver and the review core — to confirm
// that a file edited after a Round is accepted goes on being shown, and
// anchored, as it was posted, with the edit flagged rather than hidden.
func TestEditingAFileMidReviewLeavesThePostedVersionOnScreen(t *testing.T) {
	root := newRepo(t) // commits app.ts = "one\ntwo\nthree\n" on main
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\n") // new-side line 4
	write(t, root, "other.ts", "x\ny\n")
	run(t, root, "add", ".")

	walkthrough := review.Round{
		Label: "LABEL-the-review",
		Brief: review.Brief{
			Goal: "x", Approach: "y",
		},
		ChangeSet: review.ChangeSet{Repositories: []review.Repository{{Root: root, Base: "main"}}},
		Steps: []review.Step{
			{Name: "app", Explanation: "app change", Excerpts: []review.Excerpt{{Repository: root, File: "app.ts", Side: review.NewSide, FirstLine: 1, LastLine: 4}}},
			{Name: "other", Explanation: "other change", Excerpts: []review.Excerpt{{Repository: root, File: "other.ts", Side: review.NewSide, FirstLine: 1, LastLine: 2}}},
		},
	}
	session := review.NewSession(workingtree.NewResolver(), git.NewDeriver())
	if err := session.Post(walkthrough); err != nil {
		t.Fatalf("expected the Round to be accepted, got %v", err)
	}

	// The agent edits app.ts while the review is open: line 4 is rewritten and a
	// line is inserted above it, so every number after line 1 has moved.
	write(t, root, "app.ts", "one\nINSERTED\ntwo\nthree\nFOUR EDITED\n")

	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}
	excerpt := session.View().Step.Excerpts[0]
	if !excerpt.ChangedOnDisk {
		t.Error("expected app.ts to be flagged as changed on disk")
	}
	if len(excerpt.Lines) != 4 || excerpt.Lines[3].Text != "four" {
		t.Errorf("expected the posted app.ts, ending \"four\", got %+v", excerpt.Lines)
	}

	anchor, err := session.Anchor(review.AnchorTarget{
		ExcerptIndex: 0,
		Start:        review.AnchorEndpoint{Side: review.NewSide, Line: 4},
		End:          review.AnchorEndpoint{Side: review.NewSide, Line: 4},
	})
	if err != nil {
		t.Fatalf("expected the edited file to be anchorable, got %v", err)
	}
	rendered := anchor.Render()
	if !strings.Contains(rendered, "4 | four") {
		t.Errorf("expected the Anchor to quote the posted line 4, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "line numbers are from the posted version") {
		t.Errorf("expected the Anchor to say its numbers are from the posted version, got:\n%s", rendered)
	}

	if err := session.GoTo(2); err != nil {
		t.Fatal(err)
	}
	if session.View().Step.Excerpts[0].ChangedOnDisk {
		t.Error("expected other.ts, untouched, not to be flagged")
	}
}

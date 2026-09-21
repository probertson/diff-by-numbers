package git_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
	"github.com/probertson/diff-by-numbers/internal/workingtree"
)

// The deriver attributes every atom of a renamed file to its new path, so an
// agent naming the old path — the natural thing to write about a move — covers
// nothing. The core aliases one to the other, which needs git to say which
// moves it found.

// mkdir makes a directory git mv can move into; git will not create one.
func mkdir(t *testing.T, root, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestAPureRenameIsRecordedAsSourceToDestination(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	mkdir(t, root, "src")
	run(t, root, "mv", "app.ts", "src/app.ts")

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Base: "main"})

	if err != nil {
		t.Fatal(err)
	}
	if got := d.Renames["app.ts"]; got != "src/app.ts" {
		t.Errorf("expected app.ts to be recorded as renamed to src/app.ts, got %q", got)
	}
}

func TestARenameWithEditsIsRecordedToo(t *testing.T) {
	// A renamed-and-edited file has hunks, so it is not an Opaque Change and
	// nothing else in the derivation would ever mention the path it came from.
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	mkdir(t, root, "src")
	run(t, root, "mv", "app.ts", "src/app.ts")
	write(t, root, "src/app.ts", "one\ntwo\nthree\nfour\n")

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Base: "main"})

	if err != nil {
		t.Fatal(err)
	}
	if got := d.Renames["app.ts"]; got != "src/app.ts" {
		t.Errorf("expected the renamed-and-edited file to be recorded, got %q", got)
	}
	if got := linesOn(d.Lines, "src/app.ts", review.NewSide); len(got) == 0 {
		t.Errorf("expected the edit to derive Changed Lines under the new path, got %v", got)
	}
}

func TestAFileThatOnlyChangedIsNotRecordedAsARename(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\n")

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Base: "main"})

	if err != nil {
		t.Fatal(err)
	}
	if len(d.Renames) != 0 {
		t.Errorf("expected no renames, got %v", d.Renames)
	}
}

// TestAnOldSideExcerptNamingTheOldPathRendersTheBeforeSide is #86's headline
// scenario through real git and a real working-tree resolver: a file moved and
// edited in one branch, written about under the path it came from.
func TestAnOldSideExcerptNamingTheOldPathRendersTheBeforeSide(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	mkdir(t, root, "src")
	run(t, root, "mv", "app.ts", "src/app.ts")
	write(t, root, "src/app.ts", "one\nthree\n") // the middle line goes
	run(t, root, "add", "-A")

	session := review.NewSession(workingtree.NewResolver(), git.NewDeriver())
	err := session.Post(review.Walkthrough{
		Brief: review.Brief{
			Ask:        "Move the fetch layer under src/ and drop the dead line",
			Approach:   "One move, one deletion",
			Provenance: review.Provenance{Kind: review.ProvenanceStated, Citation: "session abc"},
		},
		ChangeSet: review.ChangeSet{Repositories: []review.Repository{{Root: root, Base: "main"}}},
		Steps: []review.Step{{
			Name:        "Drop the middle line",
			Explanation: "It was never read",
			Excerpts: []review.Excerpt{
				{Repository: root, File: "app.ts", Side: review.OldSide, FirstLine: 2, LastLine: 2},
			},
		}},
	})

	if err != nil {
		t.Fatalf("expected the old path to be accepted as an alias, got %v", err)
	}
	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}
	shown := session.View().Step.Excerpts[0]
	if shown.Problem != "" {
		t.Fatalf("expected the before-side to render, got problem %q", shown.Problem)
	}
	if len(shown.Lines) != 1 || shown.Lines[0].Text != "two" {
		t.Errorf("expected the deleted line \"two\" to be rendered, got %+v", shown.Lines)
	}
}

func TestARenameSourceContainingTheHeadersSeparatorIsRecordedWhole(t *testing.T) {
	// "diff --git a/OLD b/NEW" is split on the first " b/", so a source path that
	// contains that sequence is cut in the wrong place. The source is what an
	// agent writes about, so a mangled one means the alias never matches and the
	// Acknowledgement is refused for claiming a file with no changes.
	root := newRepo(t)
	mkdir(t, root, "a b")
	write(t, root, "a b/fetch.ts", "one\ntwo\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-qm", "add the awkward path")
	run(t, root, "checkout", "-q", "-b", "feature")
	mkdir(t, root, "src")
	run(t, root, "mv", "a b/fetch.ts", "src/fetch.ts")

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Base: "main"})

	if err != nil {
		t.Fatal(err)
	}
	if got := d.Renames["a b/fetch.ts"]; got != "src/fetch.ts" {
		t.Errorf("expected the whole source path to key the rename, got %v", d.Renames)
	}
}

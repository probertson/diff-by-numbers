package git_test

import (
	"testing"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
)

func corrByNewFirst(all []review.Correspondence, newFirst int) (review.Correspondence, bool) {
	for _, c := range all {
		if c.NewFirst == newFirst {
			return c, true
		}
	}
	return review.Correspondence{}, false
}

func corrByOldFirst(all []review.Correspondence, oldFirst int) (review.Correspondence, bool) {
	for _, c := range all {
		if c.OldFirst == oldFirst {
			return c, true
		}
	}
	return review.Correspondence{}, false
}

func TestDerivesCrossSideCorrespondenceForEditsAndAdditions(t *testing.T) {
	root := newRepo(t) // app.ts = one/two/three on main
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\nTWO\nthree\nfour\n") // line 2 edited, line 4 added

	derivation, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "main"})
	if err != nil {
		t.Fatalf("derivation failed: %v", err)
	}

	mod, ok := corrByNewFirst(derivation.Correspondences, 2)
	if !ok {
		t.Fatalf("expected a correspondence for the edit at new line 2, got %+v", derivation.Correspondences)
	}
	if mod.OldFirst != 2 || mod.OldLast != 2 || mod.NewLast != 2 {
		t.Errorf("expected old 2->new 2 for the edit, got %+v", mod)
	}

	add, ok := corrByNewFirst(derivation.Correspondences, 4)
	if !ok {
		t.Fatalf("expected a correspondence for the addition at new line 4, got %+v", derivation.Correspondences)
	}
	if add.OldFirst != 0 {
		t.Errorf("expected the addition to carry no before-side, got %+v", add)
	}
}

func TestDerivesADeletionAsAnOldOnlyCorrespondence(t *testing.T) {
	root := newRepo(t) // app.ts = one/two/three
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\nthree\n") // line 2 removed

	derivation, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "main"})
	if err != nil {
		t.Fatalf("derivation failed: %v", err)
	}

	del, ok := corrByOldFirst(derivation.Correspondences, 2)
	if !ok {
		t.Fatalf("expected a correspondence for the removal at old line 2, got %+v", derivation.Correspondences)
	}
	if del.NewFirst != 0 {
		t.Errorf("expected the removal to carry no after-side, got %+v", del)
	}
}

func TestDerivesCorrespondenceForARenamedAndEditedFile(t *testing.T) {
	root := newRepo(t) // app.ts = one/two/three
	run(t, root, "checkout", "-q", "-b", "feature")
	run(t, root, "mv", "app.ts", "renamed.ts")
	write(t, root, "renamed.ts", "one\nTWO\nthree\n") // renamed and line 2 edited

	derivation, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "main"})
	if err != nil {
		t.Fatalf("derivation failed: %v", err)
	}

	// The edit is line-represented under the new path, not an Opaque rename.
	if _, ok := opaqueFor(derivation.Opaque, "renamed.ts"); ok {
		t.Error("a renamed-and-edited file should derive lines, not an Opaque Change")
	}
	if got := linesOn(derivation.Lines, "renamed.ts", review.NewSide); len(got) != 1 || got[0] != 2 {
		t.Errorf("expected new line 2 under the new path, got %v", got)
	}
	mod, ok := corrByNewFirst(derivation.Correspondences, 2)
	if !ok || mod.File != "renamed.ts" || mod.OldFirst != 2 {
		t.Errorf("expected an edit correspondence on renamed.ts pairing old 2 -> new 2, got %+v", derivation.Correspondences)
	}
}

func TestDerivesACorrespondencePerHunkInAMultiHunkFile(t *testing.T) {
	root := newRepo(t)
	write(t, root, "app.ts", "1\n2\n3\n4\n5\n6\n")
	run(t, root, "add", ".")
	run(t, root, "commit", "-qm", "six lines")
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "1\nTWO\n3\n4\nFIVE\n6\n") // two separate edits

	derivation, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "main"})
	if err != nil {
		t.Fatalf("derivation failed: %v", err)
	}

	if _, ok := corrByNewFirst(derivation.Correspondences, 2); !ok {
		t.Errorf("expected a correspondence for the edit at line 2, got %+v", derivation.Correspondences)
	}
	if _, ok := corrByNewFirst(derivation.Correspondences, 5); !ok {
		t.Errorf("expected a separate correspondence for the edit at line 5, got %+v", derivation.Correspondences)
	}
}

func TestReadsTheBeforeSideAtTheMergeBase(t *testing.T) {
	root := newRepo(t) // app.ts committed as one/two/three on main
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\nCHANGED\nthree\n") // working-tree edit, not committed

	lines, err := git.ReadBefore(root, "main", "app.ts", 2, 2)

	if err != nil {
		t.Fatalf("expected to read the before-side, got %v", err)
	}
	if len(lines) != 1 || lines[0].Text != "two" {
		t.Errorf("expected the before-side of line 2 to be the committed \"two\", got %+v", lines)
	}
}

func TestReadsTheBeforeSideFollowingARename(t *testing.T) {
	root := newRepo(t) // app.ts = one/two/three
	run(t, root, "checkout", "-q", "-b", "feature")
	run(t, root, "mv", "app.ts", "renamed.ts")
	write(t, root, "renamed.ts", "one\nCHANGED\nthree\n") // edited after the rename

	// The before-side of the new path lives at the old path in git history.
	lines, err := git.ReadBefore(root, "main", "renamed.ts", 2, 2)

	if err != nil {
		t.Fatalf("expected to read the before-side across a rename, got %v", err)
	}
	if len(lines) != 1 || lines[0].Text != "two" {
		t.Errorf("expected the before-side of renamed.ts:2 read from app.ts to be \"two\", got %+v", lines)
	}
}

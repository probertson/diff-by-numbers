package git_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
)

// snapshotOf captures the working tree and returns the tree id.
func snapshotOf(t *testing.T, root string) string {
	t.Helper()
	tree, err := git.NewDeriver().Snapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func mapping(t *testing.T, root, from, to string) review.RoundMapping {
	t.Helper()
	m, err := git.NewDeriver().MapBetween(root, from, to)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// The case content-identity pre-marking could never handle: a line whose text
// repeats. Position tells them apart exactly.
func TestAnUntouchedLineMapsBackToItsOldPositionEvenWhenItsTextRepeats(t *testing.T) {
	root := newRepo(t)
	write(t, root, "app.ts", "a {\n}\nb {\n}\n")
	from := snapshotOf(t, root)
	write(t, root, "app.ts", "a {\n}\nNEW\nb {\n}\n") // one line inserted at 3
	to := snapshotOf(t, root)

	m := mapping(t, root, from, to)

	// Line 5 in the new tree is the second `}`, which was line 4 before.
	at, ok := m.Lookup("app.ts", 5)
	if !ok {
		t.Fatalf("the trailing `}` was not touched, so it should map back")
	}
	if at.File != "app.ts" || at.Line != 4 {
		t.Errorf("expected app.ts:4, got %s:%d", at.File, at.Line)
	}
}

func TestATouchedLineDoesNotMapBack(t *testing.T) {
	root := newRepo(t)
	write(t, root, "app.ts", "a {\n}\nb {\n}\n")
	from := snapshotOf(t, root)
	write(t, root, "app.ts", "a {\n}\nNEW\nb {\n}\n")
	to := snapshotOf(t, root)

	m := mapping(t, root, from, to)

	if at, ok := m.Lookup("app.ts", 3); ok {
		t.Errorf("the inserted line is new and must be demanded, but mapped to %s:%d", at.File, at.Line)
	}
}

func TestAnUnchangedFileMapsLineForLine(t *testing.T) {
	root := newRepo(t)
	write(t, root, "app.ts", "one\ntwo\nthree\n")
	from := snapshotOf(t, root)
	write(t, root, "other.ts", "unrelated\n")
	to := snapshotOf(t, root)

	m := mapping(t, root, from, to)

	for line := 1; line <= 3; line++ {
		at, ok := m.Lookup("app.ts", line)
		if !ok || at.Line != line {
			t.Errorf("app.ts:%d should map to itself, got %s:%d ok=%v", line, at.File, at.Line, ok)
		}
	}
}

func TestALineInAFileRenamedBetweenRoundsMapsThroughTheRename(t *testing.T) {
	root := newRepo(t)
	write(t, root, "app.ts", "one\ntwo\nthree\n")
	from := snapshotOf(t, root)
	move(t, root, "app.ts", "renamed.ts")
	to := snapshotOf(t, root)

	m := mapping(t, root, from, to)

	at, ok := m.Lookup("renamed.ts", 2)
	if !ok {
		t.Fatal("a moved but unedited line should still map back")
	}
	if at.File != "app.ts" || at.Line != 2 {
		t.Errorf("expected app.ts:2 through the rename, got %s:%d", at.File, at.Line)
	}
}

func TestEveryLineOfAFileThatIsNewSinceTheOlderTreeIsDemanded(t *testing.T) {
	root := newRepo(t)
	from := snapshotOf(t, root)
	write(t, root, "brand-new.ts", "one\ntwo\n")
	to := snapshotOf(t, root)

	m := mapping(t, root, from, to)

	for line := 1; line <= 2; line++ {
		if at, ok := m.Lookup("brand-new.ts", line); ok {
			t.Errorf("a wholly new file has nothing to map back to, got %s:%d", at.File, at.Line)
		}
	}
}

// Deletions shift everything below them, and a pure deletion adds no new-side
// lines of its own — so the lines after it must still find their old numbers.
func TestLinesBelowADeletionMapBackToTheirHigherOldNumbers(t *testing.T) {
	root := newRepo(t)
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\n")
	from := snapshotOf(t, root)
	write(t, root, "app.ts", "one\nfour\n") // two and three removed
	to := snapshotOf(t, root)

	m := mapping(t, root, from, to)

	at, ok := m.Lookup("app.ts", 2)
	if !ok {
		t.Fatal("`four` was not touched, so it should map back")
	}
	if at.Line != 4 {
		t.Errorf("`four` was line 4 before the deletion, got line %d", at.Line)
	}
}

// The line immediately above a deletion did not move, and must map to itself.
// Getting this wrong is not merely imprecise: it maps that line onto one of the
// lines that was *deleted*, which is exactly the kind of line the previous round
// showed — so a line the Reviewer never saw could be pre-marked as already read.
func TestTheLineAboveADeletionMapsToItself(t *testing.T) {
	root := newRepo(t)
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\n")
	from := snapshotOf(t, root)
	write(t, root, "app.ts", "one\nfour\n") // two and three removed
	to := snapshotOf(t, root)

	m := mapping(t, root, from, to)

	at, ok := m.Lookup("app.ts", 1)
	if !ok {
		t.Fatal("`one` was not touched, so it should map back")
	}
	if at.Line != 1 {
		t.Errorf("`one` sits above the deletion and did not move; got line %d", at.Line)
	}
}

// The edits between two rounds are what a Revision Round shows the Reviewer when
// it shades a Step by what changed since the last round (#44): each one says which
// lines of the earlier tree it replaced, and with which lines of the later one.
func TestTheMappingListsEachEditBetweenTheTrees(t *testing.T) {
	root := newRepo(t)
	write(t, root, "app.ts", "a\nb\nc\nd\ne\n")
	from := snapshotOf(t, root)
	write(t, root, "app.ts", "a\nB\nc\ne\nf\n") // b rewritten, d withdrawn, f added
	to := snapshotOf(t, root)

	edits := mapping(t, root, from, to).Edits("app.ts")

	want := []review.RoundEdit{
		{OldFirst: 2, OldCount: 1, NewFirst: 2, NewCount: 1},
		{OldFirst: 4, OldCount: 1, NewFirst: 3, NewCount: 0},
		{OldFirst: 5, OldCount: 0, NewFirst: 5, NewCount: 1},
	}
	if len(edits) != len(want) {
		t.Fatalf("expected %d edits, got %+v", len(want), edits)
	}
	for i := range want {
		if edits[i] != want[i] {
			t.Errorf("edit %d: expected %+v, got %+v", i, want[i], edits[i])
		}
	}
}

func TestAnUntouchedFileHasNoEdits(t *testing.T) {
	root := newRepo(t)
	from := snapshotOf(t, root)
	to := snapshotOf(t, root)

	edits := mapping(t, root, from, to).Edits("app.ts")

	if len(edits) != 0 {
		t.Errorf("expected no edits, got %+v", edits)
	}
}

// A changed line whose own text begins "++ " reaches the diff as "+++ …", the
// shape of a file header. Read as one, it would retarget every later hunk of the
// file to a path that does not exist.
func TestAChangedLineShapedLikeAFileHeaderDoesNotRetargetTheMapping(t *testing.T) {
	root := newRepo(t)
	write(t, root, "doc.md", "one\ntwo\nthree\nfour\n")
	from := snapshotOf(t, root)
	write(t, root, "doc.md", "one\n++ b/ghost\nthree\nFOUR\n")
	to := snapshotOf(t, root)

	m := mapping(t, root, from, to)

	if _, ok := m.Lookup("doc.md", 4); ok {
		t.Error("expected doc.md line 4, edited, not to map back")
	}
	if m.Touched("ghost") {
		t.Error("a line's text must not be read as a file header")
	}
}

func TestTheMappingNamesAFileTheLaterTreeNoLongerHas(t *testing.T) {
	root := newRepo(t)
	write(t, root, "scratch.ts", "x\ny\n")
	from := snapshotOf(t, root)
	if err := os.Remove(filepath.Join(root, "scratch.ts")); err != nil {
		t.Fatal(err)
	}
	to := snapshotOf(t, root)

	m := mapping(t, root, from, to)

	if files := m.Files(); len(files) != 1 || files[0] != "scratch.ts" {
		t.Fatalf("expected scratch.ts named, got %v", files)
	}
	edits := m.Edits("scratch.ts")
	if len(edits) != 1 || edits[0] != (review.RoundEdit{OldFirst: 1, OldCount: 2, NewFirst: 0, NewCount: 0}) {
		t.Errorf("expected both lines withdrawn from the top, got %+v", edits)
	}
}

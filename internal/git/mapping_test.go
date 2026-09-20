package git_test

import (
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

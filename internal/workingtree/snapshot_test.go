package workingtree_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
	"github.com/probertson/diff-by-numbers/internal/workingtree"
)

// snapshotted makes a repository holding one file and takes a Round Snapshot of
// it, returning the root and the Round that snapshot belongs to.
func snapshotted(t *testing.T, file, content string) (string, review.RoundSource) {
	t.Helper()
	root := t.TempDir()
	// A snapshot is written through a copy of the index, so the repository needs
	// one — which any repository with a base to review against has.
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"commit", "-q", "--allow-empty", "-m", "base"},
		{"read-tree", "HEAD"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	writeFile(t, root, file, content)
	tree, err := git.NewDeriver().Snapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, review.RoundSource{Snapshots: map[string]string{root: tree}}
}

func TestTheNewSideIsReadFromTheRoundSnapshotNotTheWorkingTree(t *testing.T) {
	root, round := snapshotted(t, "src/fetch.ts", "one\ntwo\nthree\n")
	writeFile(t, root, "src/fetch.ts", "ONE\nTWO\n") // edited after the post
	resolver := workingtree.NewResolver()

	lines, err := resolver.Resolve(review.Excerpt{
		Repository: root, File: "src/fetch.ts", Side: review.NewSide, FirstLine: 2, LastLine: 3,
	}, round)

	if err != nil {
		t.Fatalf("expected the posted range to resolve, got %v", err)
	}
	if len(lines) != 2 || lines[0].Text != "two" || lines[1].Text != "three" {
		t.Errorf("expected the posted lines 2-3, got %+v", lines)
	}
}

func TestAFileIsChangedOnDiskOnlyOnceItDiffersFromItsSnapshot(t *testing.T) {
	root, round := snapshotted(t, "src/fetch.ts", "one\ntwo\n")
	resolver := workingtree.NewResolver()
	tree := round.Snapshots[root]

	if resolver.ChangedOnDisk(root, tree, "src/fetch.ts") {
		t.Fatal("a file identical to its snapshot has not changed")
	}

	writeFile(t, root, "src/fetch.ts", "one\ntwo\nthree\n")

	if !resolver.ChangedOnDisk(root, tree, "src/fetch.ts") {
		t.Error("expected an edited file to have changed on disk")
	}
}

func TestADeletedFileHasChangedOnDisk(t *testing.T) {
	root, round := snapshotted(t, "src/fetch.ts", "one\n")
	resolver := workingtree.NewResolver()
	if err := os.Remove(filepath.Join(root, "src/fetch.ts")); err != nil {
		t.Fatal(err)
	}

	changed := resolver.ChangedOnDisk(root, round.Snapshots[root], "src/fetch.ts")

	if !changed {
		t.Error("expected a file deleted since the snapshot to have changed on disk")
	}
}

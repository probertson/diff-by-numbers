package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRepo makes a repo on branch main with an initial commit, and returns its root.
func newRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	run(t, root, "init", "-q", "-b", "main")
	write(t, root, "app.ts", "one\ntwo\nthree\n")
	run(t, root, "add", ".")
	run(t, root, "commit", "-qm", "initial")
	return root
}

func linesOn(all []review.ChangedLine, file string, side review.Side) []int {
	var out []int
	for _, l := range all {
		if l.File == file && l.Side == side {
			out = append(out, l.Line)
		}
	}
	return out
}

func TestDerivesAddedLinesFromAFeatureBranchPlusWorkingTree(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	// A committed change on the branch.
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\n")
	run(t, root, "add", ".")
	run(t, root, "commit", "-qm", "add four")
	// An unstaged working-tree change on top.
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\nfive\n")

	lines, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "main"})
	if err != nil {
		t.Fatal(err)
	}

	added := linesOn(lines, "app.ts", review.NewSide)
	if len(added) != 2 || added[0] != 4 || added[1] != 5 {
		t.Errorf("expected added lines 4 and 5 (branch + working tree), got %v", added)
	}
	if l := linesOn(lines, "app.ts", review.OldSide); len(l) != 0 {
		t.Errorf("expected no deletions, got old-side %v", l)
	}
}

func TestDerivesDeletedLinesOnTheOldSide(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\nthree\n") // deleted "two" (old line 2)

	lines, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "main"})
	if err != nil {
		t.Fatal(err)
	}

	deleted := linesOn(lines, "app.ts", review.OldSide)
	if len(deleted) != 1 || deleted[0] != 2 {
		t.Errorf("expected deleted old-side line 2, got %v", deleted)
	}
}

func TestDerivesAStagedNewFile(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "src/added.ts", "alpha\nbeta\n")
	run(t, root, "add", ".") // staged, not committed

	lines, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "main"})
	if err != nil {
		t.Fatal(err)
	}

	added := linesOn(lines, "src/added.ts", review.NewSide)
	if len(added) != 2 || added[0] != 1 || added[1] != 2 {
		t.Errorf("expected staged new file lines 1 and 2, got %v", added)
	}
}

func TestNoChangesYieldsNoChangedLines(t *testing.T) {
	root := newRepo(t)

	lines, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "main"})
	if err != nil {
		t.Fatal(err)
	}

	if len(lines) != 0 {
		t.Errorf("expected no changed lines against the same branch, got %v", lines)
	}
}

func TestABadRangeIsAnError(t *testing.T) {
	root := newRepo(t)

	_, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "no-such-branch"})

	if err == nil {
		t.Fatal("expected an error for a range that does not resolve")
	}
}

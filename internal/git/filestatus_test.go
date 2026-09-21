package git_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
)

// A file git reports as new or as gone is recorded as such. Which sides of a file
// carry lines cannot say this: a file that only gained lines is still modified.
func TestTheDerivationNamesTheFilesAddedAndDeleted(t *testing.T) {
	root := newRepo(t) // app.ts
	write(t, root, "gone.ts", "a\nb\n")
	run(t, root, "add", ".")
	run(t, root, "commit", "-qm", "gone.ts")
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\n") // only gains a line
	write(t, root, "staged.ts", "x\n")
	run(t, root, "add", "staged.ts")
	write(t, root, "untracked.ts", "y\n")
	if err := os.Remove(filepath.Join(root, "gone.ts")); err != nil {
		t.Fatal(err)
	}

	derivation, err := git.NewDeriver().Derive(review.Repository{Root: root, Base: "main"})

	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(derivation.Added)
	if !slices.Equal(derivation.Added, []string{"staged.ts", "untracked.ts"}) {
		t.Errorf("expected staged.ts and untracked.ts added, got %v", derivation.Added)
	}
	if !slices.Equal(derivation.Deleted, []string{"gone.ts"}) {
		t.Errorf("expected gone.ts deleted, got %v", derivation.Deleted)
	}
}

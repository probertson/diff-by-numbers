package workingtree_test

import (
	"os/exec"
	"reflect"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
	"github.com/probertson/diff-by-numbers/internal/workingtree"
)

// A Document's blocks are parsed from the whole file, so the resolver reads one
// side of a whole file from the same place it reads an Excerpt of it.

func TestReadsAWholeFileAsTheRoundSnapshotHoldsIt(t *testing.T) {
	root, round := snapshotted(t, "docs/retries.md", "# Retries\n\nWhen to retry.\n")
	writeFile(t, root, "docs/retries.md", "# Edited after the post\n")

	lines, err := workingtree.NewResolver().ReadFile(root, "docs/retries.md", review.NewSide, round)

	if err != nil {
		t.Fatalf("expected the posted file to be read, got %v", err)
	}
	if want := []string{"# Retries", "", "When to retry."}; !reflect.DeepEqual(lines, want) {
		t.Errorf("expected the posted file %q, got %q", want, lines)
	}
}

func TestReadsAWholeFileFromDiskWhenTheRepositoryHasNoSnapshot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "notes.md", "first\nlast-no-newline")

	lines, err := workingtree.NewResolver().ReadFile(root, "notes.md", review.NewSide, review.RoundSource{})

	if err != nil {
		t.Fatalf("expected the file to be read, got %v", err)
	}
	if want := []string{"first", "last-no-newline"}; !reflect.DeepEqual(lines, want) {
		t.Errorf("expected %q, got %q", want, lines)
	}
}

func TestReadsTheWholeBeforeSideAtTheMergeBase(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/retired.md", "# Retired\n\nNo longer needed.\n")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"add", "."},
		{"commit", "-q", "-m", "base"},
		{"rm", "-q", "docs/retired.md"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	round := review.RoundSource{Bases: map[string]string{root: "HEAD"}}

	lines, err := workingtree.NewResolver().ReadFile(root, "docs/retired.md", review.OldSide, round)

	if err != nil {
		t.Fatalf("expected the before-side to be read, got %v", err)
	}
	if want := []string{"# Retired", "", "No longer needed."}; !reflect.DeepEqual(lines, want) {
		t.Errorf("expected %q, got %q", want, lines)
	}
}

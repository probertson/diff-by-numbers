package git_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
)

// A file rewritten at the same size in the same instant its index entry was
// recorded is "racily clean": its stat data cannot tell the rewrite apart, and
// git only notices it because the entry is no older than the index file itself.
// The derivation diffs through a copy of the index, so the copy must keep the
// real one's timestamp, or git trusts the stale stat data and the change is left
// out of the Change Set entirely. (The copy is only made when there are
// untracked files to add to it, hence the one below.)
func TestARacilyCleanRewriteIsStillDerived(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	path := filepath.Join(root, "app.ts")
	then := time.Now().Add(-10 * time.Second).Truncate(time.Second)
	write(t, root, "app.ts", "one\ntwo\nthree\n") // as committed, re-recorded
	if err := os.Chtimes(path, then, then); err != nil {
		t.Fatal(err)
	}
	run(t, root, "add", "app.ts")
	if err := os.Chtimes(filepath.Join(root, ".git", "index"), then, then); err != nil {
		t.Fatal(err)
	}
	write(t, root, "app.ts", "one\ntwo\nthrEE\n") // same size, same mtime
	if err := os.Chtimes(path, then, then); err != nil {
		t.Fatal(err)
	}
	// An untracked file is what sends the derivation through the index copy.
	write(t, root, "untracked.ts", "u\n")

	derivation, err := git.NewDeriver().Derive(review.Repository{Root: root, Base: "main"})

	if err != nil {
		t.Fatal(err)
	}
	var got []int
	for _, line := range derivation.Lines {
		if line.File == "app.ts" && line.Side == review.NewSide {
			got = append(got, line.Line)
		}
	}
	if len(got) != 1 || got[0] != 3 {
		t.Errorf("expected app.ts line 3 derived as changed, got %v", got)
	}
}

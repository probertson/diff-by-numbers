package git_test

import (
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
)

// A snapshot is how a Revision Round knows what the previous round actually
// showed. Content-identity pre-marking could not tell one `}` from another;
// a tree object records exactly where every line stood.
func TestASnapshotCapturesTheWorkingTreeIncludingUncommittedWork(t *testing.T) {
	root := newRepo(t)
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\n") // uncommitted

	tree, err := git.NewDeriver().Snapshot(root)
	if err != nil {
		t.Fatal(err)
	}

	if got := output(t, root, "show", tree+":app.ts"); got != "one\ntwo\nthree\nfour\n" {
		t.Errorf("the snapshot should hold the working tree's app.ts, got %q", got)
	}
}

// Untracked files are part of the Change Set (#79), so they have to be in the
// snapshot too — otherwise a new file would look withdrawn in the next round.
func TestASnapshotIncludesUntrackedFilesAndSkipsIgnoredOnes(t *testing.T) {
	root := newRepo(t)
	write(t, root, ".gitignore", "ignored.txt\n")
	run(t, root, "add", ".gitignore")
	run(t, root, "commit", "-qm", "ignore")
	write(t, root, "notes.md", "alpha\n")
	write(t, root, "ignored.txt", "noise\n")

	tree, err := git.NewDeriver().Snapshot(root)
	if err != nil {
		t.Fatal(err)
	}

	listed := output(t, root, "ls-tree", "-r", "--name-only", tree)
	if !strings.Contains(listed, "notes.md") {
		t.Errorf("an untracked file belongs in the snapshot, got:\n%s", listed)
	}
	if strings.Contains(listed, "ignored.txt") {
		t.Errorf("an ignored file does not belong in the snapshot, got:\n%s", listed)
	}
}

func TestSnapshottingLeavesTheRealIndexByteForByteUnchanged(t *testing.T) {
	root := newRepo(t)
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\n")
	write(t, root, "notes.md", "alpha\n")
	before := readIndex(t, root)

	if _, err := git.NewDeriver().Snapshot(root); err != nil {
		t.Fatal(err)
	}

	if after := readIndex(t, root); string(after) != string(before) {
		t.Errorf("the real index changed: %d bytes before, %d after", len(before), len(after))
	}
	if status := output(t, root, "status", "--porcelain"); !strings.Contains(status, "?? notes.md") {
		t.Errorf("notes.md should still be untracked, git status says:\n%s", status)
	}
}

// A Revision Round compares this round's merge-base with the previous one's to
// tell whether the branch was rebased underneath the review, so the derivation
// has to report which base it actually resolved.
func TestADerivationReportsTheMergeBaseItResolved(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\n")

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Base: "main"})
	if err != nil {
		t.Fatal(err)
	}

	want := strings.TrimSpace(output(t, root, "merge-base", "main", "HEAD"))
	if d.Base != want {
		t.Errorf("Base = %q, want the merge-base %q", d.Base, want)
	}
}

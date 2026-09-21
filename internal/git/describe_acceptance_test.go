package git_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
	"github.com/probertson/diff-by-numbers/internal/workingtree"
)

// TestDescribeChangesMatchesARealRepository drives the real deriver through the
// core's description, over one of each kind of change git can report.
func TestDescribeChangesMatchesARealRepository(t *testing.T) {
	root := newRepo(t) // app.ts = one two three
	write(t, root, "gone.ts", "a\nb\n")
	write(t, root, "plain.ts", "p1\np2\np3\np4\np5\np6\np7\np8\n")
	write(t, root, "edited.ts", "e1\ne2\ne3\ne4\ne5\ne6\ne7\ne8\n")
	if err := os.WriteFile(filepath.Join(root, "logo.bin"), []byte{0, 1, 2, 0}, 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, root, "add", ".")
	run(t, root, "commit", "-qm", "more")
	run(t, root, "checkout", "-q", "-b", "feature")

	write(t, root, "app.ts", "one\nTWO\nthree\nfour\n")                // line 2 rewritten, line 4 added
	run(t, root, "mv", "plain.ts", "renamed.ts")                       // renamed, no edits
	run(t, root, "mv", "edited.ts", "rewritten.ts")                    // renamed, and ...
	write(t, root, "rewritten.ts", "e1\ne2\ne3\ne4\ne5\ne6\ne7\nE8\n") // ... with an edit
	run(t, root, "rm", "-q", "gone.ts")
	write(t, root, "new.ts", "n1\nn2\n") // untracked
	if err := os.WriteFile(filepath.Join(root, "logo.bin"), []byte{0, 9, 9, 0}, 0o644); err != nil {
		t.Fatal(err)
	}
	session := review.NewSession(workingtree.NewResolver(), git.NewDeriver())

	d, err := session.DescribeChanges(review.ChangeSet{Repositories: []review.Repository{{Root: root, Base: "main"}}})

	if err != nil {
		t.Fatal(err)
	}
	r := func(first, last int) review.LineRange { return review.LineRange{First: first, Last: last} }
	want := []review.FileDescription{
		{Path: "app.ts", Status: review.FileModified,
			NewRanges: []review.LineRange{r(2, 2), r(4, 4)}, OldRanges: []review.LineRange{r(2, 2)},
			Modifications: []review.Modification{{Old: r(2, 2), New: r(2, 2)}}},
		{Path: "gone.ts", Status: review.FileDeleted, OldRanges: []review.LineRange{r(1, 2)}},
		{Path: "logo.bin", Status: review.FileBinary},
		{Path: "new.ts", Status: review.FileAdded, NewRanges: []review.LineRange{r(1, 2)}},
		{Path: "renamed.ts", Status: review.FileRenamed, From: "plain.ts"},
		{Path: "rewritten.ts", Status: review.FileRenamed, From: "edited.ts",
			NewRanges: []review.LineRange{r(8, 8)}, OldRanges: []review.LineRange{r(8, 8)},
			Modifications: []review.Modification{{Old: r(8, 8), New: r(8, 8)}}},
	}
	if len(d.Repositories) != 1 {
		t.Fatalf("expected one repository, got %+v", d.Repositories)
	}
	got := d.Repositories[0].Files
	if len(got) != len(want) {
		t.Fatalf("expected %d files, got %+v", len(want), got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("file %d:\nwant %+v\ngot  %+v", i, want[i], got[i])
		}
	}
}

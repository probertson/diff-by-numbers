package git_test

import (
	"reflect"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
	"github.com/probertson/diff-by-numbers/internal/workingtree"
)

// The core absorbs a blank Changed Line into the Excerpt beside it, but it never
// reads a file: git has the text, so git is what says which Changed Lines hold
// nothing but whitespace.

func TestBlankLinesAreNamedAsWhitespaceOnTheNewSide(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	// Three sections separated by an empty line and by a line holding only a
	// tab, which reads as blank but is not empty.
	write(t, root, "section.md", "alpha\n\nbeta\n\t\ngamma\n")
	run(t, root, "add", "section.md")

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Base: "main"})

	if err != nil {
		t.Fatal(err)
	}
	if got, want := linesOn(d.Whitespace, "section.md", review.NewSide), []int{2, 4}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected the blank separators %v to be named as whitespace, got %v", want, got)
	}
	if got, want := linesOn(d.Lines, "section.md", review.NewSide), []int{1, 2, 3, 4, 5}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected every line of the new file to be a Changed Line, got %v", got)
	}
}

func TestADeletedBlankLineIsNamedOnTheOldSide(t *testing.T) {
	root := newRepo(t)
	write(t, root, "notes.md", "alpha\n\nbeta\n")
	run(t, root, "add", "notes.md")
	run(t, root, "commit", "-qm", "add notes")
	run(t, root, "checkout", "-q", "-b", "feature")
	run(t, root, "rm", "-q", "notes.md")

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Base: "main"})

	if err != nil {
		t.Fatal(err)
	}
	if got, want := linesOn(d.Whitespace, "notes.md", review.OldSide), []int{2}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected the deleted blank line to be named as whitespace, got %v", got)
	}
}

func TestALineOfCodeIsNotNamedAsWhitespace(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\ntwo\nthree\n\tfour\n")

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Base: "main"})

	if err != nil {
		t.Fatal(err)
	}
	if got := linesOn(d.Whitespace, "app.ts", review.NewSide); len(got) != 0 {
		t.Errorf("expected an indented line of code not to be named as whitespace, got %v", got)
	}
}

// TestANewFileListedAsSectionsIsAcceptedWithoutNamingItsBlankLines is the
// headline scenario of #85, driven through real git and a real working-tree
// resolver: an agent lists a new file's sections and leaves out the blank lines
// between them, which are Changed Lines like any other.
func TestANewFileListedAsSectionsIsAcceptedWithoutNamingItsBlankLines(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "config.yml", "first: 1\nsecond: 2\n\nthird: 3\nfourth: 4\n")
	run(t, root, "add", "config.yml")

	session := review.NewSession(workingtree.NewResolver(), git.NewDeriver())
	err := session.Post(review.Round{
		Label: "LABEL-the-review",
		Brief: review.Brief{
			Goal:     "Add the config file",
			Approach: "Two sections, listed one per Excerpt",
		},
		ChangeSet: review.ChangeSet{Repositories: []review.Repository{{Root: root, Base: "main"}}},
		Steps: []review.Step{{
			Name:        "The new config",
			Explanation: "Two sections of settings",
			Excerpts: []review.Excerpt{
				{Repository: root, File: "config.yml", Side: review.NewSide, FirstLine: 1, LastLine: 2},
				{Repository: root, File: "config.yml", Side: review.NewSide, FirstLine: 4, LastLine: 5},
			},
		}},
	})

	if err != nil {
		t.Fatalf("expected the Round to be accepted, got %v", err)
	}
	if err := session.GoTo(1); err != nil {
		t.Fatal(err)
	}
	shown := session.View().Step.Excerpts[0].Excerpt
	if shown.LastLine != 3 {
		t.Errorf("expected the first Excerpt to have absorbed the blank line 3, got %d-%d", shown.FirstLine, shown.LastLine)
	}
}

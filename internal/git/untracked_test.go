package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
	"github.com/probertson/diff-by-numbers/internal/workingtree"
)

// The glossary defines the Change Set as the range "plus its working tree", and
// to a Reviewer a new file they have not `git add`ed yet is part of that. Before
// this, such a file contributed no Changed Lines at all: it rendered as unchanged
// context and escaped coverage entirely.
func TestAnUntrackedFileDerivesAsAddedChangedLines(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "notes.md", "alpha\nbeta\ngamma\n")

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Base: "main"})
	if err != nil {
		t.Fatal(err)
	}

	added := linesOn(d.Lines, "notes.md", review.NewSide)
	if len(added) != 3 || added[0] != 1 || added[2] != 3 {
		t.Errorf("expected every line of the untracked file as added, got %v", added)
	}
	if old := linesOn(d.Lines, "notes.md", review.OldSide); len(old) != 0 {
		t.Errorf("an untracked file has no before-side, got old-side %v", old)
	}
}

func TestAnIgnoredFileIsNotDerived(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, ".gitignore", "secret.txt\n")
	run(t, root, "add", ".gitignore")
	run(t, root, "commit", "-qm", "ignore secrets")
	write(t, root, "secret.txt", "do not review me\n")

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Base: "main"})
	if err != nil {
		t.Fatal(err)
	}

	if lines := linesOn(d.Lines, "secret.txt", review.NewSide); len(lines) != 0 {
		t.Errorf("an ignored file is not part of the Change Set, got %v", lines)
	}
}

// The whole point of the temp-index mechanism: dbn reads the Reviewer's
// repository, it does not stage anything in it.
func TestDerivingLeavesTheRealIndexByteForByteUnchanged(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "notes.md", "alpha\n")
	before := readIndex(t, root)

	if _, err := git.NewDeriver().Derive(review.Repository{Root: root, Base: "main"}); err != nil {
		t.Fatal(err)
	}

	if after := readIndex(t, root); string(after) != string(before) {
		t.Errorf("the real index changed: %d bytes before, %d after", len(before), len(after))
	}
	if status := output(t, root, "status", "--porcelain"); status != "?? notes.md\n" {
		t.Errorf("notes.md should still be untracked, git status says:\n%s", status)
	}
}

// readIndex finds the index the way the code under test does. Hardcoding
// .git/index would work for an ordinary clone and quietly read the wrong file
// in a worktree — exactly where getting it right matters most.
func readIndex(t *testing.T, root string) []byte {
	t.Helper()
	located := strings.TrimSpace(output(t, root, "rev-parse", "--git-path", "index"))
	if !filepath.IsAbs(located) {
		located = filepath.Join(root, located)
	}
	raw, err := os.ReadFile(located)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// output runs git and returns its stdout, for the assertions that need to ask
// the repository a question rather than just drive it.
func output(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

// An untracked binary has no lines to show, so it takes the same route a tracked
// one does: an Opaque Change, which the Reviewer sees and the agent must
// Acknowledge rather than silently skip.
func TestAnUntrackedBinaryDerivesAsAnOpaqueChange(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	writeBytes(t, root, "logo.png", []byte{0x89, 'P', 'N', 'G', 0x00, 0x01, 0x02, 0x00, 0xff})

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Base: "main"})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := opaqueFor(d.Opaque, "logo.png"); !ok {
		t.Errorf("expected an Opaque Change for the untracked binary, got %+v", d.Opaque)
	}
	if lines := linesOn(d.Lines, "logo.png", review.NewSide); len(lines) != 0 {
		t.Errorf("a binary file has no Changed Lines, got %v", lines)
	}
}

// `mv` without `git mv` leaves a deletion and an untracked file. Once the
// untracked side is visible, git pairs them into one rename — so the Reviewer
// sees a move, not a whole file deleted and another one invented.
func TestAFileMovedWithoutGitDerivesAsOneRename(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	move(t, root, "app.ts", "renamed.ts")

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Base: "main"})
	if err != nil {
		t.Fatal(err)
	}

	opaque, ok := opaqueFor(d.Opaque, "renamed.ts")
	if !ok {
		t.Fatalf("expected one Opaque rename at the new path, got %+v", d.Opaque)
	}
	if !strings.Contains(opaque.Detail, "renamed from app.ts") {
		t.Errorf("the rename should name its source, got Detail %q", opaque.Detail)
	}
	if opaque.Kind != review.OpaqueRename {
		t.Errorf("a pure move is a rename, got Kind %q", opaque.Kind)
	}
	if old := linesOn(d.Lines, "app.ts", review.OldSide); len(old) != 0 {
		t.Errorf("a pure move is not a deletion, got old-side lines %v", old)
	}
}

// The case the agreed fix calls out as the reason both diff call sites share the
// helper: derivation sees a rename-with-edits, so the before-side reader must
// also see the untracked side or it looks for the old path under the new name.
func TestAFileMovedWithoutGitAndEditedReadsItsBeforeSideFromTheOldPath(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	move(t, root, "app.ts", "renamed.ts")
	write(t, root, "renamed.ts", "one\ntwo\nthree\nfour\n")

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if added := linesOn(d.Lines, "renamed.ts", review.NewSide); len(added) == 0 {
		t.Fatalf("expected the edit to derive as a Changed Line, got %+v", d.Lines)
	}

	before, err := git.ReadFileAt(root, mergeBaseOf(t, root, "main"), "renamed.ts")
	if err != nil {
		t.Fatalf("reading the before-side through the rename: %v", err)
	}
	if len(before) != 3 || before[0] != "one" {
		t.Errorf("the before-side should be app.ts as it stood at the base, got %v", before)
	}
}

// Worktrees keep their index under the main repository's git directory, which is
// why the index is located with rev-parse rather than assumed to be .git/index.
func TestUntrackedFilesAreDerivedInsideAGitWorktree(t *testing.T) {
	root := newRepo(t)
	tree := filepath.Join(t.TempDir(), "wt")
	run(t, root, "worktree", "add", "-q", "-b", "feature", tree)
	write(t, tree, "notes.md", "alpha\nbeta\n")

	before := readIndex(t, tree)

	d, err := git.NewDeriver().Derive(review.Repository{Root: tree, Base: "main"})
	if err != nil {
		t.Fatal(err)
	}

	if added := linesOn(d.Lines, "notes.md", review.NewSide); len(added) != 2 {
		t.Errorf("expected the untracked file's lines inside the worktree, got %v", added)
	}
	// The worktree's index lives under the main repository's git directory, so
	// this is where locating it by hand rather than by rev-parse would go wrong.
	if after := readIndex(t, tree); string(after) != string(before) {
		t.Errorf("the worktree's real index changed: %d bytes before, %d after", len(before), len(after))
	}
}

// move renames a file on disk without telling git, the way an editor or `mv` does.
func move(t *testing.T, root, from, to string) {
	t.Helper()
	if err := os.Rename(filepath.Join(root, from), filepath.Join(root, to)); err != nil {
		t.Fatal(err)
	}
}

func mergeBaseOf(t *testing.T, root, ref string) string {
	t.Helper()
	base, err := git.MergeBase(root, ref)
	if err != nil {
		t.Fatal(err)
	}
	return base
}

// The property the whole ticket exists for, and the one the unit tests above do
// not reach: before this, an agent could leave an untracked file out of the
// Round entirely and the post was *accepted*, because the ledger held
// nothing to cover. Derivation alone cannot show that — only a real Session can.
func TestARoundThatIgnoresAnUntrackedFileIsRejected(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\n") // the tracked change, covered below
	write(t, root, "notes.md", "alpha\nbeta\n")         // never `git add`ed, and never shown

	session := review.NewSession(workingtree.NewResolver(), git.NewDeriver())
	err := session.Post(walkthroughCovering(root, "app.ts", 1, 4))

	if err == nil {
		t.Fatal("an untracked file shown by nothing at all must not be accepted")
	}
	if !strings.Contains(err.Error(), "uncovered_changes") {
		t.Errorf("it should be refused for coverage, not something else, got: %v", err)
	}
	if !strings.Contains(err.Error(), "notes.md") {
		t.Errorf("the rejection should name the file that escaped coverage, got: %v", err)
	}
}

// ... and the same Round with the untracked file accounted for is accepted,
// so the rejection above is about coverage and not some unrelated refusal.
func TestARoundThatCoversTheUntrackedFileIsAccepted(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\n")
	write(t, root, "notes.md", "alpha\nbeta\n")

	walkthrough := walkthroughCovering(root, "app.ts", 1, 4)
	walkthrough.Steps[0].Excerpts = append(walkthrough.Steps[0].Excerpts,
		review.Excerpt{Repository: root, File: "notes.md", Side: review.NewSide, FirstLine: 1, LastLine: 2})
	session := review.NewSession(workingtree.NewResolver(), git.NewDeriver())

	if err := session.Post(walkthrough); err != nil {
		t.Fatalf("covering the untracked file should be enough, got: %v", err)
	}
}

// A scratch file the agent does not want to walk through is still the Reviewer's
// to see: an Acknowledgement satisfies coverage, visibly.
func TestAnUntrackedFileCanBeCoveredByAnAcknowledgement(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "app.ts", "one\ntwo\nthree\nfour\n")
	write(t, root, "scratch.log", "noise\nmore noise\n")

	walkthrough := walkthroughCovering(root, "app.ts", 1, 4)
	walkthrough.Steps[0].Acknowledgements = []review.Acknowledgement{
		{Repository: root, Files: []string{"scratch.log"}, Reason: "scratch output, nothing to read"},
	}
	session := review.NewSession(workingtree.NewResolver(), git.NewDeriver())

	if err := session.Post(walkthrough); err != nil {
		t.Fatalf("an Acknowledgement should cover an untracked file, got: %v", err)
	}
}

// roundCovering is the smallest Round that shows one range of one file.
func walkthroughCovering(root, file string, first, last int) review.Round {
	return review.Round{
		Label: "LABEL-the-review",
		Brief: review.Brief{
			Goal:     "Add a line and some notes",
			Approach: "One code change, one new file",
		},
		ChangeSet: review.ChangeSet{Repositories: []review.Repository{{Root: root, Base: "main"}}},
		Steps: []review.Step{{
			Name:        "The change",
			Explanation: "the tracked edit",
			Excerpts: []review.Excerpt{
				{Repository: root, File: file, Side: review.NewSide, FirstLine: first, LastLine: last},
			},
		}},
	}
}

// The throwaway index and pathspec file are dbn's business, not the Reviewer's
// machine's: a derivation per review, leaking a copy of the index each time,
// would be a slow disk leak nobody would trace back here.
func TestDerivingLeavesNoTemporaryFilesBehind(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "notes.md", "alpha\n")
	before := countTempFiles(t)

	for i := 0; i < 3; i++ {
		if _, err := git.NewDeriver().Derive(review.Repository{Root: root, Base: "main"}); err != nil {
			t.Fatal(err)
		}
	}

	if after := countTempFiles(t); after != before {
		t.Errorf("temporary files left behind: %d before, %d after three derivations", before, after)
	}
}

// Starting from an empty index would report every tracked file as deleted — a
// Change Set wrong in the most alarming possible direction. Better to refuse.
func TestAMissingIndexFailsTheDerivationRatherThanStartingFromAnEmptyOne(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "notes.md", "alpha\n") // an untracked file, so the index is needed
	if err := os.Remove(filepath.Join(root, ".git", "index")); err != nil {
		t.Fatal(err)
	}

	_, err := git.NewDeriver().Derive(review.Repository{Root: root, Base: "main"})

	if err == nil {
		t.Fatal("a missing index must fail the derivation, not be worked around")
	}
	if !strings.Contains(err.Error(), "index") {
		t.Errorf("the error should say what could not be read, got: %v", err)
	}
}

// git treats a leading colon as pathspec magic, so an untracked file named
// ":notes.md" is read as a malformed directive and git refuses the whole
// command — failing derivation for the entire repository over one filename.
func TestAnUntrackedFileWhoseNameLooksLikePathspecMagicIsStillDerived(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(root, ":notes.md"), []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Skipf("this filesystem will not hold a file named ':notes.md': %v", err)
	}

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Base: "main"})

	if err != nil {
		t.Fatalf("one oddly named file must not fail the whole derivation: %v", err)
	}
	if added := linesOn(d.Lines, ":notes.md", review.NewSide); len(added) != 2 {
		t.Errorf("expected the colon-named file's lines, got %v", added)
	}
}

// countTempFiles counts the temporaries dbn creates, by their prefixes, so an
// unrelated process writing to TMPDIR during the test cannot upset the count.
func countTempFiles(t *testing.T) int {
	t.Helper()
	total := 0
	for _, prefix := range []string{"dbn-index-", "dbn-pathspec-"} {
		matches, err := filepath.Glob(filepath.Join(os.TempDir(), prefix+"*"))
		if err != nil {
			t.Fatal(err)
		}
		total += len(matches)
	}
	return total
}

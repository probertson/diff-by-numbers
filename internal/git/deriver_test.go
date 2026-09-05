package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func opaqueFor(all []review.OpaqueChange, file string) (review.OpaqueChange, bool) {
	for _, o := range all {
		if o.File == file {
			return o, true
		}
	}
	return review.OpaqueChange{}, false
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

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "main"})
	if err != nil {
		t.Fatal(err)
	}
	lines := d.Lines

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

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "main"})
	if err != nil {
		t.Fatal(err)
	}
	lines := d.Lines

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

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "main"})
	if err != nil {
		t.Fatal(err)
	}
	lines := d.Lines

	added := linesOn(lines, "src/added.ts", review.NewSide)
	if len(added) != 2 || added[0] != 1 || added[1] != 2 {
		t.Errorf("expected staged new file lines 1 and 2, got %v", added)
	}
}

func TestNoChangesYieldsNoChangedLines(t *testing.T) {
	root := newRepo(t)

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "main"})
	if err != nil {
		t.Fatal(err)
	}
	lines := d.Lines

	if len(lines) != 0 {
		t.Errorf("expected no changed lines against the same branch, got %v", lines)
	}
}

func TestDerivesAWholeFileDeletionAsOldSideLines(t *testing.T) {
	root := newRepo(t)
	write(t, root, "doomed.ts", "alpha\nbeta\ngamma\n")
	run(t, root, "add", ".")
	run(t, root, "commit", "-qm", "add doomed")
	run(t, root, "checkout", "-q", "-b", "feature")
	run(t, root, "rm", "-q", "doomed.ts")

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "main"})
	if err != nil {
		t.Fatal(err)
	}

	deleted := linesOn(d.Lines, "doomed.ts", review.OldSide)
	if len(deleted) != 3 || deleted[0] != 1 || deleted[2] != 3 {
		t.Errorf("expected the deleted file's old-side lines 1-3, got %v", deleted)
	}
}

func writeBytes(t *testing.T, root, name string, content []byte) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDerivesAModifiedBinaryFileAsAnOpaqueChange(t *testing.T) {
	root := t.TempDir()
	run(t, root, "init", "-q", "-b", "main")
	writeBytes(t, root, "logo.png", []byte{0x00, 0x01, 0x02, 0x00, 0xff})
	run(t, root, "add", ".")
	run(t, root, "commit", "-qm", "add logo")
	run(t, root, "checkout", "-q", "-b", "feature")
	writeBytes(t, root, "logo.png", []byte{0x00, 0x01, 0x02, 0x00, 0xfe, 0x10})

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "main"})
	if err != nil {
		t.Fatal(err)
	}

	if got := linesOn(d.Lines, "logo.png", review.NewSide); len(got) != 0 {
		t.Errorf("a binary file has no changed lines, got %v", got)
	}
	opaque, ok := opaqueFor(d.Opaque, "logo.png")
	if !ok || opaque.Kind != review.OpaqueBinary {
		t.Fatalf("expected a binary Opaque Change for logo.png, got %v", d.Opaque)
	}
}

func TestDerivesAModeChangeAsAnOpaqueChange(t *testing.T) {
	root := newRepo(t)
	run(t, root, "config", "core.fileMode", "true")
	run(t, root, "checkout", "-q", "-b", "feature")
	if err := os.Chmod(filepath.Join(root, "app.ts"), 0o755); err != nil {
		t.Fatal(err)
	}

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "main"})
	if err != nil {
		t.Fatal(err)
	}

	if got := linesOn(d.Lines, "app.ts", review.NewSide); len(got) != 0 {
		t.Errorf("a mode-only change has no changed lines, got %v", got)
	}
	opaque, ok := opaqueFor(d.Opaque, "app.ts")
	if !ok || opaque.Kind != review.OpaqueMode {
		t.Fatalf("expected a mode Opaque Change for app.ts, got %v", d.Opaque)
	}
}

func TestDerivesAPureRenameAsAnOpaqueChange(t *testing.T) {
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	run(t, root, "mv", "app.ts", "renamed.ts")

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "main"})
	if err != nil {
		t.Fatal(err)
	}

	if len(d.Lines) != 0 {
		t.Errorf("a pure rename has no changed lines, got %v", d.Lines)
	}
	opaque, ok := opaqueFor(d.Opaque, "renamed.ts")
	if !ok || opaque.Kind != review.OpaqueRename {
		t.Fatalf("expected a rename Opaque Change for renamed.ts, got %v", d.Opaque)
	}
}

func TestDerivesANonASCIIPathLiterally(t *testing.T) {
	// git C-quotes non-ASCII paths by default, which would derive a mangled File
	// no agent-authored range could match. Derive must defeat that.
	root := newRepo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "café.ts", "un\ndeux\n")
	run(t, root, "add", ".")

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "main"})
	if err != nil {
		t.Fatal(err)
	}

	if got := linesOn(d.Lines, "café.ts", review.NewSide); len(got) != 2 {
		t.Errorf("expected café.ts to derive under its real name with 2 lines, got %v and files %v", got, d.Lines)
	}
}

func TestDerivesARenamedBinaryRecordingBothFacts(t *testing.T) {
	// A large binary with a single byte flipped stays similar enough for git's
	// rename detection to fire while still differing in content.
	original := make([]byte, 200)
	original[0] = 0x00 // ensure git treats it as binary (a NUL byte)
	changed := make([]byte, 200)
	copy(changed, original)
	changed[100] = 0x7f

	root := t.TempDir()
	run(t, root, "init", "-q", "-b", "main")
	writeBytes(t, root, "bin.dat", original)
	run(t, root, "add", ".")
	run(t, root, "commit", "-qm", "add bin")
	run(t, root, "checkout", "-q", "-b", "feature")
	run(t, root, "mv", "bin.dat", "renamed.dat")
	writeBytes(t, root, "renamed.dat", changed)

	d, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "main"})
	if err != nil {
		t.Fatal(err)
	}

	opaque, ok := opaqueFor(d.Opaque, "renamed.dat")
	if !ok {
		t.Fatalf("expected an Opaque Change for renamed.dat, got %+v", d.Opaque)
	}
	if opaque.Kind != review.OpaqueBinary {
		t.Errorf("expected the binary Kind to lead, got %q", opaque.Kind)
	}
	if !strings.Contains(opaque.Detail, "renamed from bin.dat") {
		t.Errorf("expected the Detail to record the rename too, got %q", opaque.Detail)
	}
}

func TestABadRangeIsAnError(t *testing.T) {
	root := newRepo(t)

	_, err := git.NewDeriver().Derive(review.Repository{Root: root, Range: "no-such-branch"})

	if err == nil {
		t.Fatal("expected an error for a range that does not resolve")
	}
}

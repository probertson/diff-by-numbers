package selfupdate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheNoticeNamesTheVersionAndTheCommand(t *testing.T) {
	// The test binary lives in a temp directory the test user can write, which is
	// the ordinary case: `dbn update` can do the work itself.
	notice := Notice("0.2.0")

	if !strings.Contains(notice, "0.2.0") {
		t.Errorf("the notice does not name the release: %q", notice)
	}
	if !strings.Contains(notice, "dbn update") {
		t.Errorf("the notice does not name the command that updates: %q", notice)
	}
}

func TestWritableAnswersByTryingRatherThanByModeBits(t *testing.T) {
	dir := t.TempDir()

	if !Writable(dir) {
		t.Error("a fresh temp directory is reported as unwritable")
	}

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if os.Geteuid() == 0 {
		t.Skip("running as root, which can write into a read-only directory")
	}
	if Writable(dir) {
		t.Error("a read-only directory is reported as writable")
	}
}

func TestWritableLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()

	Writable(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("the write probe left %d file(s) behind: %v", len(entries), entries[0].Name())
	}
}

func TestWritableSaysNoAboutADirectoryThatIsNotThere(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone")

	if Writable(missing) {
		t.Error("a directory that does not exist is reported as writable")
	}
}

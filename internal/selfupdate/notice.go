// Package selfupdate is how dbn replaces its own binary with a newer release,
// and how it talks about doing so. Nothing here runs on its own: a binary is
// replaced only when the Reviewer runs `dbn update`.
package selfupdate

import (
	"fmt"
	"os"
	"path/filepath"
)

// ReinstallCommand is the installer one-liner from the README — the way out when
// the running binary sits somewhere this user cannot write. It installs to
// ~/.local/bin, which they can.
const ReinstallCommand = "curl -fsSL https://raw.githubusercontent.com/probertson/diff-by-numbers/main/install.sh | sh"

// Notice is the one line dbn shows when a newer release exists: in the TUI
// header and after the version on `dbn version`. It names the way forward the
// Reviewer can actually take — `dbn update` cannot help where it cannot write.
func Notice(latest string) string {
	dir, err := ExecutableDir()
	if err != nil || !Writable(dir) {
		where := dir
		if where == "" {
			where = "the directory dbn is installed in"
		}
		return fmt.Sprintf("dbn %s available — %s is not writable; reinstall with: %s", latest, where, ReinstallCommand)
	}
	return fmt.Sprintf("dbn %s available — run dbn update", latest)
}

// ExecutableDir is the directory the running dbn lives in: where an update
// stages the new binary and what it renames over.
func ExecutableDir() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	// Follow symlinks: a link in ~/bin pointing at the real binary elsewhere must
	// be updated where the bytes are, not where the link is.
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		resolved = executable
	}
	return filepath.Dir(resolved), nil
}

// Writable reports whether dbn can put a new file in dir — the permission an
// update actually needs, since it stages beside the binary and renames over it.
// It answers by trying: mode bits alone account for neither ownership, nor ACLs,
// nor a read-only mount.
func Writable(dir string) bool {
	probe, err := os.CreateTemp(dir, ".dbn-write-probe-")
	if err != nil {
		return false
	}
	name := probe.Name()
	probe.Close()
	_ = os.Remove(name)
	return true
}

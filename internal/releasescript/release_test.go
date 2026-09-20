// Package releasescript has no source of its own: cutting a release is a shell
// script (scripts/release.sh), and its behaviour is tested by a shell harness
// (scripts/release_test.sh). This file execs that harness so the release script
// is covered by the project's canonical `go test ./...`.
//
// The same arrangement as internal/installer, for the same reason.
package releasescript

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReleaseScript(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	harness := filepath.Join(root, "scripts", "release_test.sh")
	if _, err := os.Stat(harness); err != nil {
		t.Fatalf("release test harness not found at %s: %v", harness, err)
	}

	cmd := exec.Command("sh", harness)
	cmd.Dir = root

	out, err := cmd.CombinedOutput()

	t.Logf("release_test.sh output:\n%s", out)
	if err != nil {
		t.Fatalf("release.sh harness failed: %v", err)
	}
}

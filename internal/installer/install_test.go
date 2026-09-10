// Package installer has no source of its own: the curl-able installer is a shell
// script (install.sh) at the repo root, and its behaviour is tested by a shell
// harness (install_test.sh). This file execs that harness so the installer is
// covered by the project's canonical `go test ./...`.
package installer

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInstallScript(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	harness := filepath.Join(root, "install_test.sh")
	if _, err := os.Stat(harness); err != nil {
		t.Fatalf("install test harness not found at %s: %v", harness, err)
	}

	cmd := exec.Command("sh", harness)
	cmd.Dir = root

	out, err := cmd.CombinedOutput()

	t.Logf("install_test.sh output:\n%s", out)
	if err != nil {
		t.Fatalf("install.sh harness failed: %v", err)
	}
}

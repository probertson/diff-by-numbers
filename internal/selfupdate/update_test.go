package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
	"github.com/probertson/diff-by-numbers/internal/updatecheck"
)

// newBinary is what the fake release ships, and what the installed binary must
// contain once an update has succeeded.
const newBinary = "#!/bin/sh\necho dbn 0.2.0\n"

// release is a stand-in for the GitHub release: the latest-release API and the
// assets that hang off the tag, served from one test server.
type release struct {
	*httptest.Server
	tag      string
	checksum string // overrides the real one, to fake a corrupted download
}

func newRelease(t *testing.T, tag string) *release {
	t.Helper()
	r := &release{tag: tag}
	asset := fmt.Sprintf("dbn_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archive := tarball(t, newBinary)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/latest", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": r.tag})
	})
	mux.HandleFunc("/download/"+tag+"/"+asset, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	mux.HandleFunc("/download/"+tag+"/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		sum := r.checksum
		if sum == "" {
			digest := sha256.Sum256(archive)
			sum = hex.EncodeToString(digest[:])
		}
		fmt.Fprintf(w, "%s  %s\n", sum, asset)
	})
	r.Server = httptest.NewServer(mux)
	t.Cleanup(r.Close)
	return r
}

// installed writes a stand-in for the running dbn binary and returns its path.
func installed(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dbn")
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func testUpdateConfig(t *testing.T, rel *release, executable, current string) Config {
	t.Helper()
	return Config{
		Current: current,
		Check: updatecheck.Config{
			Current:   current,
			Endpoint:  rel.URL + "/api/latest",
			CachePath: filepath.Join(t.TempDir(), "update-check.json"),
			Force:     true,
		},
		ReleaseBase: rel.URL + "/download",
		Executable:  executable,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
	}
}

func run(t *testing.T, cfg Config) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Update(context.Background(), cfg, &out)
	return out.String(), err
}

func contents(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestUpdateReplacesTheBinaryWithTheNewRelease(t *testing.T) {
	rel := newRelease(t, "v0.2.0")
	executable := installed(t, "the old binary")

	out, err := run(t, testUpdateConfig(t, rel, executable, "0.1.0"))

	if err != nil {
		t.Fatalf("the update failed: %v\n%s", err, out)
	}
	if got := contents(t, executable); got != newBinary {
		t.Errorf("the installed binary is %q, want the downloaded one", got)
	}
	if !strings.Contains(out, "Updated to 0.2.0") {
		t.Errorf("the update did not report what it did: %q", out)
	}
}

func TestTheReplacedBinaryIsExecutable(t *testing.T) {
	rel := newRelease(t, "v0.2.0")
	executable := installed(t, "the old binary")

	if _, err := run(t, testUpdateConfig(t, rel, executable, "0.1.0")); err != nil {
		t.Fatalf("the update failed: %v", err)
	}

	info, err := os.Stat(executable)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("the new binary is not executable (mode %v)", info.Mode().Perm())
	}
}

// The whole point of publishing checksums: a download that does not match them
// must not reach the disk.
func TestAChecksumMismatchLeavesTheExistingBinaryUntouched(t *testing.T) {
	rel := newRelease(t, "v0.2.0")
	rel.checksum = strings.Repeat("0", 64)
	executable := installed(t, "the old binary")

	_, err := run(t, testUpdateConfig(t, rel, executable, "0.1.0"))

	if err == nil {
		t.Fatal("expected a checksum mismatch to fail the update")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("the failure does not name the reason: %v", err)
	}
	if got := contents(t, executable); got != "the old binary" {
		t.Errorf("the installed binary was replaced anyway: %q", got)
	}
}

func TestADevelopmentBuildRefusesToUpdateItself(t *testing.T) {
	rel := newRelease(t, "v0.2.0")
	executable := installed(t, "the old binary")
	cfg := testUpdateConfig(t, rel, executable, "0.1.0-dev")
	cfg.Dev = true

	_, err := run(t, cfg)

	if err == nil {
		t.Fatal("expected a development build to refuse")
	}
	if !strings.Contains(err.Error(), "rebuilding from source") {
		t.Errorf("the refusal does not say what to do instead: %v", err)
	}
	if got := contents(t, executable); got != "the old binary" {
		t.Error("a development build replaced its own binary")
	}
}

func TestNothingNewerIsSaidPlainlyAndChangesNothing(t *testing.T) {
	rel := newRelease(t, "v0.2.0")
	executable := installed(t, "the old binary")

	out, err := run(t, testUpdateConfig(t, rel, executable, "0.2.0"))

	if err != nil {
		t.Fatalf("being up to date reported an error: %v", err)
	}
	if !strings.Contains(out, "already the latest") {
		t.Errorf("being up to date was not said plainly: %q", out)
	}
	if got := contents(t, executable); got != "the old binary" {
		t.Error("an up-to-date dbn replaced its own binary")
	}
}

func TestAFailedCheckStopsTheUpdate(t *testing.T) {
	rel := newRelease(t, "v0.2.0")
	executable := installed(t, "the old binary")
	cfg := testUpdateConfig(t, rel, executable, "0.1.0")
	cfg.Check.Endpoint = rel.URL + "/api/nothing-here"

	_, err := run(t, cfg)

	if err == nil {
		t.Fatal("expected a failed check to stop the update")
	}
	if got := contents(t, executable); got != "the old binary" {
		t.Error("the binary was replaced despite the check failing")
	}
}

func TestAnUnwritableDirectoryOffersTheInstallerBeforeSudo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which can write into a read-only directory")
	}
	rel := newRelease(t, "v0.2.0")
	executable := installed(t, "the old binary")
	directory := filepath.Dir(executable)
	if err := os.Chmod(directory, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0o700) })

	_, err := run(t, testUpdateConfig(t, rel, executable, "0.1.0"))

	if err == nil {
		t.Fatal("expected an unwritable directory to stop the update")
	}
	message := err.Error()
	if !strings.Contains(message, directory) {
		t.Errorf("the failure does not name the directory: %v", message)
	}
	installer := strings.Index(message, ReinstallCommand)
	sudo := strings.Index(message, "sudo dbn update")
	if installer < 0 || sudo < 0 {
		t.Fatalf("the failure does not offer both ways out: %v", message)
	}
	if installer > sudo {
		t.Error("the failure offers sudo before the installer")
	}
}

// --- the running daemon -----------------------------------------------------

// fakeDaemon answers /status like a daemon and records a shutdown.
type fakeDaemon struct {
	*httptest.Server
	stopped bool
}

func newFakeDaemon(t *testing.T, status daemon.StatusWire) *fakeDaemon {
	t.Helper()
	d := &fakeDaemon{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(status)
	})
	mux.HandleFunc("POST /shutdown", func(w http.ResponseWriter, _ *http.Request) {
		d.stopped = true
		fmt.Fprintln(w, "shutting down")
	})
	d.Server = httptest.NewServer(mux)
	t.Cleanup(d.Close)
	return d
}

func TestAnIdleDaemonOnThisBinaryIsStoppedSoItComesBackNew(t *testing.T) {
	rel := newRelease(t, "v0.2.0")
	executable := installed(t, "the old binary")
	running := newFakeDaemon(t, daemon.StatusWire{Version: "0.1.0", Executable: executable})
	cfg := testUpdateConfig(t, rel, executable, "0.1.0")
	cfg.DaemonURL = running.URL

	out, err := run(t, cfg)

	if err != nil {
		t.Fatalf("the update failed: %v", err)
	}
	if !running.stopped {
		t.Error("an idle daemon on the replaced binary was left running the old build")
	}
	if !strings.Contains(out, "reconnect") {
		t.Errorf("the update did not say what happens to connected sessions: %q", out)
	}
}

func TestADaemonMidReviewIsLeftAloneAndExplained(t *testing.T) {
	rel := newRelease(t, "v0.2.0")
	executable := installed(t, "the old binary")
	running := newFakeDaemon(t, daemon.StatusWire{Version: "0.1.0", Executable: executable, ActiveReview: true})
	cfg := testUpdateConfig(t, rel, executable, "0.1.0")
	cfg.DaemonURL = running.URL

	out, err := run(t, cfg)

	if err != nil {
		t.Fatalf("the update failed: %v", err)
	}
	if running.stopped {
		t.Error("a review in progress was thrown away by the update")
	}
	if got := contents(t, executable); got != newBinary {
		t.Error("the binary was not replaced, though only the daemon restart was in question")
	}
	if !strings.Contains(out, "review is in progress") || !strings.Contains(out, "0.1.0") {
		t.Errorf("the update did not explain the daemon it left behind: %q", out)
	}
}

func TestForceStopsADaemonMidReview(t *testing.T) {
	rel := newRelease(t, "v0.2.0")
	executable := installed(t, "the old binary")
	running := newFakeDaemon(t, daemon.StatusWire{Version: "0.1.0", Executable: executable, ActiveReview: true})
	cfg := testUpdateConfig(t, rel, executable, "0.1.0")
	cfg.DaemonURL = running.URL
	cfg.Force = true

	if _, err := run(t, cfg); err != nil {
		t.Fatalf("the update failed: %v", err)
	}

	if !running.stopped {
		t.Error("--force did not stop the daemon")
	}
}

func TestADaemonRunningADifferentBinaryIsNotStopped(t *testing.T) {
	rel := newRelease(t, "v0.2.0")
	executable := installed(t, "the old binary")
	elsewhere := installed(t, "a different dbn")
	running := newFakeDaemon(t, daemon.StatusWire{Version: "0.1.0", Executable: elsewhere})
	cfg := testUpdateConfig(t, rel, executable, "0.1.0")
	cfg.DaemonURL = running.URL

	out, err := run(t, cfg)

	if err != nil {
		t.Fatalf("the update failed: %v", err)
	}
	if running.stopped {
		t.Error("a daemon running a different binary was restarted pointlessly")
	}
	if !strings.Contains(out, elsewhere) {
		t.Errorf("the update did not name the binary the daemon actually runs: %q", out)
	}
}

func TestNoDaemonMeansNothingToSay(t *testing.T) {
	rel := newRelease(t, "v0.2.0")
	executable := installed(t, "the old binary")
	cfg := testUpdateConfig(t, rel, executable, "0.1.0")
	cfg.DaemonURL = "http://127.0.0.1:1" // nothing listens here

	out, err := run(t, cfg)

	if err != nil {
		t.Fatalf("the update failed with no daemon running: %v", err)
	}
	if strings.Contains(out, "daemon") {
		t.Errorf("the update talked about a daemon that is not there: %q", out)
	}
}

func TestThePlanForARunningDaemon(t *testing.T) {
	const ours, theirs = "/usr/local/bin/dbn", "/opt/other/dbn"
	cases := []struct {
		name   string
		status *daemon.StatusWire
		force  bool
		want   daemonPlan
	}{
		{"nothing listening", nil, false, daemonAbsent},
		{"idle and ours", &daemon.StatusWire{Executable: ours}, false, daemonRestart},
		{"mid-review", &daemon.StatusWire{Executable: ours, ActiveReview: true}, false, daemonBusy},
		{"mid-review, forced", &daemon.StatusWire{Executable: ours, ActiveReview: true}, true, daemonRestart},
		{"someone else's binary", &daemon.StatusWire{Executable: theirs}, false, daemonElsewhere},
		{"someone else's binary, mid-review, forced", &daemon.StatusWire{Executable: theirs, ActiveReview: true}, true, daemonElsewhere},
		{"a daemon that cannot name itself", &daemon.StatusWire{}, false, daemonRestart},
	}

	for _, c := range cases {
		if got := planFor(c.status, ours, c.force); got != c.want {
			t.Errorf("%s: planned %v, want %v", c.name, got, c.want)
		}
	}
}

// --- the archive ------------------------------------------------------------

func TestExtractBinaryFindsDbnWhateverTheArchiveCallsItsPath(t *testing.T) {
	archive := tarballNamed(t, "./dbn", newBinary)

	binary, err := extractBinary(archive)

	if err != nil {
		t.Fatalf("could not extract the binary: %v", err)
	}
	if string(binary) != newBinary {
		t.Errorf("extracted %q, want the binary", binary)
	}
}

func TestAnArchiveWithoutDbnIsRefused(t *testing.T) {
	archive := tarballNamed(t, "README.md", "not a binary")

	if _, err := extractBinary(archive); err == nil {
		t.Error("expected an archive with no dbn binary to be refused")
	}
}

func TestChecksumForReadsTheEntryForOneAsset(t *testing.T) {
	const checksums = "aaa  dbn_linux_amd64.tar.gz\nbbb  dbn_darwin_arm64.tar.gz\n"

	got, ok := checksumFor(checksums, "dbn_darwin_arm64.tar.gz")

	if !ok || got != "bbb" {
		t.Errorf("read %q (found=%v), want bbb", got, ok)
	}
	if _, ok := checksumFor(checksums, "dbn_windows_amd64.tar.gz"); ok {
		t.Error("found a checksum for an asset that has none")
	}
}

func tarball(t *testing.T, binary string) []byte {
	t.Helper()
	return tarballNamed(t, "dbn", binary)
}

func tarballNamed(t *testing.T, name, contents string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	archive := tar.NewWriter(gz)
	if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(contents)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write([]byte(contents)); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

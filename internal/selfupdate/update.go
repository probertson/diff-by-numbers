package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/probertson/diff-by-numbers/internal/buildinfo"
	"github.com/probertson/diff-by-numbers/internal/daemon"
	"github.com/probertson/diff-by-numbers/internal/updatecheck"
)

// ReleaseBase is where GoReleaser publishes a release's assets. The archive and
// checksum names below are the other half of .goreleaser.yaml's name templates,
// and the same contract install.sh is built against.
const ReleaseBase = "https://github.com/probertson/diff-by-numbers/releases/download"

// maxAsset bounds what an update will read from the network. A dbn archive is a
// few megabytes; this is loose enough never to bite and tight enough that a
// wrong URL cannot fill a disk.
const maxAsset = 64 << 20

// Config is one run of `dbn update`, with everything it touches injectable so a
// test can point it at a fake release and a throwaway binary.
type Config struct {
	// Current is the running build, and Dev says it never came from a release —
	// in which case there is nothing to replace it with.
	Current string
	Dev     bool
	// Check finds the newest release. `dbn update` forces it: about to replace a
	// binary, dbn asks for itself rather than trusting a cached answer.
	Check updatecheck.Config
	// ReleaseBase is the directory releases hang under; a release's own assets are
	// at ReleaseBase/<tag>/.
	ReleaseBase string
	// Executable is the binary to replace.
	Executable string
	// DaemonURL is where a running daemon would be listening.
	DaemonURL string
	// Force skips the active-review check, losing the in-memory review.
	Force bool

	OS, Arch string
	// Client downloads the release. The daemon is asked and stopped over its own
	// short-timeout connections: waiting minutes on loopback would mean something
	// other than a daemon is there.
	Client *http.Client
}

// downloadTimeout bounds fetching a release. Generous, because it covers a whole
// archive over whatever connection the Reviewer has.
const downloadTimeout = 5 * time.Minute

// DefaultConfig is a real update of this binary against the published releases.
func DefaultConfig(daemonURL string) (Config, error) {
	executable, err := os.Executable()
	if err != nil {
		return Config{}, fmt.Errorf("could not find the dbn executable to replace: %w", err)
	}
	executable = resolve(executable) // replace the bytes, not a link to them

	check := updatecheck.DefaultConfig()
	check.Force = true
	// The Reviewer asked for this one: neither an opt-out nor a cached answer
	// should make it silently do nothing. A development build still refuses, but
	// it says so rather than pretending to be up to date.
	check.Disabled = false

	return Config{
		Current:     buildinfo.Version(),
		Dev:         !buildinfo.IsRelease(),
		Check:       check,
		ReleaseBase: ReleaseBase,
		Executable:  executable,
		DaemonURL:   daemonURL,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		Client:      &http.Client{Timeout: downloadTimeout},
	}, nil
}

// Update replaces this binary with the newest release, then deals with a daemon
// still running the old one. Nothing downloads or replaces anything unless the
// Reviewer ran this: there is no background update.
func Update(ctx context.Context, cfg Config, out io.Writer) error {
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: downloadTimeout}
	}
	if cfg.Dev {
		return errors.New("this is a development build; update it by rebuilding from source")
	}

	result, err := updatecheck.Check(ctx, cfg.Check)
	if err != nil {
		return fmt.Errorf("could not check for a newer dbn: %w", err)
	}
	if !result.Available {
		fmt.Fprintf(out, "dbn %s is already the latest release.\n", cfg.Current)
		// Still worth saying: the binary and the skill are installed separately,
		// so an up-to-date dbn is no evidence the skill beside it is current.
		reportSkills(ctx, cfg.Executable, out)
		return nil
	}

	directory := filepath.Dir(cfg.Executable)
	if !Writable(directory) {
		// Not sudo, and not by default: a tool that escalates on its own behalf
		// teaches a habit worth not teaching. Say where the binary is and offer the
		// route that needs no privileges first.
		return fmt.Errorf("%s is not writable, so dbn cannot replace itself there.\n"+
			"  Reinstall somewhere you own (defaults to ~/.local/bin):\n    %s\n"+
			"  or run `sudo dbn update` to update in place",
			directory, ReinstallCommand)
	}

	binary, err := fetchRelease(ctx, cfg, result.Tag)
	if err != nil {
		return err
	}
	if err := replaceExecutable(cfg.Executable, binary); err != nil {
		return err
	}
	fmt.Fprintf(out, "Updated to %s.\n", result.Latest)

	// The binary has already been replaced by this point, so the skill hint is
	// owed whatever becomes of the daemon. Sequenced rather than nested for that
	// reason: restartDaemon reports its own trouble and returns nil today, but a
	// future error path there must not silently swallow the hint as well.
	restartErr := restartDaemon(ctx, cfg, out)
	reportSkills(ctx, cfg.Executable, out)
	return restartErr
}

// skillCheckTimeout bounds the skill check. It only reads a few local files, so
// anything approaching this means something is wrong and the hint is not worth
// waiting on.
const skillCheckTimeout = 10 * time.Second

// reportSkills asks the binary on disk to compare the dbn-review skills on this
// machine against the one it shipped with, and passes its answer through.
//
// It runs as a subprocess rather than in this process because of the path that
// matters: after an update, the process running this code is the *outgoing*
// binary, and it embeds the outgoing skill. Only the binary just written knows
// what the new release expects. On the nothing-to-update path the two are the
// same binary and the hop buys nothing, but it costs one exec and keeps a
// single answer to "is my skill current?".
//
// Failures are swallowed. This is advice about a separate install, appended to
// an update that has already happened; a Reviewer whose dbn was replaced should
// not be told the update failed because a hint could not be printed.
func reportSkills(ctx context.Context, executable string, out io.Writer) {
	ctx, cancel := context.WithTimeout(ctx, skillCheckTimeout)
	defer cancel()

	reported, err := exec.CommandContext(ctx, executable, "skill-check").Output()
	if err != nil {
		return
	}
	_, _ = out.Write(reported)
}

// fetchRelease downloads the archive for this platform, verifies it against the
// release's published checksums, and returns the binary inside. Nothing touches
// the installed binary until this has succeeded.
func fetchRelease(ctx context.Context, cfg Config, tag string) ([]byte, error) {
	asset := fmt.Sprintf("dbn_%s_%s.tar.gz", cfg.OS, cfg.Arch)
	base := strings.TrimSuffix(cfg.ReleaseBase, "/") + "/" + tag

	archive, err := download(ctx, cfg.Client, base+"/"+asset)
	if err != nil {
		return nil, fmt.Errorf("could not download %s: %w", asset, err)
	}
	sums, err := download(ctx, cfg.Client, base+"/checksums.txt")
	if err != nil {
		return nil, fmt.Errorf("could not download the checksums to verify %s: %w", asset, err)
	}

	want, ok := checksumFor(string(sums), asset)
	if !ok {
		return nil, fmt.Errorf("no checksum published for %s — refusing to install it", asset)
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return nil, fmt.Errorf("checksum mismatch for %s (expected %s, got %s) — refusing to install it", asset, want, got)
	}
	return extractBinary(archive)
}

func download(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the server answered %s", response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, maxAsset))
}

// checksumFor reads one entry out of a sha256sum-style checksums file, whose
// lines are "<hex>  <filename>".
func checksumFor(checksums, asset string) (string, bool) {
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			return fields[0], true
		}
	}
	return "", false
}

func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("could not read the release archive: %w", err)
	}
	defer gz.Close()

	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("the release archive contains no dbn binary")
		}
		if err != nil {
			return nil, fmt.Errorf("could not read the release archive: %w", err)
		}
		if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == "dbn" {
			return io.ReadAll(io.LimitReader(reader, maxAsset))
		}
	}
}

// replaceExecutable stages the new binary beside the old one and renames over
// it. Staging in the same directory is what makes the swap atomic: a rename
// within one filesystem cannot half-happen, where a copy interrupted partway
// would leave a Reviewer with no working dbn at all.
func replaceExecutable(executable string, binary []byte) error {
	directory := filepath.Dir(executable)
	staged, err := os.CreateTemp(directory, ".dbn-update-")
	if err != nil {
		return fmt.Errorf("could not stage the new dbn in %s: %w", directory, err)
	}
	name := staged.Name()
	defer os.Remove(name) // a no-op once the rename has moved it

	if _, err := staged.Write(binary); err != nil {
		staged.Close()
		return fmt.Errorf("could not write the new dbn: %w", err)
	}
	if err := staged.Close(); err != nil {
		return fmt.Errorf("could not write the new dbn: %w", err)
	}
	if err := os.Chmod(name, 0o755); err != nil {
		return fmt.Errorf("could not make the new dbn executable: %w", err)
	}
	if err := os.Rename(name, executable); err != nil {
		return fmt.Errorf("could not put the new dbn in place at %s: %w", executable, err)
	}
	return nil
}

// probe is what answered on the daemon port: whether anything is there, and what
// it said about itself if it could. The two are separate because a daemon from
// before the status endpoint answers the port but not the question — and that is
// precisely the daemon someone updating for the first time is running.
type probe struct {
	listening bool
	status    *daemon.StatusWire
}

// daemonPlan is what to do about a daemon that is still running the binary just
// replaced — or a different one.
type daemonPlan int

const (
	daemonAbsent     daemonPlan = iota // nothing is listening; the next start is the new build
	daemonUnreadable                   // something answers, but cannot say what it is
	daemonElsewhere                    // a daemon, but not this copy of dbn
	daemonBusy                         // a review is in progress; it would be lost
	daemonRestart                      // idle and ours: stop it, and let it come back new
)

// planFor is the whole of the decision, kept pure. Order matters: a daemon that
// cannot be asked cannot be judged, and one that is not this binary is not ours
// to stop, whatever either is in the middle of.
func planFor(found probe, executable string, force bool) daemonPlan {
	switch {
	case !found.listening:
		return daemonAbsent
	case found.status == nil:
		return daemonUnreadable
	case !sameBinary(found.status.Executable, executable):
		return daemonElsewhere
	case found.status.ActiveReview && !force:
		return daemonBusy
	default:
		return daemonRestart
	}
}

// sameBinary compares two paths to the same file through symlinks. A daemon that
// could not name its own executable is taken to be this one: it is overwhelmingly
// the common case, and the cost of being wrong is a restart of a daemon that
// comes straight back.
func sameBinary(daemonPath, executable string) bool {
	if daemonPath == "" {
		return true
	}
	return resolve(daemonPath) == resolve(executable)
}

func resolve(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// restartDaemon deals with whatever daemon is running now that the binary on
// disk is newer than the one it started from.
func restartDaemon(ctx context.Context, cfg Config, out io.Writer) error {
	found := probeDaemon(ctx, cfg)

	switch planFor(found, cfg.Executable, cfg.Force) {
	case daemonAbsent:
		return nil

	case daemonUnreadable:
		fmt.Fprintf(out, "Something is listening on %s but could not say which build it is — "+
			"if that is an older dbn daemon, restart it yourself to pick up the new one.\n", cfg.DaemonURL)
		return nil

	case daemonElsewhere:
		fmt.Fprintf(out, "The daemon runs %s, not this copy (%s), so it was left alone; "+
			"point your launchd/systemd config here to have it run the new build.\n",
			found.status.Executable, cfg.Executable)
		return nil

	case daemonBusy:
		fmt.Fprintf(out, "The daemon is still running %s because a review is in progress. "+
			"Hand it off or abandon it, then run dbn update again (or restart the daemon yourself).\n",
			found.status.Version)
		return nil

	default:
		if err := shutdownDaemon(ctx, cfg); err != nil {
			fmt.Fprintf(out, "Could not stop the running daemon (%v) — restart it yourself to pick up the new build.\n", err)
			return nil
		}
		fmt.Fprintln(out, "Stopped the daemon; it will come back on the new build.")
		fmt.Fprintln(out, "Claude Code sessions will reconnect on their own; restart them to pick up new dbn tools.")
		return nil
	}
}

// probeDaemon asks whatever is on the daemon port who it is. Only a daemon that
// cannot be reached at all counts as absent: one that answers without a status
// is an older dbn, and being told to restart it beats being told nothing.
func probeDaemon(ctx context.Context, cfg Config) probe {
	if cfg.DaemonURL == "" {
		return probe{}
	}
	status, err := daemon.FetchStatus(ctx, cfg.DaemonURL)
	switch {
	case err == nil:
		return probe{listening: true, status: status}
	case errors.Is(err, daemon.ErrNoStatus):
		return probe{listening: true}
	default:
		return probe{}
	}
}

func shutdownDaemon(ctx context.Context, cfg Config) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.DaemonURL+"/shutdown", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("the daemon answered %s", response.Status)
	}
	return nil
}

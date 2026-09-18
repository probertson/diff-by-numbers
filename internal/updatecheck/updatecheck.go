// Package updatecheck asks GitHub whether a newer dbn release has been
// published. It is dbn's only network call that leaves the loopback interface,
// so it is deliberately narrow: one GET, a short timeout, a cached answer, and
// silence when it fails. It never downloads or replaces anything — that is
// internal/selfupdate, and only when the Reviewer runs `dbn update`.
package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/probertson/diff-by-numbers/internal/buildinfo"
)

// Endpoint is the release GitHub reports as latest. The API is unauthenticated
// and rate-limited per IP; the cache below is what keeps dbn well inside it.
const Endpoint = "https://api.github.com/repos/probertson/diff-by-numbers/releases/latest"

// OptOutEnv turns the check off entirely. It is an environment variable rather
// than a setting because dbn has no settings file yet, and one variable is not
// reason enough to introduce one.
const OptOutEnv = "DBN_NO_UPDATE_CHECK"

// How long an answer stands before dbn asks again. A failure is retried sooner
// than a success is refreshed: being offline for a moment should not cost the
// Reviewer a day of not hearing about a release.
const (
	freshAfterSuccess = 24 * time.Hour
	freshAfterFailure = time.Hour
	requestTimeout    = 3 * time.Second
)

// Result is what the check found.
type Result struct {
	// Latest is the newest published release, empty when the check did not run or
	// could not reach GitHub.
	Latest string
	// Tag is the git tag that release was cut from — the same version, but in the
	// form its download URLs are built from. Carried rather than reconstructed so
	// nothing here has to assume how a tag is spelled.
	Tag string
	// Available reports that Latest is strictly newer than the running build.
	Available bool
}

// Config is everything the check needs, so a test can supply its own server,
// clock and cache file rather than the real ones.
type Config struct {
	// Current is the running build, the version an answer is compared against.
	Current string
	// Endpoint is the latest-release URL.
	Endpoint string
	// CachePath is where the last answer is remembered. Empty means do not cache,
	// which is what happens when the OS has no cache directory to offer.
	CachePath string
	// Disabled makes Check a no-op that touches no network: a development build,
	// or the Reviewer having opted out.
	Disabled bool
	// Force ignores how fresh the cached answer is and asks again. `dbn update`
	// sets it: about to replace a binary, dbn asks for itself rather than
	// trusting an answer up to a day old.
	Force bool

	Client *http.Client
	Now    func() time.Time
}

// DefaultConfig is the real check: the published endpoint, the user's cache
// directory, and off for anything but a release build that has not opted out.
func DefaultConfig() Config {
	// Only the two fields withDefaults cannot supply: where the answer is kept,
	// and whether to ask at all.
	return withDefaults(Config{
		CachePath: defaultCachePath(),
		Disabled:  !buildinfo.IsRelease() || OptedOut(),
	})
}

// OptedOut reports whether the Reviewer has turned the check off. Any value but
// the ones that plainly mean "no" counts, so DBN_NO_UPDATE_CHECK=1 — and =true,
// and =yes — all do what they look like they do.
func OptedOut() bool {
	switch os.Getenv(OptOutEnv) {
	case "", "0", "false", "no":
		return false
	default:
		return true
	}
}

// Check reports the newest published release, from the cache when the last
// answer is still fresh and from GitHub otherwise. A check that cannot run at
// all — disabled, or a development build — is not an error: it reports nothing
// available, which is all a caller needs to stay quiet.
func Check(ctx context.Context, cfg Config) (Result, error) {
	cfg = withDefaults(cfg)
	if cfg.Disabled {
		return Result{}, nil
	}

	remembered, ok := readCache(cfg.CachePath)
	if ok && !cfg.Force && remembered.fresh(cfg.Now()) {
		if remembered.Error != "" {
			// Still inside the retry window of a failure: report the same reason
			// rather than asking again, so an offline machine is asked once an hour
			// and not once a screen.
			return Result{}, errors.New(remembered.Error)
		}
		return cfg.result(remembered.Latest, remembered.Tag), nil
	}

	tag, err := fetchLatest(ctx, cfg)
	if err != nil {
		writeCache(cfg.CachePath, cached{CheckedAt: cfg.Now(), Error: err.Error()})
		return Result{}, err
	}
	latest := buildinfo.Normalize(tag)
	writeCache(cfg.CachePath, cached{CheckedAt: cfg.Now(), Latest: latest, Tag: tag})
	return cfg.result(latest, tag), nil
}

func (c Config) result(latest, tag string) Result {
	return Result{Latest: latest, Tag: tag, Available: buildinfo.Newer(latest, c.Current)}
}

func withDefaults(cfg Config) Config {
	if cfg.Current == "" {
		cfg.Current = buildinfo.Version()
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = Endpoint
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: requestTimeout}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return cfg
}

// fetchLatest reads the tag of the release GitHub calls latest.
func fetchLatest(ctx context.Context, cfg Config) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.Endpoint, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/vnd.github+json")

	response, err := cfg.Client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub answered %s", response.Status)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("could not read the release: %w", err)
	}
	tag := strings.TrimSpace(payload.TagName)
	if tag == "" {
		return "", errors.New("the latest release has no version tag")
	}
	return tag, nil
}

// cached is the remembered answer: when dbn last asked, what it heard, and — if
// the ask failed — why, so the same reason can be repeated without asking again.
type cached struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest,omitempty"`
	Tag       string    `json:"tag,omitempty"`
	Error     string    `json:"error,omitempty"`
}

func (c cached) fresh(now time.Time) bool {
	window := freshAfterSuccess
	if c.Error != "" {
		window = freshAfterFailure
	}
	age := now.Sub(c.CheckedAt)
	// A cache stamped in the future (a clock that moved) is not trusted to be
	// fresh; asking again is cheap and the answer is then stamped sanely.
	return age >= 0 && age < window
}

func defaultCachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "" // no cache directory: check every time rather than not at all
	}
	return filepath.Join(dir, "dbn", "update-check.json")
}

func readCache(path string) (cached, bool) {
	if path == "" {
		return cached{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return cached{}, false
	}
	var out cached
	if err := json.Unmarshal(raw, &out); err != nil {
		return cached{}, false
	}
	return out, true
}

// writeCache is best-effort: a cache that cannot be written costs an extra
// request, which is not worth failing a check — let alone a TUI — over.
func writeCache(path string, entry cached) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, raw, 0o644)
}

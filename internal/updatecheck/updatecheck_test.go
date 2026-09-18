package updatecheck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// releaseServer answers like the GitHub latest-release API, counting how often
// it was asked — the check's whole job is to ask rarely.
type releaseServer struct {
	*httptest.Server
	calls int
}

func newReleaseServer(t *testing.T, tag string) *releaseServer {
	t.Helper()
	rs := &releaseServer{}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rs.calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": tag})
	}))
	t.Cleanup(rs.Close)
	return rs
}

// refusingServer fails the test if it is ever asked anything.
func refusingServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("the check made a network call it should not have made")
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(server.Close)
	return server
}

func testConfig(t *testing.T, endpoint, current string) Config {
	t.Helper()
	return Config{
		Current:   current,
		Endpoint:  endpoint,
		CachePath: filepath.Join(t.TempDir(), "update-check.json"),
		Now:       time.Now,
	}
}

func TestANewerReleaseIsReportedAsAvailable(t *testing.T) {
	server := newReleaseServer(t, "v0.2.0")

	result, err := Check(context.Background(), testConfig(t, server.URL, "0.1.0"))

	if err != nil {
		t.Fatalf("the check failed: %v", err)
	}
	if !result.Available {
		t.Error("0.2.0 was not reported as available to a 0.1.0 build")
	}
	if result.Latest != "0.2.0" {
		t.Errorf("the latest release is reported as %q, want the bare version 0.2.0", result.Latest)
	}
}

func TestTheSameOrAnOlderReleaseIsNotAvailable(t *testing.T) {
	for _, current := range []string{"0.2.0", "0.3.0"} {
		server := newReleaseServer(t, "v0.2.0")

		result, err := Check(context.Background(), testConfig(t, server.URL, current))

		if err != nil {
			t.Fatalf("the check failed: %v", err)
		}
		if result.Available {
			t.Errorf("0.2.0 was offered to a %s build", current)
		}
	}
}

func TestADisabledCheckMakesNoNetworkCall(t *testing.T) {
	server := refusingServer(t)
	cfg := testConfig(t, server.URL, "0.1.0")
	cfg.Disabled = true

	result, err := Check(context.Background(), cfg)

	if err != nil {
		t.Errorf("a disabled check reported an error: %v", err)
	}
	if result.Available || result.Latest != "" {
		t.Errorf("a disabled check reported %+v, want nothing", result)
	}
}

func TestASuccessfulAnswerIsReusedForADayThenRefreshed(t *testing.T) {
	server := newReleaseServer(t, "v0.2.0")
	cfg := testConfig(t, server.URL, "0.1.0")

	first, err := Check(context.Background(), cfg)
	if err != nil {
		t.Fatalf("the first check failed: %v", err)
	}

	// An hour later the answer still stands, and nothing is asked.
	cfg.Now = func() time.Time { return time.Now().Add(time.Hour) }
	second, err := Check(context.Background(), cfg)
	if err != nil {
		t.Fatalf("the cached check failed: %v", err)
	}
	if second != first {
		t.Errorf("the cached answer %+v differs from the fresh one %+v", second, first)
	}
	if server.calls != 1 {
		t.Errorf("the check asked %d times within the day, want 1", server.calls)
	}

	// A day later it asks again.
	cfg.Now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	if _, err := Check(context.Background(), cfg); err != nil {
		t.Fatalf("the refreshed check failed: %v", err)
	}
	if server.calls != 2 {
		t.Errorf("the check asked %d times in all, want 2 once the day was up", server.calls)
	}
}

func TestForceAsksAgainEvenWithAFreshAnswer(t *testing.T) {
	server := newReleaseServer(t, "v0.2.0")
	cfg := testConfig(t, server.URL, "0.1.0")
	if _, err := Check(context.Background(), cfg); err != nil {
		t.Fatalf("the first check failed: %v", err)
	}

	cfg.Force = true
	if _, err := Check(context.Background(), cfg); err != nil {
		t.Fatalf("the forced check failed: %v", err)
	}

	if server.calls != 2 {
		t.Errorf("the forced check asked %d times, want 2", server.calls)
	}
}

func TestAFailedCheckIsNotRetriedForAnHour(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer failing.Close()
	cfg := testConfig(t, failing.URL, "0.1.0")

	if _, err := Check(context.Background(), cfg); err == nil {
		t.Fatal("expected a failing endpoint to report an error")
	}

	// Within the hour, the remembered reason is repeated without asking again.
	refusing := refusingServer(t)
	cfg.Endpoint = refusing.URL
	cfg.Now = func() time.Time { return time.Now().Add(30 * time.Minute) }
	if _, err := Check(context.Background(), cfg); err == nil {
		t.Error("expected the remembered failure to be reported again")
	}

	// Past the hour it tries the endpoint again — which now answers.
	recovered := newReleaseServer(t, "v0.2.0")
	cfg.Endpoint = recovered.URL
	cfg.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	result, err := Check(context.Background(), cfg)
	if err != nil {
		t.Fatalf("the check after the retry window failed: %v", err)
	}
	if !result.Available {
		t.Error("the check after the retry window found nothing")
	}
}

func TestTheCacheRemembersWhenAndWhat(t *testing.T) {
	server := newReleaseServer(t, "v0.2.0")
	cfg := testConfig(t, server.URL, "0.1.0")

	if _, err := Check(context.Background(), cfg); err != nil {
		t.Fatalf("the check failed: %v", err)
	}

	raw, err := os.ReadFile(cfg.CachePath)
	if err != nil {
		t.Fatalf("the check wrote no cache: %v", err)
	}
	var entry cached
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("the cache is not readable: %v", err)
	}
	if entry.Latest != "0.2.0" {
		t.Errorf("the cache remembers latest=%q, want 0.2.0", entry.Latest)
	}
	if entry.CheckedAt.IsZero() {
		t.Error("the cache remembers no check time")
	}
}

func TestAnUnreadableCacheIsJustACacheMiss(t *testing.T) {
	server := newReleaseServer(t, "v0.2.0")
	cfg := testConfig(t, server.URL, "0.1.0")
	if err := os.WriteFile(cfg.CachePath, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Check(context.Background(), cfg)

	if err != nil {
		t.Fatalf("a corrupt cache broke the check: %v", err)
	}
	if !result.Available {
		t.Error("a corrupt cache stopped the check finding the release")
	}
}

func TestOptedOutReadsTheEnvironmentTheWayItLooks(t *testing.T) {
	cases := map[string]bool{"1": true, "true": true, "yes": true, "": false, "0": false, "false": false, "no": false}

	for value, want := range cases {
		t.Setenv(OptOutEnv, value)

		if got := OptedOut(); got != want {
			t.Errorf("%s=%q opted out = %v, want %v", OptOutEnv, value, got, want)
		}
	}
}

// A development build has no published release it is meaningfully behind, and
// its binary was not downloaded in the first place — so it must never ask.
func TestTheDefaultConfigIsDisabledOnADevelopmentBuild(t *testing.T) {
	t.Setenv(OptOutEnv, "")

	if !DefaultConfig().Disabled {
		t.Error("the update check is live on a development build")
	}
}

package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/buildinfo"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

func status(t *testing.T, baseURL string) daemon.StatusWire {
	t.Helper()
	var out daemon.StatusWire
	if err := json.Unmarshal([]byte(get(t, baseURL+"/status")), &out); err != nil {
		t.Fatalf("could not decode the status: %v", err)
	}
	return out
}

// Telling "no daemon" from "a daemon too old to answer" is the whole reason
// FetchStatus has an error of its own: the second is what someone updating from
// a release before the endpoint existed is running, and it deserves to be said.
func TestFetchStatusTellsAnOldDaemonFromNoDaemon(t *testing.T) {
	ctx := context.Background()

	live := httptest.NewServer(daemon.New().Handler())
	defer live.Close()
	if _, err := daemon.FetchStatus(ctx, live.URL); err != nil {
		t.Errorf("a current daemon could not be read: %v", err)
	}

	old := httptest.NewServer(http.NotFoundHandler())
	defer old.Close()
	if _, err := daemon.FetchStatus(ctx, old.URL); !errors.Is(err, daemon.ErrNoStatus) {
		t.Errorf("a daemon with no status endpoint reported %v, want ErrNoStatus", err)
	}

	gibberish := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "not json")
	}))
	defer gibberish.Close()
	if _, err := daemon.FetchStatus(ctx, gibberish.URL); !errors.Is(err, daemon.ErrNoStatus) {
		t.Errorf("an unreadable answer reported %v, want ErrNoStatus", err)
	}

	nothing := httptest.NewServer(nil)
	nothing.Close() // closed: nothing is listening on that port any more
	_, err := daemon.FetchStatus(ctx, nothing.URL)
	if err == nil {
		t.Fatal("a port with nothing on it reported a status")
	}
	if errors.Is(err, daemon.ErrNoStatus) {
		t.Error("nothing listening was reported as a daemon that could not answer")
	}
}

func TestStatusReportsNoActiveReviewOnAFreshDaemon(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()

	reported := status(t, server.URL)

	if reported.ActiveReview {
		t.Error("a daemon with no Round posted reports a review active")
	}
	if reported.Version != buildinfo.Version() {
		t.Errorf("status reports version %q, want the build stamp %q", reported.Version, buildinfo.Version())
	}
	if reported.Executable == "" {
		t.Error("status reports no executable path")
	}
}

// The active-review flag is what stops `dbn update` restarting a daemon out from
// under a Reviewer mid-Round, so it has to follow the review's real life.
func TestStatusReportsAnActiveReviewUntilItConcludes(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)

	id := postRound(t, server.URL, minimalRound(root)).ReviewID

	if !status(t, server.URL).ActiveReview {
		t.Error("a posted Round does not report as an active review")
	}

	concluded := decodeResult[concludeOutcome](t, callTool(t, server.URL, "conclude", map[string]any{"review_id": id}))
	if !concluded.Concluded {
		t.Fatalf("could not conclude the review: %s", concluded.Message)
	}

	if status(t, server.URL).ActiveReview {
		t.Error("a concluded review still reports as active")
	}
}

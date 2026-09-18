package daemon_test

import (
	"encoding/json"
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

func TestStatusReportsNoActiveReviewOnAFreshDaemon(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()

	reported := status(t, server.URL)

	if reported.ActiveReview {
		t.Error("a daemon with no Walkthrough posted reports a review active")
	}
	if reported.Version != buildinfo.Version() {
		t.Errorf("status reports version %q, want the build stamp %q", reported.Version, buildinfo.Version())
	}
	if reported.Executable == "" {
		t.Error("status reports no executable path")
	}
}

// The active-review flag is what stops `dbn update` restarting a daemon out from
// under a Reviewer mid-Walkthrough, so it has to follow the review's real life.
func TestStatusReportsAnActiveReviewUntilItConcludes(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)

	id := postWalkthrough(t, server.URL, minimalWalkthrough(root)).ReviewID

	if !status(t, server.URL).ActiveReview {
		t.Error("a posted Walkthrough does not report as an active review")
	}

	concluded := decodeResult[concludeOutcome](t, callTool(t, server.URL, "conclude", map[string]any{"review_id": id}))
	if !concluded.Concluded {
		t.Fatalf("could not conclude the review: %s", concluded.Message)
	}

	if status(t, server.URL).ActiveReview {
		t.Error("a concluded review still reports as active")
	}
}

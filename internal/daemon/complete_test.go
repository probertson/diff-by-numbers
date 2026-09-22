package daemon_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// TestAPostAfterACompleteReviewStartsANewOneThroughTheDaemon is the tester's
// report, end to end: once a round is handed off with nothing raised,
// fetch_results calls the review complete, and the next post is a new review —
// not a Revision Round pre-marked against the finished one, which let new files
// escape coverage and left every later post refused as walkthrough_active.
func TestAPostAfterACompleteReviewStartsANewOneThroughTheDaemon(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	walkthrough := map[string]any{
		"label":        "LABEL-the-review",
		"brief":        map[string]any{"goal": "x", "approach": "y"},
		"repositories": []any{map[string]any{"root": root, "base": "main"}},
		"steps": []any{map[string]any{
			"name": "all", "explanation": "e",
			"excerpts":         []any{map[string]any{"file": "FILE-src/fetch.ts", "side": "new", "first_line": 1, "last_line": 4}},
			"acknowledgements": []any{map[string]any{"files": []any{"LOCKFILE"}, "reason": "generated"}},
		}},
	}
	first := postRound(t, server.URL, walkthrough)
	httpPost(t, server.URL+"/finish")

	second := postRound(t, server.URL, walkthrough)

	if second.ReviewID == first.ReviewID {
		t.Errorf("expected a new review after a complete one, got the same id %q", first.ReviewID)
	}
	if !strings.HasPrefix(second.Message, "Posted.") {
		t.Errorf("expected the post announced as a new review, got %q", second.Message)
	}
}

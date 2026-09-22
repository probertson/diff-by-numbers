package daemon_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

type fetchOutcome struct {
	Posted   bool   `json:"posted"`
	Finished bool   `json:"finished"`
	Message  string `json:"message"`
}

func fetchResults(t *testing.T, baseURL, reviewID string) fetchOutcome {
	t.Helper()
	return decodeResult[fetchOutcome](t, callTool(t, baseURL, "fetch_results", map[string]any{"review_id": reviewID}))
}

// TestFetchResultsSpeaksOfHandingOff holds the agent's advisory to the same
// vocabulary the Reviewer sees (#55): the turn boundary is a hand-off, not a
// finish, so the agent and the human describe the same moment the same way.
func TestFetchResultsSpeaksOfHandingOff(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)

	posted := postRound(t, server.URL, minimalRound(root))

	waiting := fetchResults(t, server.URL, posted.ReviewID)

	if !strings.Contains(waiting.Message, "has not handed off") {
		t.Errorf("expected the advisory to say the Reviewer has not handed off, got: %s", waiting.Message)
	}

	httpPost(t, server.URL+"/goto/1")
	raiseComment(t, server.URL, 0, 4, 4, "please rename this")
	httpPost(t, server.URL+"/finish")

	handed := fetchResults(t, server.URL, posted.ReviewID)

	if !strings.Contains(handed.Message, "has handed off") {
		t.Errorf("expected the advisory to say the Reviewer has handed off, got: %s", handed.Message)
	}
	if strings.Contains(handed.Message, "finish") {
		t.Errorf("the advisory should not fall back on finish, got: %s", handed.Message)
	}
}

func TestFetchResultsCallsAHandOffWithNothingRaisedComplete(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)

	posted := postRound(t, server.URL, minimalRound(root))
	httpPost(t, server.URL+"/finish")

	complete := fetchResults(t, server.URL, posted.ReviewID)

	if !strings.Contains(complete.Message, "handed off having raised nothing") {
		t.Errorf("expected the complete advisory to name the hand-off, got: %s", complete.Message)
	}
	if !strings.Contains(complete.Message, "the review is complete") {
		t.Errorf("expected the complete advisory to say the review is complete, got: %s", complete.Message)
	}
}

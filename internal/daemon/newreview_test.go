package daemon_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// After an explicit conclude, the next post is a new Review: a new id, and
// nothing carried over from the one that ended.
func TestAPostAfterAConcludeStartsANewReview(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	first := postRound(t, server.URL, minimalRound(root))
	httpPost(t, server.URL+"/goto/1")
	raiseComment(t, server.URL, 0, 4, 4, "never answered")
	callTool(t, server.URL, "conclude", map[string]any{"review_id": first.ReviewID})

	second := postRound(t, server.URL, minimalRound(root))

	if second.ReviewID == first.ReviewID {
		t.Errorf("expected a new review after a conclude, got the same id %q", first.ReviewID)
	}
	if view := getView(t, server.URL); len(view.Comments) != 0 || len(view.Dispositions) != 0 {
		t.Errorf("a new review starts with nothing carried from the concluded one, got %+v", view)
	}
}

// Dismissing a Review frees the daemon for the next post, which is a new Review.
func TestAPostAfterADismissalStartsANewReview(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	first := postRound(t, server.URL, minimalRound(root))
	httpPost(t, server.URL+"/abandon")

	second := postRound(t, server.URL, minimalRound(root))

	if second.ReviewID == first.ReviewID {
		t.Errorf("expected a new review after a Dismissal, got the same id %q", first.ReviewID)
	}
}

// A refused post leaves a concluded Review answerable: the agent can still
// fetch its results, and nothing about it has been wiped.
func TestARefusedPostAfterAHandOffLeavesTheConcludedReviewAnswerable(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	posted := postRound(t, server.URL, minimalRound(root))
	httpPost(t, server.URL+"/finish")
	broken := minimalRound(root)
	broken["steps"] = broken["steps"].([]any)[:1]

	refused := decodeResult[postOutcome](t, callTool(t, server.URL, "post_round", broken))

	if refused.Accepted || !refused.has("uncovered_changes") {
		t.Fatalf("new work gets no pre-marking from the finished review, so leaving the lockfile out is uncovered_changes; got accepted=%v %s", refused.Accepted, refused.summary())
	}
	if message := fetchResults(t, server.URL, posted.ReviewID).Message; !strings.Contains(message, "complete") {
		t.Errorf("the concluded review should still report complete, got %q", message)
	}
}

// After a hand-off with nothing raised, new work is round 1 of a new Review, not
// a Revision Round of the finished one: it gets no pre-marking to hide behind.
func TestAPostAfterAHandOffWithNothingRaisedIsRoundOne(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	postRound(t, server.URL, minimalRound(root))
	httpPost(t, server.URL+"/finish")

	postRound(t, server.URL, minimalRound(root))

	var view struct {
		Round int `json:"round"`
	}
	if err := json.Unmarshal([]byte(get(t, server.URL+"/view")), &view); err != nil {
		t.Fatalf("decode /view: %v", err)
	}
	if view.Round != 1 {
		t.Errorf("expected round 1 of a new review, got round %d", view.Round)
	}
}

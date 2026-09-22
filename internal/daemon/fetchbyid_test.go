package daemon_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

type fetchByID struct {
	Posted      bool          `json:"posted"`
	Message     string        `json:"message"`
	Goal        string        `json:"goal"`
	Problems    []problemJSON `json:"problems"`
	OpenReviews []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		State string `json:"state"`
	} `json:"open_reviews"`
}

func fetchByReviewID(t *testing.T, baseURL string, args map[string]any) fetchByID {
	t.Helper()
	return decodeResult[fetchByID](t, callTool(t, baseURL, "fetch_results", args))
}

func TestFetchResultsAnswersTheReviewItNames(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	posted := postRound(t, server.URL, minimalRound(root))

	results := fetchByReviewID(t, server.URL, map[string]any{"review_id": posted.ReviewID})

	if !results.Posted || len(results.Problems) != 0 {
		t.Errorf("expected the named Review's results, got %+v", results)
	}
}

// An agent that lost its id — to compaction, usually — finds it here: calling
// without one is refused, and the refusal lists what is open (ADR-0015).
func TestFetchResultsWithoutAnIDListsTheOpenReviews(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	labelled := minimalRound(root)
	labelled["label"] = "LABEL-auth-refactor"
	posted := postRound(t, server.URL, labelled)

	results := fetchByReviewID(t, server.URL, map[string]any{})

	if results.Posted {
		t.Fatalf("a fetch naming no Review should be refused, got %+v", results)
	}
	if len(results.OpenReviews) != 1 {
		t.Fatalf("expected the one open Review listed, got %+v", results.OpenReviews)
	}
	open := results.OpenReviews[0]
	if open.ID != posted.ReviewID || open.Label != "LABEL-auth-refactor" || open.State == "" {
		t.Errorf("expected the open Review listed by id, label and state, got %+v", open)
	}
}

func TestFetchResultsWithoutAnIDSaysSoWhenNothingIsOpen(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()

	results := fetchByReviewID(t, server.URL, map[string]any{})

	if results.Posted || len(results.OpenReviews) != 0 {
		t.Fatalf("expected nothing open, got %+v", results)
	}
	if !strings.Contains(results.Message, "no review") {
		t.Errorf("expected the message to say no review is open, got %q", results.Message)
	}
}

func TestFetchResultsWithAnUnknownIDSaysSo(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	postRound(t, server.URL, minimalRound(root))

	results := fetchByReviewID(t, server.URL, map[string]any{"review_id": "no-such-id"})

	if results.Posted {
		t.Fatalf("an unknown id has no results, got %+v", results)
	}
	if !strings.Contains(results.Message, "no-such-id") {
		t.Errorf("expected the unknown id named in the message, got %q", results.Message)
	}
}

// The whole loop with explicit ids, as an agent runs it: post, the Reviewer
// raises a Comment and hands off, fetch by id, then a Revision Round naming the
// same id (ADR-0015).
func TestTheLoopRunsOnExplicitIDs(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	reviewID := handedOffWithAComment(t, server.URL, root, "GOAL-first")

	results := fetchByReviewID(t, server.URL, map[string]any{"review_id": reviewID})
	revised := postRound(t, server.URL, revisionWithBrief(root, reviewID, map[string]any{"approach": "renamed it"}))

	if !results.Posted || !strings.Contains(results.Message, "handed off") {
		t.Errorf("expected the handed-off Review's results, got %+v", results)
	}
	if revised.ReviewID != reviewID {
		t.Errorf("the Revision Round belongs to the Review it named, got %q", revised.ReviewID)
	}
}

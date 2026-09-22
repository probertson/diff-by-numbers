package daemon_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

type inboxRow struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	State string `json:"state"`
}

func inbox(t *testing.T, baseURL string) []inboxRow {
	t.Helper()
	var listed struct {
		Reviews []inboxRow `json:"reviews"`
	}
	if err := json.Unmarshal([]byte(get(t, baseURL+"/inbox")), &listed); err != nil {
		t.Fatalf("decode /inbox: %v", err)
	}
	return listed.Reviews
}

// viewOf is what a window draws for one review: the Reviewer's position in it
// and what they have raised.
func viewOf(t *testing.T, baseURL, reviewID string) struct {
	Position int `json:"position"`
	Comments []struct {
		ID int `json:"id"`
	} `json:"comments"`
} {
	t.Helper()
	var view struct {
		Position int `json:"position"`
		Comments []struct {
			ID int `json:"id"`
		} `json:"comments"`
	}
	if err := json.Unmarshal([]byte(get(t, reviewURL(baseURL, reviewID)+"/view")), &view); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	return view
}

// labelledRound is a Round the Reviewer can tell apart in the Inbox.
func labelledRound(root, label string) map[string]any {
	round := minimalRound(root)
	round["label"] = label
	return round
}

// Several agent sessions posting at once is the workflow dbn exists for
// (ADR-0015): neither waits on the other.
func TestTwoReviewsAreBothHeldAndBothListed(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)

	first := postRound(t, server.URL, labelledRound(root, "LABEL-auth"))
	second := postRound(t, server.URL, labelledRound(root, "LABEL-billing"))

	if first.ReviewID == second.ReviewID {
		t.Fatal("two posts naming no review are two reviews, with two ids")
	}
	rows := inbox(t, server.URL)
	if len(rows) != 2 {
		t.Fatalf("expected both reviews in the Inbox, got %+v", rows)
	}
	if rows[0].ID != first.ReviewID || rows[0].Label != "LABEL-auth" {
		t.Errorf("expected the first review listed first, got %+v", rows)
	}
	if rows[1].ID != second.ReviewID || rows[1].Label != "LABEL-billing" {
		t.Errorf("expected the second review listed second, got %+v", rows)
	}
}

// Which Review the Reviewer is working is the window's business, so every
// Reviewer call names the Review it acts on.
func TestReviewerActionsReachOnlyTheReviewTheyName(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	first := postRound(t, server.URL, labelledRound(root, "LABEL-auth"))
	second := postRound(t, server.URL, labelledRound(root, "LABEL-billing"))

	httpPost(t, reviewURL(server.URL, first.ReviewID)+"/goto/1")
	raiseComment(t, reviewURL(server.URL, first.ReviewID), 0, 4, 4, "a point on the first")

	moved := viewOf(t, server.URL, first.ReviewID)
	untouched := viewOf(t, server.URL, second.ReviewID)
	if moved.Position != 1 || len(moved.Comments) != 1 {
		t.Errorf("expected the named review moved and commented, got position %d with %d Comments", moved.Position, len(moved.Comments))
	}
	if untouched.Position != 0 || len(untouched.Comments) != 0 {
		t.Errorf("the other review should be untouched, got position %d with %d Comments", untouched.Position, len(untouched.Comments))
	}
}

func TestTheInboxStatesWhoEachReviewIsWaitingOn(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	postRound(t, server.URL, labelledRound(root, "LABEL-fresh"))
	opened := postRound(t, server.URL, labelledRound(root, "LABEL-opened"))
	handedOff := postRound(t, server.URL, labelledRound(root, "LABEL-handed-off"))

	viewOf(t, server.URL, opened.ReviewID)
	httpPost(t, reviewURL(server.URL, handedOff.ReviewID)+"/goto/1")
	raiseComment(t, reviewURL(server.URL, handedOff.ReviewID), 0, 4, 4, "a point")
	httpPost(t, reviewURL(server.URL, handedOff.ReviewID)+"/finish")

	states := map[string]string{}
	for _, row := range inbox(t, server.URL) {
		states[row.Label] = row.State
	}
	want := map[string]string{"LABEL-fresh": "new", "LABEL-opened": "needs_you", "LABEL-handed-off": "waiting_on_agent"}
	for label, state := range want {
		if states[label] != state {
			t.Errorf("expected %s to read %q, got %q", label, state, states[label])
		}
	}
	if states["LABEL-fresh"] == states["LABEL-opened"] {
		t.Error("a review never opened reads differently from one left part-way")
	}
}

// A concluded Review leaves the Inbox at once — the Reviewer is done with it —
// but the daemon holds it until the agent has its results.
func TestAConcludedReviewLeavesTheInboxAndIsReleasedOnceFetched(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	posted := postRound(t, server.URL, minimalRound(root))
	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/finish")

	if rows := inbox(t, server.URL); len(rows) != 0 {
		t.Fatalf("a review handed off with nothing raised is concluded and leaves the Inbox, got %+v", rows)
	}
	results := fetchByReviewID(t, server.URL, map[string]any{"review_id": posted.ReviewID})
	if !results.Posted {
		t.Fatalf("the concluded review answers until its results are fetched, got %+v", results)
	}

	after := fetchByReviewID(t, server.URL, map[string]any{"review_id": posted.ReviewID})

	if after.Posted {
		t.Errorf("once fetched, the review is released and its id is unknown, got %+v", after)
	}
}

func TestConcludingReleasesTheReviewWithoutAFetch(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	posted := postRound(t, server.URL, minimalRound(root))

	callTool(t, server.URL, "conclude", map[string]any{"review_id": posted.ReviewID})

	if rows := inbox(t, server.URL); len(rows) != 0 {
		t.Errorf("a concluded review leaves the Inbox, got %+v", rows)
	}
	if fetchByReviewID(t, server.URL, map[string]any{"review_id": posted.ReviewID}).Posted {
		t.Error("an agent that concluded its review has said it wants nothing more")
	}
}

// `dbn update` and self-exit both read this: any review still going keeps the
// daemon busy, and nothing does not.
func TestStatusIsBusyWhileAnyReviewIsOpen(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	first := postRound(t, server.URL, labelledRound(root, "LABEL-auth"))
	second := postRound(t, server.URL, labelledRound(root, "LABEL-billing"))

	httpPost(t, reviewURL(server.URL, first.ReviewID)+"/finish")
	callTool(t, server.URL, "fetch_results", map[string]any{"review_id": first.ReviewID})

	if !activeReview(t, server.URL) {
		t.Fatal("the second review is still open, so the daemon is busy")
	}

	httpPost(t, reviewURL(server.URL, second.ReviewID)+"/finish")
	callTool(t, server.URL, "fetch_results", map[string]any{"review_id": second.ReviewID})

	if activeReview(t, server.URL) {
		t.Error("with every review released the daemon is idle")
	}
}

func activeReview(t *testing.T, baseURL string) bool {
	t.Helper()
	var status struct {
		ActiveReview bool `json:"active_review"`
	}
	if err := json.Unmarshal([]byte(get(t, baseURL+"/status")), &status); err != nil {
		t.Fatalf("decode /status: %v", err)
	}
	return status.ActiveReview
}

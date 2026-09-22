package daemon_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

type dismissedFetch struct {
	Posted    bool   `json:"posted"`
	Dismissed bool   `json:"dismissed"`
	Message   string `json:"message"`
	Comments  []struct {
		Note string `json:"note"`
	} `json:"comments"`
}

func TestADismissedReviewLeavesTheInboxAndLeavesOthersAlone(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	dismissed := postRound(t, server.URL, labelledRound(root, "LABEL-dismissed"))
	kept := postRound(t, server.URL, labelledRound(root, "LABEL-kept"))

	httpPost(t, reviewURL(server.URL, dismissed.ReviewID)+"/dismiss")

	rows := inbox(t, server.URL)
	if len(rows) != 1 || rows[0].ID != kept.ReviewID {
		t.Errorf("expected only the review that was not dismissed, got %+v", rows)
	}
	if !activeReview(t, server.URL) {
		t.Error("the review left behind still needs the daemon")
	}
}

// The agent hears that the Reviewer dismissed its review — with what they had
// raised before they did — rather than that its id never existed (ADR-0015).
func TestFetchResultsReportsADismissalOnceThenForgetsIt(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	posted := postRound(t, server.URL, minimalRound(root))
	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/goto/1")
	raiseComment(t, reviewURL(server.URL, posted.ReviewID), 0, 4, 4, "this needs a rename")
	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/dismiss")

	told := decodeResult[dismissedFetch](t, callTool(t, server.URL, "fetch_results", map[string]any{"review_id": posted.ReviewID}))

	if !told.Dismissed {
		t.Fatalf("expected the Dismissal reported, got %+v", told)
	}
	if !strings.Contains(told.Message, "dismissed") {
		t.Errorf("expected the message to say the Reviewer dismissed it, got %q", told.Message)
	}
	if len(told.Comments) != 1 || told.Comments[0].Note != "this needs a rename" {
		t.Errorf("expected the Comments raised before the Dismissal, got %+v", told.Comments)
	}

	again := decodeResult[dismissedFetch](t, callTool(t, server.URL, "fetch_results", map[string]any{"review_id": posted.ReviewID}))

	if again.Dismissed || !strings.Contains(again.Message, "no review with id") {
		t.Errorf("once told, the id is forgotten, got %+v", again)
	}
}

func TestADismissedReviewLeavesTheDaemonIdle(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	posted := postRound(t, server.URL, minimalRound(root))

	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/dismiss")

	if activeReview(t, server.URL) {
		t.Error("a dismissed review is not one the daemon is busy with")
	}
}

// Dismissal is the Reviewer's act: an agent cannot discard a review of its own
// work, so there is no tool for it.
func TestTheAgentCannotDismissItsOwnReview(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	postRound(t, server.URL, minimalRound(root))

	for _, tool := range toolsOf(t, server.URL) {
		if strings.Contains(tool, "dismiss") || strings.Contains(tool, "abandon") {
			t.Errorf("there should be no tool for an agent to discard a review with, got %q", tool)
		}
	}
}

// toolsOf is the names of the tools the daemon offers an agent.
func toolsOf(t *testing.T, baseURL string) []string {
	t.Helper()
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: baseURL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("could not connect to the daemon: %v", err)
	}
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("could not list tools: %v", err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	return names
}

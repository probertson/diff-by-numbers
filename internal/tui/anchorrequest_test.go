package tui

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// The comment modal and copying an Anchor both ask the daemon to compose the
// Anchor for a selection. Against a real daemon, since the request has to name
// the review it is about, and a stub that answers any path would not notice
// when it stops doing so.
func TestASelectionIsComposedIntoAnAnchorByARealDaemon(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	t.Cleanup(server.Close)
	reviewID := postedReview(t, server.URL)
	reviewerAct(t, server.URL, reviewID, "goto/1", nil)
	run := selectedRun{ack: -1, excerpt: 0, start: rowRef{side: "new", line: 3}, end: rowRef{side: "new", line: 4}, rows: 2}

	anchor, ok := client{base: server.URL}.on(reviewID).composeAnchor(run)

	if !ok {
		t.Fatal("the daemon did not compose an Anchor for the selection")
	}
	if !strings.Contains(anchor, "ADDED") {
		t.Errorf("expected the Anchor to quote the selected code, got:\n%s", anchor)
	}
}

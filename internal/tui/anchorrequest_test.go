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

// A Comment on a row of a table, in a Document's formatted view, carries the
// table's header row so the agent can read the row's columns: what the TUI
// selects, the request it sends and the Anchor dbn composes, end to end.
func TestCommentingOnATableRowQuotesItsHeaderRow(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	t.Cleanup(server.Close)
	// A heading on line 1; the header row on 3, the divider on 4, rows on 5-7.
	table := "| Attempt | Delay |\n| ------- | ----- |\n| 1 | none |\n| 2 | 1s |\n| 3 | 2s |\n"
	root := repoChanging(t, "retries.md", "# Retries\n", "# Retries\n\n"+table)
	excerpt := func(first, last int) map[string]any {
		return map[string]any{"file": "retries.md", "side": "new", "first_line": first, "last_line": last}
	}
	reviewID := postedRound(t, server.URL, root,
		map[string]any{"name": "the last attempt", "explanation": "why", "excerpts": []any{excerpt(7, 7)}},
		map[string]any{"name": "the rest", "explanation": "why", "excerpts": []any{excerpt(2, 6)}},
	)
	reviewerAct(t, server.URL, reviewID, "goto/1", nil)
	m := model{client: client{base: server.URL}.on(reviewID), mode: modeReview, width: 80, height: 40, ready: true, note: newNote(80)}
	view, err := m.client.view()
	if err != nil {
		t.Fatalf("could not read the view: %v", err)
	}
	m.view = view
	m.syncCursor()
	m = press(m, "down") // past the header row, onto the row

	m = press(m, "c")

	for _, want := range []string{"after 7 ", "      3 | | Attempt | Delay |", "+     7 | | 3 | 2s |"} {
		if !strings.Contains(m.pendingCode, want) {
			t.Errorf("expected the Anchor to contain %q, got:\n%s", want, m.pendingCode)
		}
	}
}

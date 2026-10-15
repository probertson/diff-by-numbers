package daemon_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// noteAt is where a Comment's note is drawn on a stop, failing if it is not.
func noteAt(t *testing.T, out, note string) int {
	t.Helper()
	at := strings.Index(out, `<aside class="comment">`)
	if at < 0 || !strings.Contains(out[at:], note) {
		t.Fatalf("expected the Comment's note drawn, got:\n%s", out)
	}
	return at
}

// The TUI shows a Comment beneath the block it is on; so does the page, rather
// than after the whole run of the Document the block is drawn in.
func TestACommentsNoteFollowsItsBlockInTheDocumentsFlow(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onANewDocument(t, server.URL, "# Title\n\nFirst paragraph.\n\nSecond paragraph.\n")
	raiseComment(t, reviewURL(server.URL, reviewID), 0, 3, 3, "about the first")

	out := stop(t, server.URL, reviewID)

	note := noteAt(t, out, "about the first")
	if note < strings.Index(out, "First paragraph.") || note > strings.Index(out, "Second paragraph.") {
		t.Errorf("expected the note between its paragraph and the next, got:\n%s", out)
	}
}

// A list item's note is drawn in the item, under its text, so the list stays
// one list.
func TestACommentOnAListItemIsDrawnInTheItem(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onANewDocument(t, server.URL, "- one\n- two\n- three\n")
	raiseComment(t, reviewURL(server.URL, reviewID), 0, 2, 2, "about two")

	out := stop(t, server.URL, reviewID)

	note := noteAt(t, out, "about two")
	two := strings.Index(out, "two")
	if note < two || note > two+strings.Index(out[two:], "</li>") {
		t.Errorf("expected the note inside its item, after its text, got:\n%s", out)
	}
	if lists := strings.Count(out, "<ul"); lists != 1 {
		t.Errorf("expected the list kept whole, got %d lists in:\n%s", lists, out)
	}
}

// A table row cannot hold a note, so a row's note follows its table.
func TestACommentOnATableRowFollowsItsTable(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onANewDocument(t, server.URL, "| a | b |\n| - | - |\n| 1 | 2 |\n| 3 | 4 |\n\nAfter the table.\n")
	raiseComment(t, reviewURL(server.URL, reviewID), 0, 3, 3, "about the first row")

	out := stop(t, server.URL, reviewID)

	note := noteAt(t, out, "about the first row")
	if note < strings.Index(out, "</table>") || note > strings.Index(out, "After the table.") {
		t.Errorf("expected the note between its table and the next block, got:\n%s", out)
	}
	if tables := strings.Count(out, "<table"); tables != 1 {
		t.Errorf("expected the table kept whole, got %d tables in:\n%s", tables, out)
	}
}

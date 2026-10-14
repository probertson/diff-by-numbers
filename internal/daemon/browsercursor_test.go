package daemon_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// reportCursor tells dbn where the TUI's cursor is, as the TUI does each time
// it moves.
func reportCursor(t *testing.T, baseURL, reviewID string, cursor daemon.CursorWire) {
	t.Helper()
	body, _ := json.Marshal(cursor)
	response, err := http.Post(reviewURL(baseURL, reviewID)+"/cursor", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /cursor: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(response.Body)
		t.Fatalf("POST /cursor answered %s: %s", response.Status, b)
	}
}

// openingTag is the opening tag of the first element in out carrying attr, or
// "" when none does.
func openingTag(out, attr string) string {
	return regexp.MustCompile(`<[a-z0-9]+[^>]*\s` + regexp.QuoteMeta(attr) + `[^>]*>`).FindString(out)
}

// isTheCursors reports whether an element's opening tag marks it as where the
// TUI's cursor is.
func isTheCursors(tag string) bool {
	class := regexp.MustCompile(`class="([^"]*)"`).FindStringSubmatch(tag)
	return class != nil && strings.Contains(" "+class[1]+" ", " cursor ")
}

func TestACursorReportOnABlockEmitsAnEventAndMarksTheBlock(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	t.Cleanup(server.Close) // after the stream closes: a cleanup, not a defer
	reviewID := onANewDocument(t, server.URL, "# Retry policy\n\n"+
		"Retries are capped at three.\n\n"+
		"Each waits twice as long.\n")
	events := follow(t, server.URL, reviewID)

	reportCursor(t, server.URL, reviewID, daemon.CursorWire{Step: 1, Excerpt: 0, Block: 2})

	if got := nextEvent(events); got != "cursor" {
		t.Errorf("expected a cursor event after a cursor report, got %q", got)
	}
	out := stop(t, server.URL, reviewID)
	if tag := openingTag(out, `data-block="2"`); !isTheCursors(tag) {
		t.Errorf("expected block 2 marked as the cursor's, got %q in:\n%s", tag, out)
	}
	for _, other := range []string{`data-block="1"`, `data-block="3"`} {
		if tag := openingTag(out, other); isTheCursors(tag) {
			t.Errorf("expected only the cursor's block marked, but %q is too", tag)
		}
	}
}

// onTheEditedGuard posts a Round whose one Step shows a line of code
// replaced (old line 2 by new line 2), and moves onto it.
func onTheEditedGuard(t *testing.T, baseURL string) string {
	t.Helper()
	root := editedRepo(t)
	posted := postRound(t, baseURL, map[string]any{
		"label":        "LABEL-the-review",
		"brief":        map[string]any{"goal": "rework the guard", "background": "renamed it"},
		"repositories": []any{map[string]any{"root": root, "base": "main"}},
		"steps": []any{map[string]any{
			"name": "Rework the guard", "explanation": "one line became another",
			"excerpts": []any{map[string]any{
				"repository": root, "file": "fetch.ts", "side": "new", "first_line": 1, "last_line": 3,
			}},
		}},
	})
	httpPost(t, reviewURL(baseURL, posted.ReviewID)+"/advance")
	return posted.ReviewID
}

func TestACursorReportOnALineOfCodeMarksThatLine(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onTheEditedGuard(t, server.URL)

	reportCursor(t, server.URL, reviewID, daemon.CursorWire{Step: 1, Excerpt: 0, Side: "new", Line: 2})

	out := stop(t, server.URL, reviewID)
	if tag := openingTag(out, `data-side="new" data-line="2"`); !isTheCursors(tag) {
		t.Errorf("expected new line 2 marked as the cursor's, got %q in:\n%s", tag, out)
	}
	for _, other := range []string{`data-side="old" data-line="2"`, `data-side="new" data-line="1"`} {
		if tag := openingTag(out, other); tag == "" || isTheCursors(tag) {
			t.Errorf("expected %s drawn and not the cursor's, got %q", other, tag)
		}
	}
}

// A cursor the TUI reported on one Step means nothing on another, so the page
// draws it only on the Step it was reported at.
func TestACursorReportedOnAnotherStepMarksNothing(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onTheEditedGuard(t, server.URL)

	reportCursor(t, server.URL, reviewID, daemon.CursorWire{Step: 2, Excerpt: 0, Side: "new", Line: 2})

	out := stop(t, server.URL, reviewID)
	if tag := openingTag(out, `data-side="new" data-line="2"`); isTheCursors(tag) {
		t.Errorf("expected a cursor from another Step not drawn, got %q", tag)
	}
	if strings.Contains(out, "following") {
		t.Errorf("expected nothing dimmed for a cursor from another Step, got:\n%s", out)
	}
}

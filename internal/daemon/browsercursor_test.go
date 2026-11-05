package daemon_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
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

// A cursor event carries where the cursor moved to, which the page brightens
// without fetching anything: the cursor as reported, and the place it is at,
// which the block's element carries.
func TestACursorEventCarriesWhereTheCursorIs(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	t.Cleanup(server.Close)
	reviewID := onANewDocument(t, server.URL, "# Retry policy\n\n"+
		"Retries are capped at three.\n\n"+
		"Each waits twice as long.\n")
	events := follow(t, server.URL, reviewID)

	reportCursor(t, server.URL, reviewID, daemon.CursorWire{Step: 1, Excerpt: 0, Block: 2})

	event := nextEventWithData(events)
	var got struct {
		daemon.CursorWire
		Place string `json:"place"`
	}
	if err := json.Unmarshal([]byte(event.data), &got); err != nil || event.name != "cursor" {
		t.Fatalf("expected a cursor event carrying the cursor, got %+v (%v)", event, err)
	}
	if want := (daemon.CursorWire{Step: 1, Excerpt: 0, Block: 2}); got.CursorWire != want {
		t.Errorf("expected the event to carry the cursor %+v, got %+v", want, got.CursorWire)
	}
	if tag := openingTag(stop(t, server.URL, reviewID), `data-place="`+got.Place+`"`); !strings.Contains(tag, `data-block="2"`) {
		t.Errorf("expected the place %q to be block 2's, got %q", got.Place, tag)
	}
}

// A cursor report dbn cannot place is refused, as a screen report is.
func TestACursorReportNamingNoPlaceIsRefused(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onTheEditedGuard(t, server.URL)

	for _, report := range []string{
		`not json`,
		`{"step":1,"excerpt":0}`,
		`{"step":1,"excerpt":0,"side":"sideways","line":2}`,
		`{"step":0,"excerpt":-1}`,
	} {
		response, err := http.Post(reviewURL(server.URL, reviewID)+"/cursor", "application/json", strings.NewReader(report))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()

		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("expected the cursor report %s refused, got %s", report, response.Status)
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

// An edited block is drawn as track changes (#188), which the cursor marks as
// it marks a rendered block.
func TestACursorOnAnEditedBlockMarksItsTrackChanges(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onAnEditedDocument(t, server.URL,
		"# Retry policy\n\nA request is retried when the transport fails.\n",
		"# Retry policy\n\nA request is resent when the transport fails.\n",
		lines{"new", 1, 3})

	reportCursor(t, server.URL, reviewID, daemon.CursorWire{Step: 1, Excerpt: 0, Block: 2})

	out := stop(t, server.URL, reviewID)
	tag := openingTag(out, `data-treatment="edited"`)
	if !strings.Contains(tag, `data-block="2"`) || !isTheCursors(tag) {
		t.Errorf("expected the edited block 2 marked as the cursor's, got %q in:\n%s", tag, out)
	}
	if tag := openingTag(out, `data-block="1"`); isTheCursors(tag) {
		t.Errorf("expected only the cursor's block marked, but %q is too", tag)
	}
}

// A code block, fenced or indented, carries its block as any other block does,
// so the cursor on it marks it rather than marking nothing on the page.
func TestACursorOnACodeBlockMarksTheCodeBlock(t *testing.T) {
	for name, code := range map[string]string{
		"fenced":   "```text\nSugar: empty, because the bakery is closed.\n```\n",
		"indented": "    Sugar: empty, because the bakery is closed.\n",
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(daemon.New().Handler())
			defer server.Close()
			reviewID := onANewDocument(t, server.URL, "# Pantry\n\nAn empty jar gets a label:\n\n"+code+"\nThe season changes nothing.\n")

			reportCursor(t, server.URL, reviewID, daemon.CursorWire{Step: 1, Excerpt: 0, Block: 3})

			out := stop(t, server.URL, reviewID)
			tag := openingTag(out, `data-block="3"`)
			if !strings.HasPrefix(tag, "<pre") || !strings.Contains(tag, `data-place="1.0.b3"`) || !isTheCursors(tag) {
				t.Errorf("expected the code block marked as the cursor's, got %q in:\n%s", tag, out)
			}
			if !strings.Contains(out, "Sugar: empty, because the bakery is closed.") {
				t.Errorf("expected the code block's text drawn, got:\n%s", out)
			}
		})
	}
}

// An edited table row stays a row of its table, which the cursor marks as the
// row's, leaving the table holding it bright.
func TestACursorOnAnEditedTableRowMarksThatRow(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	table := "# Retries\n\n" +
		"| Attempt | Delay |\n" +
		"| ------- | ----- |\n" +
		"| 1       | 1s    |\n" +
		"| 2       | %s    |\n" +
		"| 3       | 4s    |\n"
	reviewID := onAnEditedDocument(t, server.URL,
		strings.Replace(table, "%s", "2s", 1),
		strings.Replace(table, "%s", "3s", 1),
		lines{"new", 5, 7})
	block := regexp.MustCompile(`data-block="(\d+)"`).FindStringSubmatch(openingTag(stop(t, server.URL, reviewID), `data-treatment="edited"`))
	if block == nil {
		t.Fatal("expected the edited row to carry its block")
	}

	reportCursor(t, server.URL, reviewID, daemon.CursorWire{Step: 1, Excerpt: 0, Block: atoi(t, block[1])})

	out := stop(t, server.URL, reviewID)
	if tag := openingTag(out, `data-treatment="edited"`); !strings.HasPrefix(tag, "<tr") || !isTheCursors(tag) {
		t.Errorf("expected the edited row marked as the cursor's, got %q in:\n%s", tag, out)
	}
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("not a number: %q", s)
	}
	return n
}

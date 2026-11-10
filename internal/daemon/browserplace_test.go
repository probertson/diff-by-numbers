package daemon_test

import (
	"bytes"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// A Document Excerpt is headed by its file and the headings it opens under, as
// the TUI's file header is.
func TestADocumentExcerptIsHeadedByItsHeadingPath(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onExcerptsOfANewDocument(t, server.URL, "# Retry policy\n\n"+
		"## Backoff\n\n"+
		"Wait longer each time.\n", [2]int{1, 4}, [2]int{5, 5})

	out := html.UnescapeString(stop(t, server.URL, reviewID))

	if want := "spec.md · new 5 › Retry policy › Backoff"; !strings.Contains(out, want) {
		t.Errorf("expected the Excerpt headed %q, got:\n%s", want, out)
	}
}

// An Excerpt's header names the lines it shows on each side, since the old
// lines an edit replaced or removed are drawn among the new ones: a range alone
// would leave which file's lines it counts unsaid, and miss the old ones.
func TestAnExcerptsHeaderNamesTheLinesItShowsOnEachSide(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onAnEditedDocument(t, server.URL,
		"1. **Keep.** The first item stays.\n"+
			"2. **The sauce.** As the cook, I can make the sauce.\n"+
			"   1. Tomatoes go in first.\n"+
			"   2. Herbs go in last.\n"+
			"3. **The garnish.** As the cook, I can add the garnish\n"+
			"   for the one plate being served now.\n",
		"1. **Keep.** The first item stays.\n"+
			"2. **The garnish.** As the cook, I can add the garnish\n"+
			"   for the one plate being served now.\n",
		lines{"new", 1, 3})

	out := html.UnescapeString(stop(t, server.URL, reviewID))

	if want := "spec.md · new 1–3 · old 2–5"; !strings.Contains(out, want) {
		t.Errorf("expected the Excerpt headed %q, got:\n%s", want, out)
	}
}

// A Step in a Chapter names its Chapter by number and name, as the TUI's Step
// header does.
func TestAStepInAChapterNamesItsChapter(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	posted := postRound(t, server.URL, chapteredRound(featureRepo(t)))
	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/goto/1")

	out := html.UnescapeString(stop(t, server.URL, posted.ReviewID))

	if want := "1 · NAME-the-chapter"; !strings.Contains(out, want) {
		t.Errorf("expected the Step header to name its Chapter %q, got:\n%s", want, out)
	}
}

// On a Chapter's intro the page shows the intro, as the TUI does: the
// Chapter's number and name, what it covers, and its Steps.
func TestAChaptersIntroShowsTheIntro(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	posted := postRound(t, server.URL, chapteredRound(featureRepo(t)))
	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/chapter/1")

	out := html.UnescapeString(stop(t, server.URL, posted.ReviewID))

	for _, want := range []string{
		"1 · NAME-the-chapter",
		"INTRO-what-it-covers",
		"NAME-the-change", // the Chapter's one Step
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the intro to show %q, got:\n%s", want, out)
		}
	}
}

// reportScreen tells dbn which of its screens the TUI shows for a review, as
// the TUI does for the screens only it knows of.
func reportScreen(t *testing.T, baseURL, reviewID, screen string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"screen": screen})
	response, err := http.Post(reviewURL(baseURL, reviewID)+"/screen", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /screen: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST /screen %q answered %s", screen, response.Status)
	}
}

// Past the last Step the TUI shows its conclusion screen while dbn stays on
// the last Step, so the page knows of it only from the TUI's report.
func TestOnTheConclusionScreenThePageSaysSoUntilTheReviewerGoesBack(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	t.Cleanup(server.Close)
	root := featureRepo(t)
	posted := postRound(t, server.URL, roundWithRepository(root, map[string]any{"root": root, "base": "main"}))
	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/advance")
	events := follow(t, server.URL, posted.ReviewID)

	reportScreen(t, server.URL, posted.ReviewID, "conclusion")

	if got := nextEvent(events); got != "redraw" {
		t.Fatalf("expected a redraw event after the report, got %q", got)
	}
	if out := stop(t, server.URL, posted.ReviewID); !strings.Contains(out, "At the end of the review") ||
		strings.Contains(out, "NAME-the-change") {
		t.Errorf("expected only a notice that the Reviewer is at the end of the review, got:\n%s", out)
	}

	reportScreen(t, server.URL, posted.ReviewID, "review")

	if got := nextEvent(events); got != "redraw" {
		t.Fatalf("expected a redraw event after going back, got %q", got)
	}
	if out := stop(t, server.URL, posted.ReviewID); !strings.Contains(out, "NAME-the-change") {
		t.Errorf("expected going back to bring the last Step back, got:\n%s", out)
	}
}

// The Inbox is the TUI window's, not the review's, so a page learns the
// Reviewer has set its review aside only from the TUI's report.
func TestAtTheInboxThePageSaysSo(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	posted := postRound(t, server.URL, roundWithRepository(root, map[string]any{"root": root, "base": "main"}))
	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/advance")

	reportScreen(t, server.URL, posted.ReviewID, "inbox")

	if out := stop(t, server.URL, posted.ReviewID); !strings.Contains(out, "At the Inbox") ||
		strings.Contains(out, "NAME-the-change") {
		t.Errorf("expected only a notice that the Reviewer is at the Inbox, got:\n%s", out)
	}
}

func TestAScreenDbnDoesNotKnowIsRefused(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	posted := postRound(t, server.URL, minimalRound(featureRepo(t)))

	response, err := http.Post(reviewURL(server.URL, posted.ReviewID)+"/screen", "application/json",
		strings.NewReader(`{"screen":"elsewhere"}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Errorf("expected an unknown screen refused, got %s", response.Status)
	}
}

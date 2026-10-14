package daemon_test

import (
	"bufio"
	"context"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// page is what the Browser Renderer serves for a review: the page a Reviewer's
// browser opens.
func page(t *testing.T, baseURL, reviewID string) string {
	t.Helper()
	response, err := http.Get(reviewURL(baseURL, reviewID) + "/page")
	if err != nil {
		t.Fatalf("GET /page: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /page answered %s", response.Status)
	}
	if kind := response.Header.Get("Content-Type"); !strings.HasPrefix(kind, "text/html") {
		t.Errorf("expected the page served as HTML, got %q", kind)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read /page: %v", err)
	}
	return string(body)
}

func TestThePageShowsTheStepTheReviewerIsOn(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	round := roundWithRepository(root, map[string]any{"root": root, "base": "main"})
	round["steps"].([]any)[0].(map[string]any)["questions"] = []any{
		map[string]any{"text": "QUESTION-is-a-retry-enough?"},
	}
	posted := postRound(t, server.URL, round)
	at := reviewURL(server.URL, posted.ReviewID)
	httpPost(t, at+"/advance")
	raiseComment(t, at, 0, 4, 4, "NOTE-name-it-better")

	out := page(t, server.URL, posted.ReviewID)

	for _, want := range []string{
		"NAME-the-change",
		"EXPLANATION-what-it-does",
		"QUESTION-is-a-retry-enough?",
		"NOTE-name-it-better",
		"regenerated lockfile", // the Acknowledgement's reason
		"LOCKFILE",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the page to show %q, got:\n%s", want, out)
		}
	}
}

// A code Excerpt is drawn as the TUI's source view draws it: a sign on each
// changed row, and only the words the edit changed coloured.
func TestACodeExcerptIsDrawnInTheSourceViewsStyle(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := editedRepo(t)
	posted := postRound(t, server.URL, map[string]any{
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
	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/advance")

	// The stop, not the page: the page's style strikes through edited Documents.
	out := html.UnescapeString(stop(t, server.URL, posted.ReviewID))

	for _, want := range []string{
		`<span class="sign">-</span><span class="text">the <span class="changed">old</span> guard</span>`,
		`<span class="sign">+</span><span class="text">the <span class="changed">new</span> guard</span>`,
		`<span class="sign"> </span><span class="text">keep</span>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the code drawn with %s, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "<del") || strings.Contains(out, "<s>") || strings.Contains(out, "line-through") {
		t.Errorf("code is never struck through, got:\n%s", out)
	}
}

// stop is the fragment the page's script swaps in: the stop the Reviewer is
// at, without the page around it.
func stop(t *testing.T, baseURL, reviewID string) string {
	t.Helper()
	response, err := http.Get(reviewURL(baseURL, reviewID) + "/page/stop")
	if err != nil {
		t.Fatalf("GET /page/stop: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /page/stop answered %s", response.Status)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read /page/stop: %v", err)
	}
	return string(body)
}

// onANewDocument posts a Round whose one Step shows a new Markdown Document
// whole, and moves onto it.
func onANewDocument(t *testing.T, baseURL, document string) string {
	t.Helper()
	return onExcerptsOfANewDocument(t, baseURL, document, [2]int{1, strings.Count(document, "\n")})
}

// onExcerptsOfANewDocument posts a Round whose one Step shows a new Markdown
// Document in Excerpts, each its first and last line, and moves onto it.
func onExcerptsOfANewDocument(t *testing.T, baseURL, document string, excerpts ...[2]int) string {
	t.Helper()
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	os.WriteFile(filepath.Join(root, "README"), []byte("base\n"), 0o644)
	git("init", "-q", "-b", "main")
	git("add", ".")
	git("commit", "-qm", "initial")
	git("checkout", "-q", "-b", "feature")
	os.WriteFile(filepath.Join(root, "spec.md"), []byte(document), 0o644)
	git("add", ".")
	var shown []any
	for _, lines := range excerpts {
		shown = append(shown, map[string]any{
			"repository": root, "file": "spec.md", "side": "new", "first_line": lines[0], "last_line": lines[1],
		})
	}
	posted := postRound(t, baseURL, map[string]any{
		"label":        "LABEL-the-review",
		"brief":        map[string]any{"goal": "write the spec", "background": "none yet"},
		"repositories": []any{map[string]any{"root": root, "base": "main"}},
		"steps": []any{map[string]any{
			"name": "The spec", "explanation": "all of it",
			"excerpts": shown,
		}},
	})
	httpPost(t, reviewURL(baseURL, posted.ReviewID)+"/advance")
	return posted.ReviewID
}

func TestANewDocumentIsRenderedWithRealHeadingsEmphasisAndTables(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onANewDocument(t, server.URL, "# Retry policy\n\n"+
		"Retries are *capped* at **three**.\n\n"+
		"| Attempt | Delay |\n"+
		"| ------- | ----- |\n"+
		"| 1       | 1s    |\n"+
		"| 2       | 2s    |\n")

	out := stop(t, server.URL, reviewID)

	for _, want := range []string{
		">Retry policy</h1>",
		"<em>capped</em>",
		"<strong>three</strong>",
		"<table",
		">Attempt</th>",
		">2s</td>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the Document rendered with %s, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "| Attempt") {
		t.Errorf("a rendered table should not show its source pipes, got:\n%s", out)
	}
}

func TestRawHTMLInADocumentComesOutEscaped(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onANewDocument(t, server.URL, "# Notes\n\n"+
		"<script>alert(\"block\")</script>\n\n"+
		"Inline <b onclick=\"steal()\">bold</b> stays text.\n")

	out := stop(t, server.URL, reviewID)

	for _, unsafe := range []string{"<script", "<b ", "<b>"} {
		if strings.Contains(out, unsafe) {
			t.Errorf("expected raw HTML never passed through, found %q in:\n%s", unsafe, out)
		}
	}
	for _, want := range []string{"&lt;script&gt;alert(", "&lt;b onclick=", "bold&lt;/b&gt; stays text"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the raw HTML shown as text (%s), got:\n%s", want, out)
		}
	}
}

// follow opens a review's change stream as the page does, and returns the
// names of the events it sends.
func follow(t *testing.T, baseURL, reviewID string) <-chan string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, reviewURL(baseURL, reviewID)+"/page/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET /page/events: %v", err)
	}
	if kind := response.Header.Get("Content-Type"); !strings.HasPrefix(kind, "text/event-stream") {
		t.Fatalf("expected an event stream, got %q", kind)
	}
	events := make(chan string, 16)
	go func() {
		defer response.Body.Close()
		lines := bufio.NewScanner(response.Body)
		for lines.Scan() {
			if name, ok := strings.CutPrefix(lines.Text(), "event: "); ok {
				events <- name
			}
		}
	}()
	return events
}

// nextEvent is the stream's next event, or "" if none comes in good time.
func nextEvent(events <-chan string) string {
	select {
	case name := <-events:
		return name
	case <-time.After(2 * time.Second):
		return ""
	}
}

func TestTheChangeStreamEmitsWhenTheReviewerMoves(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	t.Cleanup(server.Close) // after the stream closes: a cleanup, not a defer
	root := featureRepo(t)
	posted := postRound(t, server.URL, chapteredRound(root))
	at := reviewURL(server.URL, posted.ReviewID)
	events := follow(t, server.URL, posted.ReviewID)

	for _, move := range []string{"/advance", "/back", "/goto/1", "/chapter/1"} {
		httpPost(t, at+move)

		if got := nextEvent(events); got != "redraw" {
			t.Errorf("expected a redraw event after %s, got %q", move, got)
		}
	}
}

// The page draws the Reviewer's Comments, so raising one redraws it too.
func TestTheChangeStreamEmitsWhenACommentIsRaised(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	t.Cleanup(server.Close)
	posted := postRound(t, server.URL, minimalRound(featureRepo(t)))
	at := reviewURL(server.URL, posted.ReviewID)
	httpPost(t, at+"/advance")
	events := follow(t, server.URL, posted.ReviewID)

	raiseComment(t, at, 0, 4, 4, "name it better")

	if got := nextEvent(events); got != "redraw" {
		t.Errorf("expected a redraw event after a Comment was raised, got %q", got)
	}
}

// Each page follows its own review, whichever review the TUI has open.
func TestTheChangeStreamFollowsOnlyItsOwnReview(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	t.Cleanup(server.Close)
	mine := postRound(t, server.URL, minimalRound(featureRepo(t)))
	other := postRound(t, server.URL, minimalRound(featureRepo(t)))
	events := follow(t, server.URL, mine.ReviewID)

	httpPost(t, reviewURL(server.URL, other.ReviewID)+"/advance")

	select {
	case got := <-events:
		t.Errorf("expected no event for another review's move, got %q", got)
	case <-time.After(200 * time.Millisecond):
	}
}

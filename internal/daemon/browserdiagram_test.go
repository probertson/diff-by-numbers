package daemon_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// The Browser Renderer draws a Document's Mermaid diagrams (#191). The page
// holds each diagram's source in a diagram container, and Mermaid's library,
// embedded in dbn and served by the daemon, draws it in the browser. These
// tests read what dbn serves; Mermaid's drawing is not re-tested.

const diagramDocument = "# Architecture\n" +
	"\n" +
	"```mermaid\n" +
	"flowchart LR\n" +
	"  tui --> daemon\n" +
	"  style daemon fill:#f9f\n" +
	"```\n"

// served fetches a path from the daemon, failing unless it answers 200.
func served(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s answered %s", url, response.Status)
	}
	return response, string(body)
}

func TestANewDiagramIsHeldInADiagramContainerWithItsSourceAndNoLabel(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onANewDocument(t, server.URL, diagramDocument)

	out := stop(t, server.URL, reviewID)

	container := regexp.MustCompile(`(?s)<figure class="diagram"[^>]*>(.*?)</figure>`).FindStringSubmatch(out)
	if container == nil {
		t.Fatalf("expected the diagram in a diagram container, got:\n%s", out)
	}
	if want := "flowchart LR\n  tui --&gt; daemon\n  style daemon fill:#f9f\n"; !strings.Contains(container[1], want) {
		t.Errorf("expected the container to hold the diagram's source %q as authored, got:\n%s", want, container[1])
	}
	for _, label := range []string{"before", "after"} {
		if strings.Contains(strings.ToLower(container[0]), label) {
			t.Errorf("expected a new diagram drawn once with no %q label, got:\n%s", label, container[0])
		}
	}
	if strings.Contains(out, "language-mermaid") {
		t.Errorf("expected the diagram held for drawing, not as a code block, got:\n%s", out)
	}
}

// Mermaid is drawn by dbn's own copy of the library, in a frame dbn serves: the
// page references no outside script, and nothing is fetched from the internet.
func TestTheMermaidLibraryIsServedByDbnAndThePageReferencesNoOutsideScript(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onANewDocument(t, server.URL, diagramDocument)
	out := page(t, server.URL, reviewID)
	frameURL := regexp.MustCompile(`data-diagrams-url="([^"]+)"`).FindStringSubmatch(out)
	if frameURL == nil || !strings.HasPrefix(frameURL[1], "/") {
		t.Fatalf("expected the page to name where on dbn its diagrams are drawn, got:\n%s", out)
	}

	_, frame := served(t, server.URL+frameURL[1])
	libraryURL := regexp.MustCompile(`<script src="([^"]+)"></script>`).FindStringSubmatch(frame)
	if libraryURL == nil {
		t.Fatalf("expected the diagrams frame to load Mermaid's library, got:\n%s", frame)
	}
	library, script := served(t, server.URL+libraryURL[1])

	if kind := library.Header.Get("Content-Type"); !strings.HasPrefix(kind, "text/javascript") {
		t.Errorf("expected the library served as JavaScript, got %q", kind)
	}
	if !strings.Contains(script, `globalThis["mermaid"]`) {
		t.Errorf("expected the library to be Mermaid's, got %d bytes starting %.200q", len(script), script)
	}
	for name, html := range map[string]string{"page": out, "frame": frame} {
		for _, src := range regexp.MustCompile(`<(?:script|iframe|link|img)[^>]*(?:src|href)="([^"]*)"`).FindAllStringSubmatch(html, -1) {
			if !strings.HasPrefix(src[1], "/") || strings.HasPrefix(src[1], "//") {
				t.Errorf("expected the %s to load nothing from outside dbn, found %q", name, src[1])
			}
		}
	}
}

// The library is served compressed to a browser that accepts it, since it is
// large, and readable to anything that does not.
func TestTheMermaidLibraryIsServedCompressedOnlyToWhatAcceptsIt(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	_, frame := served(t, server.URL+"/browser/diagrams")
	libraryURL := regexp.MustCompile(`<script src="([^"]+)"></script>`).FindStringSubmatch(frame)[1]

	request, _ := http.NewRequest(http.MethodGet, server.URL+libraryURL, nil)
	request.Header.Set("Accept-Encoding", "identity")
	plain, err := http.DefaultTransport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Body.Close()
	request.Header.Set("Accept-Encoding", "gzip, deflate, br")
	compressed, err := http.DefaultTransport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Body.Close()

	body, _ := io.ReadAll(plain.Body)
	if plain.Header.Get("Content-Encoding") != "" || !strings.Contains(string(body), `globalThis["mermaid"]`) {
		t.Errorf("expected the library readable without compression, got encoding %q", plain.Header.Get("Content-Encoding"))
	}
	if got := compressed.Header.Get("Content-Encoding"); got != "gzip" {
		t.Errorf("expected the library sent gzipped to a browser that accepts it, got %q", got)
	}
}

// The page still runs no script and applies no style but its own: Mermaid runs
// in its own frame from dbn, and its drawing is shown as an image made in the
// page, so the author's styles inside it apply without the page allowing any
// inline style.
func TestThePagesPolicyAllowsOnlyDbnsDiagramFrameAndTheImagesItDraws(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onANewDocument(t, server.URL, diagramDocument)
	response, err := http.Get(reviewURL(server.URL, reviewID) + "/page")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	frame, _ := served(t, server.URL+"/browser/diagrams")

	policy := response.Header.Get("Content-Security-Policy")
	framePolicy := frame.Header.Get("Content-Security-Policy")

	for _, want := range []string{"frame-src 'self'", "img-src 'self' blob:"} {
		if !strings.Contains(policy, want) {
			t.Errorf("expected the page's policy to have %q, got %q", want, policy)
		}
	}
	for _, want := range []string{"default-src 'none'", "script-src 'self'", "frame-ancestors 'self'"} {
		if !strings.Contains(framePolicy, want) {
			t.Errorf("expected the diagrams frame's policy to have %q, got %q", want, framePolicy)
		}
	}
	if strings.Contains(framePolicy, "unsafe-eval") || strings.Contains(framePolicy, "script-src 'unsafe-inline'") {
		t.Errorf("expected the diagrams frame to run nothing but Mermaid's library, got %q", framePolicy)
	}
}

// A changed diagram is drawn twice, before and after, each as authored, with
// the edit to its source beneath as track changes (#193). dbn marks nothing on
// either drawing, so its colours are never mistaken for the author's.

const (
	diagramBefore = "# Architecture\n" +
		"\n" +
		"```mermaid\n" +
		"flowchart LR\n" +
		"  tui --> daemon\n" +
		"```\n"
	diagramAfter = "# Architecture\n" +
		"\n" +
		"```mermaid\n" +
		"flowchart LR\n" +
		"  tui --> daemon\n" +
		"  daemon --> store\n" +
		"```\n"
)

// figure is the diagram container whose class starts with class, and what it
// holds; it fails the test if there is none.
func figure(t *testing.T, out, class string) (string, string, int) {
	t.Helper()
	pattern := regexp.MustCompile(`(?s)<figure class="` + regexp.QuoteMeta(class) + `[" ][^>]*>(.*?)</figure>`)
	at := pattern.FindStringSubmatchIndex(out)
	if at == nil {
		t.Fatalf("expected a diagram container of class %q, got:\n%s", class, out)
	}
	return out[at[0]:at[1]], out[at[2]:at[3]], at[1]
}

func TestAChangedDiagramIsHeldBeforeAndAfterLabelledWithTheEditToItsSourceBeneath(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onAnEditedDocument(t, server.URL, diagramBefore, diagramAfter, lines{"new", 1, 7})

	out := stop(t, server.URL, reviewID)

	before, beforeHolds, _ := figure(t, out, "diagram before")
	after, afterHolds, afterEnds := figure(t, out, "diagram after")
	if !strings.Contains(beforeHolds, ">before<") || !strings.Contains(afterHolds, ">after<") {
		t.Errorf("expected the containers labelled before and after, got:\n%s\n%s", before, after)
	}
	if want := "flowchart LR\n  tui --&gt; daemon\n</code>"; !strings.Contains(beforeHolds, want) {
		t.Errorf("expected before to hold the source as it was, %q, got:\n%s", want, before)
	}
	if want := "flowchart LR\n  tui --&gt; daemon\n  daemon --&gt; store\n</code>"; !strings.Contains(afterHolds, want) {
		t.Errorf("expected after to hold the source as it is, %q, got:\n%s", want, after)
	}
	if strings.Index(out, before) > strings.Index(out, after) {
		t.Errorf("expected before ahead of after, got:\n%s", out)
	}
	for _, drawing := range []string{before, after} {
		for _, mark := range []string{"data-treatment", "<ins>", "<del>"} {
			if strings.Contains(drawing, mark) {
				t.Errorf("expected no change mark on a diagram, found %s in:\n%s", mark, drawing)
			}
		}
	}
	edit := regexp.MustCompile(`(?s)<div class="tracked code[^"]*"[^>]*>.*?<ins>store</ins>`).FindString(out[afterEnds:])
	if edit == "" {
		t.Errorf("expected the edit to the source beneath the drawings, as track changes, got:\n%s", out)
	}
}

func TestARemovedDiagramIsHeldBeforeOnlyAndDimmed(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onAnEditedDocument(t, server.URL,
		"# Architecture\n\nThe parts.\n\n```mermaid\nflowchart LR\n  tui --> daemon\n```\n\nThe end.\n",
		"# Architecture\n\nThe parts.\n\nThe end.\n",
		lines{"new", 1, 5}, lines{"old", 4, 9})

	out := stop(t, server.URL, reviewID)

	before, holds, _ := figure(t, out, "diagram before")
	if !strings.Contains(before, `class="diagram before removed`) {
		t.Errorf("expected the removed diagram marked dimmed, got:\n%s", before)
	}
	if !strings.Contains(holds, ">before<") || !strings.Contains(holds, "flowchart LR\n  tui --&gt; daemon\n</code>") {
		t.Errorf("expected the removed diagram labelled before, holding its source, got:\n%s", before)
	}
	if strings.Contains(out, `class="diagram after`) {
		t.Errorf("expected no after for a removed diagram, got:\n%s", out)
	}
}

// Part of a diagram is not the diagram its author drew, so a diagram an
// Excerpt cuts into is shown as its source, as code.
func TestADiagramAnExcerptCutsIntoIsShownAsCode(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onExcerptsOfANewDocument(t, server.URL, diagramDocument, [2]int{1, 5}, [2]int{6, 7})

	out := stop(t, server.URL, reviewID)

	if strings.Contains(out, `class="diagram`) {
		t.Errorf("expected no diagram container for part of a diagram, got:\n%s", out)
	}
	if !strings.Contains(out, "tui --&gt; daemon") {
		t.Errorf("expected the part shown as source, got:\n%s", out)
	}
}

// A diagram is one block, so the TUI's cursor on its block is on the drawn
// diagram: the page marks the diagram's container as the cursor's, and the
// cursor event names the container's place, which the page brightens and
// scrolls to (#196).
func TestACursorOnADiagramsBlockMarksTheDiagram(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	t.Cleanup(server.Close) // after the stream closes: a cleanup, not a defer
	reviewID := onANewDocument(t, server.URL, diagramDocument)
	events := follow(t, server.URL, reviewID)

	reportCursor(t, server.URL, reviewID, daemon.CursorWire{Step: 1, Excerpt: 0, Block: 2})

	event := nextEventWithData(events)
	out := stop(t, server.URL, reviewID)
	tag := openingTag(out, `data-block="2"`)
	if !strings.HasPrefix(tag, `<figure class="diagram`) || !isTheCursors(tag) {
		t.Errorf("expected the diagram's container marked as the cursor's, got %q in:\n%s", tag, out)
	}
	var cursor struct {
		Place string `json:"place"`
	}
	if err := json.Unmarshal([]byte(event.data), &cursor); err != nil || cursor.Place == "" ||
		!strings.Contains(tag, `data-place="`+cursor.Place+`"`) {
		t.Errorf("expected the cursor event to name the diagram's place, got %q for %q (%v)", event.data, tag, err)
	}
	if tag := openingTag(out, `data-block="1"`); isTheCursors(tag) {
		t.Errorf("expected only the diagram marked, but %q is too", tag)
	}
}

// The Reviewer comments on a diagram's source in the TUI; on the page the
// Comment's marker is on the drawn diagram, and its note follows it (#196).
func TestACommentOnADiagramsBlockIsMarkedOnTheDiagram(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onANewDocument(t, server.URL, diagramDocument)

	raiseComment(t, reviewURL(server.URL, reviewID), 0, 4, 5, "the daemon calls the TUI too")

	out := stop(t, server.URL, reviewID)
	figure := regexp.MustCompile(`(?s)<figure[^>]*>.*?</figure>`).FindStringIndex(out)
	if figure == nil {
		t.Fatalf("expected the diagram drawn, got:\n%s", out)
	}
	tag := openingTag(out[figure[0]:], `data-block="2"`)
	if class := regexp.MustCompile(`class="([^"]*)"`).FindStringSubmatch(tag); class == nil ||
		!strings.Contains(" "+class[1]+" ", " diagram ") || !strings.Contains(" "+class[1]+" ", " commented ") {
		t.Errorf("expected the diagram's container marked as commented, got %q in:\n%s", tag, out)
	}
	if tag := openingTag(out, `data-block="1"`); strings.Contains(tag, "commented") {
		t.Errorf("expected only the diagram marked as commented, but %q is too", tag)
	}
	note := strings.Index(out, "the daemon calls the TUI too")
	if note < figure[1] {
		t.Errorf("expected the Comment's note to follow the diagram, got:\n%s", out)
	}
}

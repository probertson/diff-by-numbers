package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// A Document Excerpt's sticky file header names the section the Reviewer is
// reading: the file, then the heading path of the block at the top of the pane,
// as dbn's Step view gives it (#145). Code's headers are as they were.

// withPaths gives an Excerpt's blocks their heading paths, in order.
func withPaths(excerpt daemon.ExcerptWire, paths ...[]string) daemon.ExcerptWire {
	for i, path := range paths {
		excerpt.Blocks[i].Path = path
	}
	return excerpt
}

// limitsSection is an Excerpt of two paragraphs deep in a Document, under
// "Retries" and "Limits": lines 1 and 3.
func limitsSection() daemon.ExcerptWire {
	under := []string{"Retries", "Limits"}
	return withPaths(documentExcerpt("docs/retries.md", para("limit one"), para("limit two")), under, under)
}

// sectionedDocument is a Document of two sections, each of six one-line
// paragraphs: "Retries", then "Limits" inside it.
func sectionedDocument() daemon.ExcerptWire {
	blocks := []docBlock{heading(1, "Retries")}
	paths := [][]string{nil}
	for n := 1; n <= 6; n++ {
		blocks = append(blocks, para(fmt.Sprintf("retry %d", n)))
		paths = append(paths, []string{"Retries"})
	}
	blocks = append(blocks, heading(2, "Limits"))
	paths = append(paths, []string{"Retries"})
	for n := 1; n <= 6; n++ {
		blocks = append(blocks, para(fmt.Sprintf("limit %d", n)))
		paths = append(paths, []string{"Retries", "Limits"})
	}
	return withPaths(documentExcerpt("docs/retries.md", blocks...), paths...)
}

// fileHeaderOf is the one file header drawn for file, as the Reviewer reads it.
func fileHeaderOf(t *testing.T, out, file string) string {
	t.Helper()
	return strings.TrimSpace(lineWith(t, ansi.Strip(out), "── "+file))
}

// downTo presses ↓ until the cursor is on the block reading text.
func downTo(t *testing.T, m model, text string) model {
	t.Helper()
	for range 40 {
		if strings.Contains(cursorText(t, m), text) {
			return m
		}
		m = press(m, "down")
	}
	t.Fatalf("expected ↓ to reach %q", text)
	return m
}

func TestTheFileHeaderInPlaceShowsTheExcerptsFirstBlocksPath(t *testing.T) {
	m := stepModel(t, documentStep(limitsSection()))

	out := drawnStep(m)

	if got, want := fileHeaderOf(t, out, "docs/retries.md"), "── docs/retries.md › Retries › Limits"; got != want {
		t.Errorf("expected the header %q, got %q", want, got)
	}
}

func TestTheStuckHeaderShowsThePathAtTheTopOfThePaneAndChangesPastAHeading(t *testing.T) {
	m := stepModel(t, documentStep(sectionedDocument()))
	m.height = 16

	inRetries := downTo(t, m, "retry 5")
	inLimits := downTo(t, inRetries, "limit 5")

	if got, want := fileHeaderOf(t, drawnStep(inRetries), "docs/retries.md"), "── docs/retries.md › Retries"; got != want {
		t.Errorf("expected the stuck header %q in the first section, got %q:\n%s", want, got, ansi.Strip(drawnStep(inRetries)))
	}
	if got, want := fileHeaderOf(t, drawnStep(inLimits), "docs/retries.md"), "── docs/retries.md › Retries › Limits"; got != want {
		t.Errorf("expected the stuck header %q past the Limits heading, got %q:\n%s", want, got, ansi.Strip(drawnStep(inLimits)))
	}
}

func TestANarrowPaneDropsOuterHeadingsBeforeCuttingTheInnermost(t *testing.T) {
	deep := []string{"Retries", "Limits", "Per host"}
	step := documentStep(withPaths(documentExcerpt("docs/retries.md", para("each host has a cap")), deep))
	cases := []struct {
		width int
		want  string
	}{
		{80, "── docs/retries.md › Retries › Limits › Per host"},
		{44, "── docs/retries.md › … › Limits › Per host"},
		{40, "── docs/retries.md › … › Per host"},
		{30, "── docs/retries.md › … › Per…"},
	}
	for _, c := range cases {
		m := stepModel(t, step)
		m.width = c.width

		got := fileHeaderOf(t, drawnStep(m), "docs/retries.md")

		if got != c.want {
			t.Errorf("at width %d expected the header %q, got %q", c.width, c.want, got)
		}
	}
}

func TestTheHeaderUsesThePanesFullWidthNotTheProseWidth(t *testing.T) {
	deep := []string{"Retries", "Limits", "Per host"}
	m := stepModel(t, documentStep(withPaths(documentExcerpt("docs/retries.md", para("each host has a cap")), deep)))
	m.proseWidth = 30

	got := fileHeaderOf(t, drawnStep(m), "docs/retries.md")

	if want := "── docs/retries.md › Retries › Limits › Per host"; got != want {
		t.Errorf("expected the header %q, got %q", want, got)
	}
}

func TestTheSourceViewShowsTheSameHeader(t *testing.T) {
	m := press(stepModel(t, documentStep(limitsSection())), "s")

	out := drawnStep(m)

	if got, want := fileHeaderOf(t, out, "docs/retries.md"), "── docs/retries.md › Retries › Limits"; got != want {
		t.Errorf("expected the header %q in the source view, got %q", want, got)
	}
}

func TestAnExpandedAcknowledgementOverADocumentShowsTheSameHeader(t *testing.T) {
	expansion := []daemon.ExcerptWire{limitsSection()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/expand/") {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(expansion)
	}))
	t.Cleanup(server.Close)
	m := stepModel(t, docAndCodeStep())
	m.client = client{base: server.URL, review: "a1"}

	m = press(press(m, "down"), "x")

	if got, want := fileHeaderOf(t, drawnStep(m), "docs/retries.md"), "── docs/retries.md › Retries › Limits"; got != want {
		t.Errorf("expected the header %q over the expansion, got %q", want, got)
	}
}

func TestADocumentWithoutHeadingsShowsJustTheFile(t *testing.T) {
	m := stepModel(t, documentStep(documentExcerpt("docs/retries.md", para("a request is retried"), para("twice"))))

	if got, want := fileHeaderOf(t, drawnStep(m), "docs/retries.md"), "── docs/retries.md"; got != want {
		t.Errorf("expected just the file %q, got %q", want, got)
	}
}

func TestTheCursorNeverLandsOnTheHeader(t *testing.T) {
	m := stepModel(t, documentStep(limitsSection()))

	m = press(m, "up")

	if got := cursorText(t, m); !strings.Contains(got, "limit one") {
		t.Errorf("expected ↑ to stay on the first block, not the header, got %q", got)
	}
	for _, line := range m.cursor.lines {
		if strings.Contains(line.text, "›") {
			t.Errorf("expected no cursor stop on the header, got %+v", line)
		}
	}
}

func TestCodeHeadersAreUnchanged(t *testing.T) {
	code := daemon.ExcerptWire{File: "retry.go", Side: "new", FirstLine: 1, LastLine: 1,
		Lines: []daemon.LineWire{{Number: 1, Text: "func retry() {}", Side: "new", Changed: true}}}
	m := stepModel(t, documentStep(limitsSection(), code))

	if got, want := fileHeaderOf(t, drawnStep(m), "retry.go"), "── retry.go"; got != want {
		t.Errorf("expected the code's header %q, got %q", want, got)
	}
}

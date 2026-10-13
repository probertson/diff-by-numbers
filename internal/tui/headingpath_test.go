package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// A Document Excerpt's sticky file header names the section the Reviewer is
// reading: the file, then the heading path of the block at the top of the pane,
// as dbn's Step view gives it (#145). Code's headers are as they were.

// withPaths gives an Excerpt's blocks their heading paths, in order, and each
// row its own, as dbn's view does for rows on their block's side: a row in a
// block its block's, and a blank line between blocks that of the block above.
func withPaths(excerpt daemon.ExcerptWire, paths ...[]string) daemon.ExcerptWire {
	for i, path := range paths {
		excerpt.Blocks[i].Path = path
	}
	var above []string
	for i, line := range excerpt.Lines {
		if line.Block > 0 {
			above = excerpt.Blocks[line.Block-1].Path
		}
		excerpt.Lines[i].Path = above
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

func TestTheSourceViewsStuckHeaderChangesPastAHeading(t *testing.T) {
	m := press(stepModel(t, documentStep(sectionedDocument())), "s")
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

// expandingInto is a model of docAndCodeStep whose daemon expands the
// Acknowledgement into the given Excerpts.
func expandingInto(t *testing.T, expansion ...daemon.ExcerptWire) model {
	t.Helper()
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
	return m
}

func TestAnExpandedAcknowledgementOverADocumentShowsTheSameHeader(t *testing.T) {
	m := expandingInto(t, limitsSection())

	formatted := press(press(m, "down"), "x")
	source := press(formatted, "s")

	want := "── docs/retries.md › Retries › Limits"
	if got := fileHeaderOf(t, drawnStep(formatted), "docs/retries.md"); got != want {
		t.Errorf("expected the header %q over the expansion, got %q", want, got)
	}
	if got := fileHeaderOf(t, drawnStep(source), "docs/retries.md"); got != want {
		t.Errorf("expected the header %q over the expansion's source, got %q", want, got)
	}
}

// renamedSection is an Excerpt whose section "Limits" was renamed "Caps": its
// one paragraph was removed and a new one added in its place.
func renamedSection() daemon.ExcerptWire {
	old, renamed := []string{"Retries", "Limits"}, []string{"Retries", "Caps"}
	return daemon.ExcerptWire{File: "docs/retries.md", Side: "new", FirstLine: 5, LastLine: 5,
		Lines: []daemon.LineWire{
			{Number: 5, Text: "Old limits applied once.", Side: "old", Changed: true, Block: 1, Path: old},
			{Number: 5, Text: "Brand new caps hold.", Side: "new", Changed: true, Block: 2, Path: renamed},
		},
		Blocks: []daemon.BlockWire{
			{Kind: "paragraph", Side: "old", FirstLine: 5, LastLine: 5, Treatment: "removed", Path: old},
			{Kind: "paragraph", Side: "new", FirstLine: 5, LastLine: 5, Treatment: "new", Path: renamed},
		},
	}
}

func TestARemovedBlockShowsTheOldHeadingsInEitherView(t *testing.T) {
	formatted := stepModel(t, documentStep(renamedSection()))
	source := press(formatted, "s")

	want := "── docs/retries.md › Retries › Limits"
	if got := fileHeaderOf(t, drawnStep(formatted), "docs/retries.md"); got != want {
		t.Errorf("expected the header %q over the removed block, got %q", want, got)
	}
	if got := fileHeaderOf(t, drawnStep(source), "docs/retries.md"); got != want {
		t.Errorf("expected the header %q over the removed block's source, got %q", want, got)
	}
}

// editedInRenamedSection is an Excerpt whose section "Limits" was renamed
// "Caps" and whose paragraph was edited: the row the edit replaced is drawn in
// the paragraph's block but sits under the old heading.
func editedInRenamedSection() daemon.ExcerptWire {
	renamed := []string{"Retries", "Caps"}
	return daemon.ExcerptWire{File: "docs/retries.md", Side: "new", FirstLine: 5, LastLine: 5,
		Lines: []daemon.LineWire{
			{Number: 5, Text: "Each host has a cap of five.", Side: "old", Changed: true, Block: 1, Path: []string{"Retries", "Limits"}},
			{Number: 5, Text: "Each host has a cap of six.", Side: "new", Changed: true, Block: 1, Path: renamed, Runs: []daemon.RunWire{
				{Text: "Each host has a cap of ", Change: "kept"},
				{Text: "five", Change: "removed"},
				{Text: "six", Change: "added"},
				{Text: ".", Change: "kept"},
			}},
		},
		Blocks: []daemon.BlockWire{
			{Kind: "paragraph", Side: "new", FirstLine: 5, LastLine: 5, Treatment: "edited", Path: renamed},
		},
	}
}

func TestTheSourceViewShowsTheOldHeadingsOverARowAnEditReplaced(t *testing.T) {
	formatted := stepModel(t, documentStep(editedInRenamedSection()))
	source := press(formatted, "s")

	if got, want := fileHeaderOf(t, drawnStep(source), "docs/retries.md"), "── docs/retries.md › Retries › Limits"; got != want {
		t.Errorf("expected the header %q over the replaced row's source, got %q", want, got)
	}
	if got, want := fileHeaderOf(t, drawnStep(formatted), "docs/retries.md"), "── docs/retries.md › Retries › Caps"; got != want {
		t.Errorf("expected the header %q over the edited block, as it now stands, got %q", want, got)
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

func TestTheStuckHeaderCannotBeSelectedCopiedOrAnchored(t *testing.T) {
	var copied []string
	was := copyText
	copyText = func(text string) bool { copied = append(copied, text); return true }
	t.Cleanup(func() { copyText = was })
	for _, source := range []bool{false, true} {
		m := stepModel(t, documentStep(sectionedDocument()))
		if source {
			m = press(m, "s")
		}
		m.height = 16
		m = downTo(t, m, "limit 5")
		asked := anchorServer(t, &m, "Re: docs/retries.md — after 1-25")
		m = press(m, "v")
		for range 40 {
			m = press(m, "up")
		}

		press(m, "y")

		if len(*asked) != 1 {
			t.Fatalf("expected one Anchor asked for, got %v", *asked)
		}
		if start := (*asked)[0]["start"]; !reflect.DeepEqual(start, map[string]any{"side": "new", "line": float64(1)}) {
			t.Errorf("expected the selection to start on the Document's first line, not its header (source view %v), got %v", source, start)
		}
	}
	if want := []string{"Re: docs/retries.md — after 1-25", "Re: docs/retries.md — after 1-25"}; !slices.Equal(copied, want) {
		t.Errorf("expected only dbn's Anchor copied, with nothing of the header, got %q", copied)
	}
}

// headerOverCursor is the file and heading path the file header names over the
// cursor's row: what stands at the top of the pane when that row does.
func headerOverCursor(t *testing.T, m model) string {
	t.Helper()
	for _, row := range m.pane().rows {
		if row.line == m.cursor.cursor {
			return strings.TrimPrefix(ansi.Strip(row.fileHeader), "── ")
		}
	}
	t.Fatalf("expected a row drawn for the cursor %d", m.cursor.cursor)
	return ""
}

// anchoredPath is the file and heading path an Anchor's Re: line names.
func anchoredPath(anchor string) string {
	re, _, _ := strings.Cut(strings.TrimPrefix(anchor, "Re: "), " — ")
	return re
}

// The heading path is decided once, by dbn, for every row: the header over a
// row and an Anchor from it name the same one in either view, a row in no
// block among the rows an edit replaced included.
func TestTheHeaderAndAnAnchorNameTheSamePathOnEveryRow(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	t.Cleanup(server.Close)
	// The section "Limits" is renamed "Caps", its first paragraph edited and
	// its second removed.
	root := repoChanging(t, "retries.md",
		"# Retries\n\n## Limits\n\nEach host has a cap of five.\n\nOld words vanish here.\n\nKept para.\n",
		"# Retries\n\n## Caps\n\nEach host has a cap of six.\n\nKept para.\n")
	reviewID := postedRound(t, server.URL, root, map[string]any{"name": "caps", "explanation": "why",
		"excerpts": []any{map[string]any{"file": "retries.md", "side": "new", "first_line": 1, "last_line": 7}}})
	formatted := openedOn(t, server.URL, reviewID)
	source := press(formatted, "s")

	for _, view := range []model{formatted, source} {
		for i, line := range view.cursor.lines {
			at := view
			at.cursor.cursor = i

			header, anchor := headerOverCursor(t, at), press(at, "c").pendingCode

			if got := anchoredPath(anchor); got != header {
				t.Errorf("expected an Anchor on %q (source view %v) to name the header's %q, got %q",
					line.text, view.cursor.source, header, got)
			}
		}
	}
	// The blank line between the edited paragraph's replaced row and the
	// removed paragraph reads with the old text around it.
	for i, line := range source.cursor.lines {
		if line.side == "old" && line.number == 6 {
			at := source
			at.cursor.cursor = i
			if got, want := headerOverCursor(t, at), "retries.md › Retries › Limits"; got != want {
				t.Errorf("expected the header %q over the blank line among the old rows, got %q", want, got)
			}
			return
		}
	}
	t.Fatal("expected the source view to show the old blank line 6")
}

package daemon_test

import (
	"fmt"
	"html"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// lines is the lines an Excerpt shows, on one side.
type lines struct {
	side        string
	first, last int
}

// onAnEditedDocument posts a Round whose one Step shows a Markdown Document the
// Change Set edits from before to after, in Excerpts of the lines given, and
// moves onto it.
func onAnEditedDocument(t *testing.T, baseURL, before, after string, shown ...lines) string {
	t.Helper()
	return onAnEditedFile(t, baseURL, "spec.md", before, after, shown...)
}

// onAnEditedFile posts a Round whose one Step shows a file the Change Set
// edits from before to after, or deletes when after is "", in Excerpts of the
// lines given, and moves onto it.
func onAnEditedFile(t *testing.T, baseURL, file, before, after string, shown ...lines) string {
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
	os.WriteFile(filepath.Join(root, file), []byte(before), 0o644)
	git("init", "-q", "-b", "main")
	git("add", ".")
	git("commit", "-qm", "initial")
	git("checkout", "-q", "-b", "feature")
	if after == "" {
		os.Remove(filepath.Join(root, file))
	} else {
		os.WriteFile(filepath.Join(root, file), []byte(after), 0o644)
	}
	git("add", ".")
	var excerpts []any
	for _, l := range shown {
		excerpts = append(excerpts, map[string]any{
			"repository": root, "file": file, "side": l.side, "first_line": l.first, "last_line": l.last,
		})
	}
	posted := postRound(t, baseURL, map[string]any{
		"label":        "LABEL-the-review",
		"brief":        map[string]any{"goal": "rework the spec", "background": "it was wrong"},
		"repositories": []any{map[string]any{"root": root, "base": "main"}},
		"steps": []any{map[string]any{
			"name": "The spec", "explanation": "the edit", "excerpts": excerpts,
		}},
	})
	httpPost(t, reviewURL(baseURL, posted.ReviewID)+"/advance")
	return posted.ReviewID
}

// wantIn reports each of wants missing from out.
func wantIn(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("expected the page to contain %s, got:\n%s", want, out)
		}
	}
}

func TestAnEditedBlockIsDrawnAsTrackChanges(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onAnEditedDocument(t, server.URL,
		"# Retry policy\n\nA request is retried when the transport fails.\n",
		"# Retry policy\n\nA request is resent when the transport fails.\n",
		lines{"new", 3, 3})

	out := html.UnescapeString(stop(t, server.URL, reviewID))

	wantIn(t, out,
		`data-treatment="edited"`,
		`A request is <del>retried</del> <ins>resent</ins> when the transport fails.`,
	)
}

// An edited block's Markdown is shown as source, its markers muted, since the
// edit may be to the markers themselves; a list item's bullet is its list's,
// drawn as an item of the list.
func TestAnEditedBlockShowsItsMarkdownMarkersMutedRatherThanRendered(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onAnEditedDocument(t, server.URL,
		"# Retry policy\n\n- A request is **retried** when it fails.\n",
		"## Retry policy\n\n- A request is **resent** when it fails.\n",
		lines{"new", 1, 3})

	out := html.UnescapeString(stop(t, server.URL, reviewID))

	wantIn(t, out,
		`<span class="md">## </span>Retry policy`,
		`<div class="line">A request is <del><span class="md">**</span><span class="bold">retried</span><span class="md">**</span></del> <ins><span class="md">**</span><span class="bold">resent</span><span class="md">**</span></ins> when it fails.`,
	)
	if !regexp.MustCompile(`<li [^>]*data-treatment="edited"`).MatchString(out) {
		t.Errorf("expected the edited list item drawn as an item of its list, got:\n%s", out)
	}
	for _, rendered := range []string{"Retry policy</h", "<strong>"} {
		if strings.Contains(out, rendered) {
			t.Errorf("expected an edited block's Markdown not rendered, found %s in:\n%s", rendered, out)
		}
	}
}

func TestARemovedBlockIsStruckWholeAndMarkedRemoved(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onAnEditedDocument(t, server.URL,
		"# Policy\n\nThe old rule goes.\n\nThe kept rule.\n",
		"# Policy\n\nThe kept rule.\n",
		lines{"new", 1, 3}, lines{"old", 3, 4})

	out := html.UnescapeString(stop(t, server.URL, reviewID))

	wantIn(t, out,
		`data-treatment="removed"`,
		`<del>The old rule goes.</del>`,
	)
}

// New blocks are marked as the TUI's gutter marks them; unchanged ones are not.
func TestANewBlockIsMarkedNewAndUnchangedTextIsNot(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onAnEditedDocument(t, server.URL,
		"# Policy\n\nThe kept rule.\n",
		"# Policy\n\nThe kept rule.\n\nAn added rule.\n",
		lines{"new", 1, 5})

	out := html.UnescapeString(stop(t, server.URL, reviewID))

	wantIn(t, out,
		`<h1 data-block="1" data-place="1.0.b1">Policy</h1>`,
		`<p data-block="2" data-place="1.0.b2">The kept rule.</p>`,
		`<p data-block="3" data-place="1.0.b3" data-treatment="new">An added rule.</p>`,
	)
}

// An edited table row is a styled source row inside its table, so a table cut
// by an edit stays one table under its header.
func TestAnEditedTableRowIsASourceRowInsideItsTable(t *testing.T) {
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

	out := html.UnescapeString(stop(t, server.URL, reviewID))

	if got := strings.Count(out, "<table"); got != 1 {
		t.Errorf("expected the rows drawn as one table, got %d tables in:\n%s", got, out)
	}
	wantIn(t, out,
		"Attempt</span></th>",
		">1s</td>",
		">4s</td>",
		`<span class="md">|</span>`,
		`<del>2s</del> <ins>3s</ins>`,
	)
	start, end := strings.Index(out, "<table"), strings.Index(out, "</table>")
	edit := strings.Index(out, `data-treatment="edited"`)
	if edit < start || edit > end {
		t.Errorf("expected the edited row inside the table, got:\n%s", out)
	}
}

// A table cut by an Excerpt is given its header row, and a paragraph it cuts
// into is widened to the whole block; what it was widened by is dimmed as
// context.
func TestWhatAnExcerptIsWidenedByIsDimmedAsContext(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	document := "Intro line one\nintro line two\nintro line three\n\n" +
		"%s\n\n" +
		"| Attempt | Delay |\n" +
		"| ------- | ----- |\n" +
		"| 1       | 1s    |\n" +
		"| 2       | %s    |\n" +
		"\n" +
		"Kept line one\nkept line two\n%s line three\n"
	reviewID := onAnEditedDocument(t, server.URL,
		fmt.Sprintf(document, "Old end.", "2s", "kept"),
		fmt.Sprintf(document, "New end.", "3s", "changed"),
		lines{"new", 3, 5}, lines{"new", 10, 10}, lines{"new", 14, 14})

	out := html.UnescapeString(stop(t, server.URL, reviewID))

	wantIn(t, out,
		// Rendered text: an unchanged paragraph the Excerpt cut into.
		`<span class="widened">Intro line one</span>`,
		`intro line three`,
		// A cut table's header row.
		`<span class="widened">Attempt</span>`,
		// An edited paragraph's lines outside the Excerpt.
		`<div class="line widened">Kept line one</div>`,
	)
	if strings.Contains(out, `<span class="widened">intro line three`) {
		t.Errorf("expected the line the Excerpt shows not dimmed, got:\n%s", out)
	}
}

// A table row shown without its table, as a deletion's Excerpt shows it, is
// still drawn as track changes rather than read as a paragraph.
func TestARemovedTableRowShownWithoutItsTableIsStillTrackChanges(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onAnEditedDocument(t, server.URL,
		"| Attempt | Delay |\n| ------- | ----- |\n| 1       | 1s    |\n| 2       | 2s    |\n",
		"| Attempt | Delay |\n| ------- | ----- |\n| 1       | 1s    |\n",
		lines{"old", 4, 4})

	out := html.UnescapeString(stop(t, server.URL, reviewID))

	wantIn(t, out,
		`data-treatment="removed"`,
		`<del><span class="md">|</span> 2       <span class="md">|</span> 2s    <span class="md">|</span></del>`,
	)
	if strings.Contains(out, "<p data-block") {
		t.Errorf("expected the row not read as a paragraph, got:\n%s", out)
	}
}

// The TUI reflows an edited paragraph as Markdown would, its source lines
// joined; the page draws it as one flowing paragraph too, broken only where
// a line ends in a hard break, rather than at every source line.
func TestAnEditedParagraphFlowsAsOneParagraph(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onAnEditedDocument(t, server.URL,
		"A request is retried\nwhen the transport\nfails.\n",
		"A request is resent\nwhen the transport  \nfails.\n",
		lines{"new", 1, 3})

	out := html.UnescapeString(stop(t, server.URL, reviewID))

	wantIn(t, out,
		`class="tracked paragraph flow`,
		`<div class="line hard-break">when the transport`,
	)
}

// A block quote's markers lead its first line only once its lines flow, as
// the TUI draws them, rather than standing in the middle of the paragraph.
func TestAFlowingQuoteShowsItsMarkerOnlyOnItsFirstLine(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onAnEditedDocument(t, server.URL,
		"> A request is retried\n> when it fails.\n",
		"> A request is resent\n> when it fails.\n",
		lines{"new", 1, 2})

	out := html.UnescapeString(stop(t, server.URL, reviewID))

	if markers := strings.Count(out, `<span class="md">&gt; </span>`) + strings.Count(out, `<span class="md">> </span>`); markers != 1 {
		t.Errorf("expected the quote's marker once, on its first line, got %d in:\n%s", markers, out)
	}
}

// An edited block is drawn where it begins, as the TUI lays it out and moves
// its cursor through it, even when the lines its edit replaced begin above
// blocks the change removed: an item renumbered into the place of one removed
// with its sub-items comes before them, not after.
func TestAnEditedBlockIsDrawnWhereItBeginsAsTheTUIOrdersIt(t *testing.T) {
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

	out := stop(t, server.URL, reviewID)

	edited := strings.Index(out, `data-treatment="edited"`)
	removed := strings.Index(out, `data-treatment="removed"`)
	if edited < 0 || removed < 0 || edited > removed {
		t.Errorf("expected the edited item drawn before the sub-items removed beneath where it begins, got:\n%s", out)
	}
}

// A Document's run is read as Markdown whole, its edited and removed blocks in
// place, so a paragraph that continues a list item after an edited one stays
// a paragraph of its list rather than becoming an indented code block, and is
// found by the TUI's cursor like any other block.
func TestAParagraphAfterAnEditedItemStaysInItsList(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	document := "- Parent item.\n" +
		"  - **Chose:** every jar starts empty:\n" +
		"    - no jar has a label\n" +
		"    - the shelf holds %s\n" +
		"\n" +
		"    Each change only adds, so nothing is lost.\n" +
		"  - **Over:** filling every jar first.\n"
	reviewID := onAnEditedDocument(t, server.URL,
		fmt.Sprintf(document, "4 jars"), fmt.Sprintf(document, "3 jars"), lines{"new", 1, 7})

	out := html.UnescapeString(stop(t, server.URL, reviewID))

	if strings.Contains(out, "<pre") {
		t.Errorf("expected no code block, got:\n%s", out)
	}
	paragraph := regexp.MustCompile(`<p [^>]*data-place="[^"]+"[^>]*>Each change only adds`)
	if !paragraph.MatchString(out) {
		t.Errorf("expected the continuing paragraph drawn as a block of the list, got:\n%s", out)
	}
	if lists := strings.Count(out, "<ul"); lists != 3 {
		t.Errorf("expected the nested list kept whole, three lists deep, got %d in:\n%s", lists, out)
	}
}

// A removed list item stays an item of its list, its bullet the list's and its
// text struck, rather than its Markdown standing as text outside the list.
func TestARemovedListItemIsDrawnAsAnItemOfItsList(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onAnEditedDocument(t, server.URL,
		"- keep\n- the item is going\n  - and its child\n",
		"- keep\n",
		lines{"new", 1, 1}, lines{"old", 2, 3})

	out := html.UnescapeString(stop(t, server.URL, reviewID))

	for _, text := range []string{"the item is going", "and its child"} {
		if !regexp.MustCompile(`<li [^>]*data-treatment="removed"[^>]*>(\s*<[^>]+>)*` + text).MatchString(out) {
			t.Errorf("expected %q drawn as an item of its list, got:\n%s", text, out)
		}
	}
	if regexp.MustCompile(`<span class="md">\s*- </span>`).MatchString(out) {
		t.Errorf("expected each item's bullet to be its list's, not its marker shown as text, got:\n%s", out)
	}
}

package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// An Acknowledgement over Document text expands into what an Excerpt of the
// same file would show: formatted, with track changes, and switched to source
// by s. Code beside it in the same Acknowledgement expands as a unified diff.

// docAndCodeStep is a Step with one narrated line and one Acknowledgement over
// a Document and a file of code.
func docAndCodeStep() *daemon.StepWire {
	step := ackStep()
	step.Acknowledgements[0].Entries = []daemon.AcknowledgedFileWire{
		{File: "docs/retries.md", ChangedLines: 2, Change: "modified"},
		{File: "gen.go", ChangedLines: 3, Change: "modified"},
	}
	return step
}

// docAndCodeModel is a model of docAndCodeStep whose daemon expands the
// Acknowledgement into an edit of the Document, then an edit of the code.
func docAndCodeModel(t *testing.T) model {
	t.Helper()
	expansion := append([]daemon.ExcerptWire{editedRetries()}, ackExpansion()[0])
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

func TestAnExpandedAcknowledgementOverADocumentIsDrawnFormattedWithTrackChanges(t *testing.T) {
	inColour(t)
	m := press(docAndCodeModel(t), "down") // onto the stop

	m = press(m, "x")

	out := drawnStep(m)
	row := rowReading(t, out, "retried") // one merged flow: the removed line is not drawn on its own
	if !strings.Contains(ansi.Strip(row), "A request is retried resent when the transport fails.") {
		t.Errorf("expected the struck and added words in one flow: %q", ansi.Strip(row))
	}
	if !trackedWith(t, row, "retried", redSGR, strikeSGR) || !trackedWith(t, row, "resent", greenSGR, underlineSGR) {
		t.Errorf("expected the edit drawn as track changes: %q", row)
	}
	if !drawnWith(t, row, "±", yellowSGR) {
		t.Errorf("expected the edited paragraph signed with a yellow ±: %q", row)
	}
}

func TestCodeBesideADocumentInAnAcknowledgementStillExpandsAsAUnifiedDiff(t *testing.T) {
	m := press(press(docAndCodeModel(t), "down"), "x")

	out := ansi.Strip(drawnStep(m))

	removed, added := lineWith(t, out, "old generated"), lineWith(t, out, "new generated")
	if !strings.Contains(removed, "-") || !strings.Contains(added, "+") {
		t.Errorf("expected the code's rows signed - and +:\n%s\n%s", removed, added)
	}
	if strings.Index(out, "old generated") > strings.Index(out, "new generated") {
		t.Error("expected the removal above its replacement")
	}
}

func TestSShowsAnExpandedDocumentsSourceAndSAgainShowsItFormatted(t *testing.T) {
	m := press(press(docAndCodeModel(t), "down"), "x")

	source := press(m, "s")
	formatted := press(source, "s")

	out := ansi.Strip(drawnStep(source))
	for _, want := range []string{"# Retries", "A request is retried when the transport fails.", "A request is resent when the transport fails."} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the source line %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "old generated") || !strings.Contains(out, "new generated") {
		t.Errorf("expected the code beside it unchanged:\n%s", out)
	}
	again := ansi.Strip(drawnStep(formatted))
	if strings.Contains(again, "A request is resent when") || !strings.Contains(again, "A request is retried resent when") {
		t.Errorf("expected a second s to show the Document formatted again:\n%s", again)
	}
}

func TestADocumentExpandedWhileSourceIsShownOpensAsSource(t *testing.T) {
	m := docAndCodeModel(t)
	m.showSource = true // s was pressed on an earlier Step
	m = press(m, "down")

	m = press(m, "x")

	out := ansi.Strip(drawnStep(m))
	if !strings.Contains(out, "A request is resent when the transport fails.") || strings.Contains(out, "retried resent") {
		t.Errorf("expected the Document's source lines, not its formatted flow:\n%s", out)
	}
}

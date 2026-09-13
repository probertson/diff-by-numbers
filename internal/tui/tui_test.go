package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// widestLine is the width in cells of the widest row of s, ignoring the ANSI
// styling lipgloss embeds. It is how the wrapping tests assert that nothing runs
// off the right edge of the width the view was given.
func widestLine(s string) int {
	widest := 0
	for _, line := range strings.Split(s, "\n") {
		if w := lipgloss.Width(line); w > widest {
			widest = w
		}
	}
	return widest
}

// newNote builds a textarea configured the way the model's WindowSizeMsg handler
// does, so a view test that renders the note input matches what runs.
func newNote(width int) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "what should change here?"
	ta.CharLimit = 1000
	ta.ShowLineNumbers = false
	ta.SetWidth(max(20, width-4))
	ta.SetHeight(4)
	return ta
}

func TestBriefWrapsTheSourceCitation(t *testing.T) {
	// A long provenance citation used to run off the right edge because the Source
	// line was written without wrapping, unlike Goal and Approach.
	const width = 60
	m := model{
		width:    width,
		viewport: viewport.New(width, 20),
		view: &daemon.ViewWire{
			Posted: true,
			Brief: daemon.BriefWire{
				Ask:                "short ask",
				Approach:           "short approach",
				ProvenanceKind:     "stated",
				ProvenanceCitation: "the session on 2026-09-01 where the reviewer asked for the retry backoff to be threaded through every caller of the fetch layer",
			},
			Repositories: []daemon.RepositoryWire{{Root: "repo", Range: "main"}},
			StepNames:    []string{"one"},
			Seen:         []bool{false},
		},
	}

	out := m.brief()

	if over := widestLine(out); over > width {
		t.Errorf("the Source citation is %d cells wide, over the %d viewport — it did not wrap:\n%s", over, width, out)
	}
}

func TestNoteViewWrapsTheAnchorHeader(t *testing.T) {
	// The anchor's "Re: …" header is one long line; in the New Change Request modal
	// it used to run off the edge instead of wrapping.
	const width = 50
	m := model{
		width: width,
		note:  newNote(width),
		pendingCode: "Re: internal/review/anchor.go:110-125 (new side) — Step \"Compose the paste-ready Anchor text\" in diff-by-numbers\n" +
			"+   110 | func (a Anchor) Render() string {\n",
	}

	out := m.noteView()

	if over := widestLine(out); over > width {
		t.Errorf("the anchor header is %d cells wide, over the %d modal — it did not wrap:\n%s", over, width, out)
	}
}

func TestNoteInputGrowsToTenLinesThenIsBoundedBySpace(t *testing.T) {
	tall := model{width: 80, height: 40, note: newNote(80)}
	tall.setNoteHeight()
	if got := tall.note.Height(); got != 10 {
		t.Errorf("with ample height the note should grow to 10 lines, got %d", got)
	}

	short := model{width: 80, height: 12, note: newNote(80)}
	short.setNoteHeight()
	if got := short.note.Height(); got < 1 || got >= 10 {
		t.Errorf("with little height the note should shrink below 10 (and stay >=1), got %d", got)
	}
}

func TestNoteViewShowsACharacterCounter(t *testing.T) {
	m := model{width: 80, height: 40, note: newNote(80)}
	m.note.SetValue("hello")

	out := m.noteView()

	if !strings.Contains(out, "5/1000") {
		t.Errorf("expected a '5/1000' character counter, got:\n%s", out)
	}
}

func TestListViewTitlePluralizesChangeRequests(t *testing.T) {
	m := model{
		width: 80,
		view:  &daemon.ViewWire{ChangeRequests: []daemon.ChangeRequestWire{{ID: 1, Step: 1, Note: "n"}}},
	}

	out := m.listView()

	if !strings.Contains(out, "1 Change Request") || strings.Contains(out, "Request(s)") {
		t.Errorf("one Change Request should read '1 Change Request', got:\n%s", out)
	}
}

func TestDoneViewPluralizesItsCounts(t *testing.T) {
	m := model{
		view: &daemon.ViewWire{
			StepStatuses:   []string{"seen"},
			ChangeRequests: []daemon.ChangeRequestWire{{ID: 1}},
		},
	}

	out := m.doneView()

	if strings.Contains(out, "(s)") {
		t.Errorf("the finish summary should not use the lazy (s) form, got:\n%s", out)
	}
	if !strings.Contains(out, "1 Step seen") {
		t.Errorf("expected '1 Step seen', got:\n%s", out)
	}
	if !strings.Contains(out, "1 Change Request raised") {
		t.Errorf("expected '1 Change Request raised', got:\n%s", out)
	}
}

func TestListViewWrapsLongNotes(t *testing.T) {
	// A long Change Request note used to print raw, running off the right edge.
	const width = 50
	m := model{
		width: width,
		view: &daemon.ViewWire{
			ChangeRequests: []daemon.ChangeRequestWire{{
				ID: 1, Step: 1, Location: "a.go:1-2 (new)", Anchor: "+ 1 | x",
				Note: "This name reads as a boolean but returns the count; rename it so a caller is not misled into an if-check that is always true.",
			}},
		},
	}

	out := m.listView()

	if over := widestLine(out); over > width {
		t.Errorf("a Change Request note is %d cells wide, over the %d list — it did not wrap:\n%s", over, width, out)
	}
}

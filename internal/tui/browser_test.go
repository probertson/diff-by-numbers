package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/ansi"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// thePage is where the daemon on the default port serves review a1's page.
const thePage = "http://127.0.0.1:7373/reviews/a1/page"

// openingModel is review a1 open on one of its screens, with an opener that
// records what it was asked to open rather than starting a browser.
func openingModel(on mode, view *daemon.ViewWire) (model, *[]string) {
	opened := &[]string{}
	m := model{
		client:     client{base: "http://127.0.0.1:7373", review: "a1"},
		openReview: "a1",
		mode:       on,
		view:       view,
		width:      120, height: 40, ready: true,
		viewport: viewport.New(120, 30),
		openURL: func(url string) error {
			*opened = append(*opened, url)
			return nil
		},
	}
	m.syncCursor()
	return m, opened
}

func overviewView() *daemon.ViewWire {
	return &daemon.ViewWire{Posted: true, ReviewID: "a1", StepCount: 1, StepNames: []string{"one"}, Seen: []bool{false},
		Brief:        daemon.BriefWire{Goal: "the goal"},
		Repositories: []daemon.RepositoryWire{{Root: "repo", Base: "main"}}}
}

func stepView() *daemon.ViewWire {
	view := overviewView()
	view.Position = 1
	view.Step = tallStep()
	return view
}

func TestPOpensThePageFromAnyScreenAndShowsItsURL(t *testing.T) {
	finished := stepView()
	finished.Finished = true
	for _, tc := range []struct {
		screen string
		mode   mode
		view   *daemon.ViewWire
	}{
		{"the Overview", modeReview, overviewView()},
		{"a Step", modeReview, stepView()},
		{"the Comment list", modeList, stepView()},
		{"the conclusion screen", modeConclusion, stepView()},
		{"the handed-off screen", modeDone, finished},
	} {
		t.Run(tc.screen, func(t *testing.T) {
			m, opened := openingModel(tc.mode, tc.view)

			after := press(m, "p")

			if len(*opened) != 1 || (*opened)[0] != thePage {
				t.Errorf("expected p to open %s, opened %v", thePage, *opened)
			}
			if after.mode != tc.mode {
				t.Errorf("expected p to leave %s showing, got mode %v", tc.screen, after.mode)
			}
			if shown := ansi.Strip(after.View()); !strings.Contains(shown, thePage) {
				t.Errorf("expected the page's URL in the status line, got:\n%s", shown)
			}
		})
	}
}

func TestPressingPAgainOpensThePageAgain(t *testing.T) {
	m, opened := openingModel(modeReview, stepView())

	press(press(m, "p"), "p")

	if len(*opened) != 2 {
		t.Errorf("expected each p to open the page, opened %v", *opened)
	}
}

// Where no browser can be opened (over SSH, say), the URL is what the Reviewer
// needs, so it is still shown.
func TestPShowsTheURLWhenNoBrowserCanBeOpened(t *testing.T) {
	m, _ := openingModel(modeReview, stepView())
	m.openURL = func(string) error { return errors.New("no opener") }

	after := press(m, "p")

	if shown := ansi.Strip(after.View()); !strings.Contains(shown, thePage) {
		t.Errorf("expected the URL shown even when no browser opened, got:\n%s", shown)
	}
}

func TestPInTheNoteEditorIsTypedNotOpened(t *testing.T) {
	m, opened := openingModel(modeNote, stepView())
	m.note = newNote(80)
	m.note.Focus()

	after := press(m, "p")

	if len(*opened) != 0 {
		t.Errorf("expected p in the editor not to open anything, opened %v", *opened)
	}
	if after.note.Value() != "p" {
		t.Errorf("expected p typed into the note, got %q", after.note.Value())
	}
}

func TestPAtTheInboxOpensNothing(t *testing.T) {
	m := inboxModel(rows()...)
	var opened []string
	m.openURL = func(url string) error { opened = append(opened, url); return nil }

	press(m, "p")

	if len(opened) != 0 {
		t.Errorf("expected no page opened with no review open, opened %v", opened)
	}
}

func TestTheKeybarOffersThePageAsALink(t *testing.T) {
	m, _ := openingModel(modeReview, stepView())

	bar := m.globalKeys()

	if !strings.Contains(ansi.Strip(bar), "p"+nbsp+"open"+nbsp+"in"+nbsp+"browser") {
		t.Errorf("expected the keybar to offer p open in browser, got %q", ansi.Strip(bar))
	}
	if !strings.Contains(bar, ansi.SetHyperlink(thePage)+"p"+nbsp+"open"+nbsp+"in"+nbsp+"browser"+ansi.ResetHyperlink()) {
		t.Errorf("expected the keybar entry to be a link to the page, got %q", bar)
	}
}

func TestTheOverviewShowsThePagesURLAsALink(t *testing.T) {
	m, _ := openingModel(modeReview, overviewView())

	out := m.brief()

	if !strings.Contains(ansi.Strip(out), thePage) {
		t.Errorf("expected the Overview to show the page's URL, got:\n%s", ansi.Strip(out))
	}
	if !strings.Contains(out, ansi.SetHyperlink(thePage)+thePage+ansi.ResetHyperlink()) {
		t.Errorf("expected the URL to be a link to the page, got %q", out)
	}
}

package tui

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// inboxModel is the TUI at home: the Inbox, holding whatever the daemon does.
func inboxModel(rows ...daemon.InboxRowWire) model {
	m := model{width: 80, height: 24, viewport: viewport.New(80, 18), ready: true}
	after, _ := m.Update(refreshMsg{inbox: rows})
	return after.(model)
}

func pressEnter(m model) model {
	after, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return after.(model)
}

func rows() []daemon.InboxRowWire {
	return []daemon.InboxRowWire{
		{ID: "a1", Label: "auth refactor", State: "needs_you"},
		{ID: "b2", Label: "billing totals", State: "new"},
		{ID: "c3", Label: "retry policy", State: "waiting_on_agent"},
	}
}

func TestTheInboxListsEveryReviewAndWhoItIsWaitingOn(t *testing.T) {
	m := inboxModel(rows()...)

	out := m.content()

	for _, want := range []string{"auth refactor", "billing totals", "retry policy", "needs you", "new", "waiting on agent"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the Inbox to show %q, got:\n%s", want, out)
		}
	}
}

// The Inbox is home whether it holds anything or not, so there is one screen to
// learn rather than a waiting screen that becomes something else (ADR-0015).
func TestAnEmptyInboxIsStillTheInbox(t *testing.T) {
	m := inboxModel()

	out := m.content()

	if m.mode != modeInbox {
		t.Errorf("an empty Inbox is still the Inbox, got mode %v", m.mode)
	}
	if !strings.Contains(out, "Authoring Agent") {
		t.Errorf("an empty Inbox says where a review comes from, got:\n%s", out)
	}
}

func TestEnterOpensTheReviewUnderTheCursor(t *testing.T) {
	m := inboxModel(rows()...)

	m = press(m, "j")
	m = pressEnter(m)

	if m.openReview != "b2" {
		t.Errorf("expected the review under the cursor to open, got %q", m.openReview)
	}
	if m.mode == modeInbox {
		t.Error("opening a review leaves the Inbox")
	}
}

func TestTheInboxKeyReturnsFromAReviewWithoutHandingItOff(t *testing.T) {
	m := inboxModel(rows()...)
	m = pressEnter(m)
	m.view = &daemon.ViewWire{Posted: true, ReviewID: "a1", StepNames: []string{"one"}, Seen: []bool{false}}

	m = press(m, "i")

	if m.mode != modeInbox || m.openReview != "" {
		t.Errorf("i returns to the Inbox, got mode %v holding %q", m.mode, m.openReview)
	}
	if strings.Contains(m.content(), "one") {
		t.Errorf("the Inbox draws the Inbox, not the review left behind:\n%s", m.content())
	}
}

// A Hand Off never moves the Reviewer: the completion screen stays until the
// daemon lets the review go, and only then does the window fall back home.
func TestTheCompletionScreenStaysUntilTheReviewIsReleased(t *testing.T) {
	m := inboxModel(rows()...)
	m = pressEnter(m)
	m.mode = modeDone
	m.openReview = "a1"

	staying, _ := m.Update(refreshMsg{view: &daemon.ViewWire{Posted: true, ReviewID: "a1", Finished: true, Concluded: true}})
	m = staying.(model)
	if m.mode != modeDone {
		t.Fatalf("the completion screen stays while the daemon still holds the review, got mode %v", m.mode)
	}

	released, _ := m.Update(refreshMsg{released: true})

	if got := released.(model); got.mode != modeInbox || got.openReview != "" {
		t.Errorf("once released the window returns to the Inbox, got mode %v holding %q", got.mode, got.openReview)
	}
}

// The Inbox against a real daemon, not a stub: attaching asks it what it is
// holding, and an answer of "nothing" is still the Inbox.
func TestAttachingToARealDaemonOpensOnItsInbox(t *testing.T) {
	port := servePort(t, httptest.NewServer(daemon.New().Handler()))

	m, err := attach(port)

	if err != nil {
		t.Fatalf("attaching to a daemon holding nothing failed: %v", err)
	}
	if m.waiting {
		t.Error("a daemon that answered is not something to wait for")
	}
	if m.mode != modeInbox || len(m.inbox) != 0 {
		t.Errorf("expected an empty Inbox, got mode %v holding %+v", m.mode, m.inbox)
	}
}

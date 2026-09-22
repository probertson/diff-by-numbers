package tui

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// inboxModel is the TUI at home: the Inbox, holding whatever the daemon does.
func inboxModel(rows ...daemon.InboxRowWire) model {
	m := model{width: 80, height: 24, viewport: viewport.New(80, 18), ready: true,
		now: func() time.Time { return fixedNow }}
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

// The Inbox has no review under it, so it shows one row of keys, not the review
// screens' two — and none of the review's own keys, which would do nothing here.
func TestTheInboxShowsOneRowOfKeys(t *testing.T) {
	m := inboxModel(rows()...)

	out := m.View()

	if got := strings.Count(out, "q"+nbsp+"exit"); got != 1 {
		t.Errorf("expected one exit hint at the Inbox, got %d:\n%s", got, out)
	}
	for _, review := range []string{"hand" + nbsp + "off", "g" + nbsp + "Overview", "i" + nbsp + "inbox"} {
		if strings.Contains(out, review) {
			t.Errorf("the Inbox should not offer %q, got:\n%s", review, out)
		}
	}
}

// inboxRow is an Inbox row as the daemon sends it. Its fields are named at the
// call site: a row is mostly numbers, and transposed numbers still compile.
type inboxRow struct {
	id, label, state              string
	round, position, steps, notes int
	since                         time.Duration
}

func (r inboxRow) wire() daemon.InboxRowWire {
	return daemon.InboxRowWire{
		ID: r.id, Label: r.label, State: r.state,
		Repositories: []daemon.InboxRepositoryWire{{Name: "diff-by-numbers", Branch: "feature/auth"}},
		Round:        r.round, Position: r.position, StepCount: r.steps, CommentCount: r.notes,
		PostedAt: fixedNow.Add(-r.since),
	}
}

// fixedNow is when these tests say it is, so a row's age is exact rather than
// whatever the wall clock rounds to.
var fixedNow = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

// The second line is what the Reviewer chooses by without opening anything.
func TestARowSaysWhoseWorkItIsAndHowFarItGot(t *testing.T) {
	m := inboxModel(inboxRow{id: "a1", label: "auth refactor", state: daemon.StateNeedsReviewer, round: 2, position: 4, steps: 9, notes: 2, since: 12 * time.Minute}.wire())

	out := m.content()

	for _, want := range []string{"auth refactor", "needs you", "diff-by-numbers @ feature/auth", "round 2", "Step 4 of 9", "2 Comments", "12m"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the row to say %q, got:\n%s", want, out)
		}
	}
}

func TestARowSaysHowManyStepsAreWaitingWhenItIsUnopened(t *testing.T) {
	m := inboxModel(inboxRow{id: "a1", label: "auth refactor", state: daemon.StateNew, round: 1, steps: 9, since: time.Minute}.wire())

	out := m.content()

	if !strings.Contains(out, "9 Steps") || strings.Contains(out, "Step 0") {
		t.Errorf("an unopened review says how much there is, not where you are: got:\n%s", out)
	}
	if strings.Contains(out, "Comments") {
		t.Errorf("no Comments raised is nothing to say, got:\n%s", out)
	}
}

// A row waiting on its agent says so once, on the first line: the second is for
// what the Reviewer would be picking it up for.
func TestARowWaitingOnItsAgentDoesNotSaySoTwice(t *testing.T) {
	m := inboxModel(inboxRow{id: "a1", label: "auth refactor", state: daemon.StateWaitingOnAgent, round: 2, position: 9, steps: 9, notes: 3, since: time.Hour}.wire())

	out := m.content()

	if strings.Count(out, "waiting on agent") != 1 || strings.Contains(out, "handed off") {
		t.Errorf("whose turn it is belongs on the first line, once, got:\n%s", out)
	}
	if !strings.Contains(out, "round 2") || !strings.Contains(out, "3 Comments") || !strings.Contains(out, "1h") {
		t.Errorf("expected the round, the Comments raised and the age, got:\n%s", out)
	}
}

// Opened and left on the Overview is not the same as never opened, and the row
// says which.
func TestARowSaysWhenTheReviewerOnlyGotAsFarAsTheOverview(t *testing.T) {
	unopened := inboxModel(inboxRow{id: "a1", label: "auth", state: daemon.StateNew, round: 1, steps: 9, since: time.Minute}.wire()).content()
	opened := inboxModel(inboxRow{id: "a1", label: "auth", state: daemon.StateNeedsReviewer, round: 1, steps: 9, since: time.Minute}.wire()).content()

	if !strings.Contains(unopened, "9 Steps") {
		t.Errorf("an unopened review says how much there is, got:\n%s", unopened)
	}
	if !strings.Contains(opened, "Overview") {
		t.Errorf("a review left on the Overview says so, got:\n%s", opened)
	}
}

// A detached HEAD has no branch to name, so the row leaves it out rather than
// trailing an empty "@".
func TestARowWithNoBranchNamesOnlyTheRepository(t *testing.T) {
	detached := inboxRow{id: "a1", label: "auth", state: daemon.StateNew, round: 1, steps: 2, since: time.Minute}.wire()
	detached.Repositories = []daemon.InboxRepositoryWire{{Name: "diff-by-numbers"}}

	out := inboxModel(detached).content()

	if strings.Contains(out, "@") {
		t.Errorf("no branch means no @, got:\n%s", out)
	}
	if !strings.Contains(out, "diff-by-numbers") {
		t.Errorf("the repository is still named, got:\n%s", out)
	}
}

// The cursor follows the Review, so a row arriving above it never changes what
// Enter opens.
func TestAnArrivalDoesNotMoveTheCursorOffItsReview(t *testing.T) {
	m := inboxModel(
		inboxRow{id: "a1", label: "auth", state: daemon.StateNew, round: 1, steps: 2, since: time.Minute}.wire(),
		inboxRow{id: "b2", label: "billing", state: daemon.StateNew, round: 1, steps: 2, since: time.Minute}.wire(),
	)
	m = press(m, "j")

	arrived, _ := m.Update(refreshMsg{inbox: []daemon.InboxRowWire{
		inboxRow{id: "c3", label: "retry", state: daemon.StateNew, round: 1, steps: 2, since: time.Second}.wire(),
		inboxRow{id: "a1", label: "auth", state: daemon.StateNew, round: 1, steps: 2, since: time.Minute}.wire(),
		inboxRow{id: "b2", label: "billing", state: daemon.StateNew, round: 1, steps: 2, since: time.Minute}.wire(),
	}})
	m = arrived.(model)

	if m.inboxCursor != "b2" {
		t.Errorf("the cursor follows the review, not the row, got %q", m.inboxCursor)
	}
	m = pressEnter(m)
	if m.openReview != "b2" {
		t.Errorf("Enter opens the review the cursor was on, got %q", m.openReview)
	}
}

// One branch across every repository is the common case, and repeating it says
// nothing; branches that differ are worth the room.
func TestARowNamesEachBranchOnlyWhenTheyDiffer(t *testing.T) {
	shared := inboxRow{id: "a1", label: "two repos", state: daemon.StateNew, round: 1, steps: 2, since: time.Minute}.wire()
	shared.Repositories = []daemon.InboxRepositoryWire{
		{Name: "portal", Branch: "feature/auth"},
		{Name: "api", Branch: "feature/auth"},
	}
	split := shared
	split.Repositories = []daemon.InboxRepositoryWire{
		{Name: "portal", Branch: "feature/auth"},
		{Name: "api", Branch: "spike"},
	}

	together, apart := inboxModel(shared).content(), inboxModel(split).content()

	if !strings.Contains(together, "portal, api @ feature/auth") {
		t.Errorf("one branch is said once, got:\n%s", together)
	}
	if !strings.Contains(apart, "portal @ feature/auth, api @ spike") {
		t.Errorf("branches that differ are named per repository, got:\n%s", apart)
	}
}

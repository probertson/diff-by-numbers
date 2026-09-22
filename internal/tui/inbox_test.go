package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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

// Dismissing discards what the Reviewer raised, so it asks first.
func TestDismissingFromTheInboxAsksFirst(t *testing.T) {
	m := inboxModel(
		inboxRow{id: "a1", label: "auth", state: daemon.StateNew, round: 1, steps: 2, notes: 2, since: time.Minute}.wire(),
		inboxRow{id: "b2", label: "billing", state: daemon.StateNew, round: 1, steps: 2, since: time.Minute}.wire(),
	)

	m = press(m, "d")

	if m.dismissing != "a1" {
		t.Fatalf("expected the guard armed on the review under the cursor, got %q", m.dismissing)
	}
	out := m.View()
	if !strings.Contains(out, "auth") || !strings.Contains(out, "(y/n)") {
		t.Errorf("the guard names the review it would discard, got:\n%s", out)
	}
	if !strings.Contains(out, "2 Comments") {
		t.Errorf("the guard says what is lost, got:\n%s", out)
	}
}

// The rows re-sort under an armed guard as agents post, so the answer lands on
// the review the guard named rather than on whatever row is there by then.
func TestTheGuardHoldsTheReviewItNamedWhileTheRowsMove(t *testing.T) {
	m := inboxModel(
		inboxRow{id: "a1", label: "auth", state: daemon.StateNew, round: 1, steps: 2, since: time.Minute}.wire(),
		inboxRow{id: "b2", label: "billing", state: daemon.StateNew, round: 1, steps: 2, since: time.Minute}.wire(),
	)
	m = press(m, "d")

	moved, _ := m.Update(refreshMsg{inbox: []daemon.InboxRowWire{
		inboxRow{id: "b2", label: "billing", state: daemon.StateNew, round: 1, steps: 2, since: time.Minute}.wire(),
		inboxRow{id: "a1", label: "auth", state: daemon.StateNew, round: 1, steps: 2, since: time.Minute}.wire(),
	}})
	m = moved.(model)

	if m.dismissing != "a1" {
		t.Errorf("the guard still means the review it named, got %q", m.dismissing)
	}
	if !strings.Contains(m.View(), "auth") {
		t.Errorf("and still says so, got:\n%s", m.View())
	}
}

func TestSayingNoOrEscapeLeavesTheReviewWhereItIs(t *testing.T) {
	for _, key := range []string{"n", "esc"} {
		t.Run(key, func(t *testing.T) {
			dismissed := make(chan string, 1)
			m := inboxOn(t, dismissed)

			m = press(press(m, "d"), key)

			if m.dismissing != "" {
				t.Errorf("%s disarms the guard, got %q", key, m.dismissing)
			}
			select {
			case id := <-dismissed:
				t.Errorf("nothing should have been dismissed, got %q", id)
			default:
			}
		})
	}
}

func TestSayingYesDismissesTheReviewUnderTheCursor(t *testing.T) {
	dismissed := make(chan string, 1)
	m := inboxOn(t, dismissed)

	m = press(press(m, "d"), "y")

	if m.dismissing != "" {
		t.Error("answering disarms the guard")
	}
	select {
	case id := <-dismissed:
		if id != "a1" {
			t.Errorf("expected the review under the cursor dismissed, got %q", id)
		}
	case <-time.After(time.Second):
		t.Error("expected the daemon to be told to dismiss the review")
	}
}

// A daemon that refuses says why, and the review stays where it is.
func TestARefusedDismissalIsReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "review \"a1\" is already over; there is nothing to dismiss", http.StatusConflict)
	}))
	t.Cleanup(server.Close)
	m := inboxModel(inboxRow{id: "a1", label: "auth", state: daemon.StateNew, round: 1, steps: 2, since: time.Minute}.wire())
	m.client = client{base: server.URL}

	m = press(press(m, "d"), "y")

	if !strings.Contains(m.View(), "already over") {
		t.Errorf("expected the refusal shown to the Reviewer, got:\n%s", m.View())
	}
}

// A window holding a review someone dismissed — here or in another window — has
// nothing left to show, so it goes home.
func TestAWindowHoldingADismissedReviewReturnsToTheInbox(t *testing.T) {
	m := inboxModel(inboxRow{id: "a1", label: "auth", state: daemon.StateNew, round: 1, steps: 2, since: time.Minute}.wire())
	m = pressEnter(m)

	gone, _ := m.Update(refreshMsg{view: &daemon.ViewWire{Posted: true, ReviewID: "a1", Dismissed: true}})

	if got := gone.(model); got.mode != modeInbox || got.openReview != "" {
		t.Errorf("expected the window back at the Inbox, got mode %v holding %q", got.mode, got.openReview)
	}
}

// inboxOn is an Inbox whose daemon records what it was asked to dismiss.
func inboxOn(t *testing.T, dismissed chan<- string) model {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/reviews/"), "/dismiss"); ok {
			dismissed <- id
		}
		fmt.Fprintln(w, "ok")
	}))
	t.Cleanup(server.Close)
	m := inboxModel(
		inboxRow{id: "a1", label: "auth", state: daemon.StateNew, round: 1, steps: 2, since: time.Minute}.wire(),
		inboxRow{id: "b2", label: "billing", state: daemon.StateNew, round: 1, steps: 2, since: time.Minute}.wire(),
	)
	m.client = client{base: server.URL}
	return m
}

// Dismissal end to end: a real daemon holding a real review, the window asking
// it to let the review go, and the row leaving the Inbox.
func TestDismissingAgainstARealDaemonEmptiesTheInbox(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	port := servePort(t, server)
	reviewID := postedReview(t, server.URL)
	m, err := attach(port)
	if err != nil {
		t.Fatalf("attaching failed: %v", err)
	}
	m.width, m.height, m.viewport, m.ready = 80, 24, viewport.New(80, 18), true
	if len(m.inbox) != 1 || m.inbox[0].ID != reviewID {
		t.Fatalf("expected the posted review in the Inbox, got %+v", m.inbox)
	}

	m = press(press(m, "d"), "y")
	m = settle(m)

	if len(m.inbox) != 0 {
		t.Errorf("the dismissed review leaves the Inbox, got %+v", m.inbox)
	}
	if !strings.Contains(m.content(), "Nothing to review") {
		t.Errorf("expected an empty Inbox, got:\n%s", m.content())
	}
}

// settle runs the model's own refresh, so a test sees what the next poll would.
func settle(m model) model {
	after, _ := m.Update(m.refresh()())
	return after.(model)
}

// postedReview posts a Round to a real daemon over MCP, as an agent does, and
// returns the review id.
func postedReview(t *testing.T, baseURL string) string {
	t.Helper()
	root := reviewableRepo(t)
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: baseURL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("could not connect to the daemon: %v", err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "post_round", Arguments: map[string]any{
		"label":        "auth refactor",
		"brief":        map[string]any{"goal": "g", "approach": "a"},
		"repositories": []any{map[string]any{"root": root, "base": "main"}},
		"steps": []any{map[string]any{
			"name": "the change", "explanation": "why",
			"excerpts": []any{map[string]any{"file": "fetch.ts", "side": "new", "first_line": 1, "last_line": 4}},
		}},
	}})
	if err != nil {
		t.Fatalf("post_round failed: %v", err)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var posted struct {
		Accepted bool   `json:"accepted"`
		ReviewID string `json:"review_id"`
		Problems []struct {
			Detail string `json:"detail"`
		} `json:"problems"`
	}
	if err := json.Unmarshal(encoded, &posted); err != nil {
		t.Fatal(err)
	}
	if !posted.Accepted {
		t.Fatalf("expected the Round accepted, got %+v", posted.Problems)
	}
	return posted.ReviewID
}

// reviewableRepo is a repository with one changed line to review.
func reviewableRepo(t *testing.T) string {
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
	os.WriteFile(filepath.Join(root, "fetch.ts"), []byte("a\nb\nc\n"), 0o644)
	git("init", "-q", "-b", "main")
	git("add", ".")
	git("commit", "-qm", "initial")
	git("checkout", "-q", "-b", "feature")
	os.WriteFile(filepath.Join(root, "fetch.ts"), []byte("a\nb\nc\nADDED\n"), 0o644)
	return root
}

package tui

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// closedPort is a port nothing is listening on: taken from the OS and released
// again, so a connection to it is refused rather than answered.
func closedPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not take a port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	return port
}

func TestAttachingWithNoDaemonWaitsRatherThanFailing(t *testing.T) {
	port := closedPort(t)

	m, err := attach(port)

	if err != nil {
		t.Fatalf("attaching with no daemon failed: %v", err)
	}
	if !m.waiting {
		t.Error("attaching with no daemon should leave the model waiting for one")
	}
}

// servePort starts server and returns the loopback port it answers on, which is
// what the TUI attaches by.
func servePort(t *testing.T, server *httptest.Server) int {
	t.Helper()
	t.Cleanup(server.Close)
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("could not read the test server's address: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("could not read the test server's port: %v", err)
	}

	return port
}

// Waiting only helps when a daemon might still turn up. Something already
// answering on the port that is not dbn will never become dbn, so that still
// belongs on stderr at startup.
func TestAttachingToSomethingThatIsNotTheDaemonStillFails(t *testing.T) {
	port := servePort(t, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not dbn", http.StatusNotFound)
	})))

	m, err := attach(port)

	if err == nil {
		t.Fatalf("attaching to a server that is not dbn should fail; got waiting=%v", m.waiting)
	}
}

// waitingOn is the model starting with no daemon on port really produces, sized
// as the first WindowSizeMsg would size it. Built through attach rather than by
// hand, so a wait these tests describe is a wait the TUI can actually be in.
func waitingOn(t *testing.T, port int) model {
	t.Helper()
	m, err := attach(port)
	if err != nil {
		t.Fatalf("attaching with no daemon on port %d failed: %v", port, err)
	}
	if !m.waiting {
		t.Fatalf("attaching with no daemon on port %d did not start a wait", port)
	}
	m.width, m.height = 80, 24
	m.viewport = viewport.New(80, 18)
	m.ready = true

	return m
}

func waitingModel(t *testing.T) model { return waitingOn(t, closedPort(t)) }

func TestTheWaitingScreenSaysWhatItIsWaitingFor(t *testing.T) {
	m := waitingModel(t)

	header, body := m.headerLine(), m.content()

	if !strings.Contains(header, "waiting for the dbn daemon") {
		t.Errorf("the header should say it is waiting for the daemon, got %q", header)
	}
	if strings.Contains(header, "lost the daemon") {
		t.Errorf("waiting for a first daemon is not losing one, got %q", header)
	}
	if !strings.Contains(body, "Authoring Agent") || !strings.Contains(body, "Round") {
		t.Errorf("the body should say the daemon starts when the Authoring Agent posts a Round, got %q", body)
	}
}

// A wait with no end in sight needs a nudge: the daemon only ever appears if the
// agent is actually wired up to dbn, and the port it would appear on is the
// first thing to check.
func TestALongWaitAddsAHintNamingThePort(t *testing.T) {
	port := closedPort(t)
	m := waitingOn(t, port)
	// Deliberately not the 30s default: the threshold the test sets is the one
	// that has to decide, or the field is not the seam it claims to be.
	m.hintAfter = time.Minute
	m.waitingSince = time.Now().Add(-2 * time.Minute)

	body := m.content()

	if !strings.Contains(body, "still waiting") {
		t.Errorf("a wait past the threshold should add the hint, got %q", body)
	}
	if !strings.Contains(body, strconv.Itoa(port)) {
		t.Errorf("the hint should name port %d, the one being watched, got %q", port, body)
	}
	if !strings.Contains(body, "-port") || !strings.Contains(body, "$DBN_PORT") {
		t.Errorf("the hint should say how to watch another port, got %q", body)
	}
}

// The Reviewer who opens the TUI a beat before their agent posts is in the
// ordinary case, and should not be told to go and check their configuration.
func TestAFreshWaitHoldsTheHintBack(t *testing.T) {
	m := waitingModel(t)
	m.hintAfter = time.Hour

	body := m.content()

	if strings.Contains(body, "still waiting") {
		t.Errorf("a wait inside the threshold should hold the hint back, got %q", body)
	}
}

func TestADaemonAppearingEndsTheWait(t *testing.T) {
	m := waitingModel(t)

	updated, _ := m.Update(refreshMsg{view: &daemon.ViewWire{}})
	after := updated.(model)

	if after.waiting {
		t.Error("a daemon that answered should end the wait")
	}
	if header := after.headerLine(); !strings.Contains(header, "no Round posted") {
		t.Errorf("after connecting the header should be the ordinary one, got %q", header)
	}
}

// The daemon that turns up may be a different build from this TUI — a stale one
// left running from before an update is exactly how that happens — so the first
// connect asks who it is, just as a reconnect does.
func TestTheFirstConnectAsksWhichBuildTheDaemonIs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(daemon.StatusWire{Version: "0.9.9"})
	}))
	t.Cleanup(server.Close)
	m := waitingModel(t)
	m.client = client{base: server.URL, review: "a1"}

	_, cmd := m.Update(refreshMsg{view: &daemon.ViewWire{}})

	if cmd == nil {
		t.Fatal("the first connect asked the daemon nothing about itself")
	}
	msg, ok := cmd().(statusMsg)
	if !ok {
		t.Fatalf("the first connect ran some other command, got %T", cmd())
	}
	if msg.err != nil || msg.status == nil {
		t.Fatalf("the status read failed: %v", msg.err)
	}
	if msg.status.Version != "0.9.9" {
		t.Errorf("the status read reported version %q, want 0.9.9", msg.status.Version)
	}
}

// Waiting and losing are different situations with different words, so a poll
// that still finds nothing must not quietly turn one into the other.
func TestAPollThatStillFindsNothingKeepsWaiting(t *testing.T) {
	m := waitingModel(t)

	updated, _ := m.Update(refreshMsg{err: errNoDaemon{errUnreachable{}}})
	after := updated.(model)

	if !after.waiting {
		t.Error("a poll that found no daemon should leave the wait running")
	}
	if after.lostErr != nil {
		t.Errorf("a daemon never had cannot be lost, got lostErr %v", after.lostErr)
	}
	if header := after.headerLine(); !strings.Contains(header, "waiting for the dbn daemon") {
		t.Errorf("the header should still be the waiting one, got %q", header)
	}
}

// The wait has no timeout, so leaving it is the Reviewer's call and the keybar
// has to say so.
func TestTheWaitEndsOnQ(t *testing.T) {
	m := waitingModel(t)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})

	if !isQuit(cmd) {
		t.Error("q should exit while waiting for a daemon")
	}
	if keys := m.globalKeys(); !strings.Contains(keys, "q"+nbsp+"exit") {
		t.Errorf("the keybar should offer q exit while waiting, got %q", keys)
	}
}

// Waiting is only ever waiting for a daemon. Something else taking the port
// mid-wait is an answer, not a silence, and no amount of waiting turns it into
// dbn — so it ends the wait the same way it would have ended startup.
func TestSomethingElseAnsweringDuringAWaitEndsIt(t *testing.T) {
	port := closedPort(t)
	m := waitingOn(t, port)

	updated, cmd := m.Update(refreshMsg{err: errors.New("the server answered 404 Not Found — is that dbn?")})
	after := updated.(model)

	if after.fatalErr == nil {
		t.Fatal("a server that is not dbn should end the wait with an error")
	}
	if !strings.Contains(after.fatalErr.Error(), strconv.Itoa(port)) || !strings.Contains(after.fatalErr.Error(), "is that dbn?") {
		t.Errorf("the error should name the port and what answered, got %q", after.fatalErr)
	}
	if !isQuit(cmd) {
		t.Error("the wait should end rather than run on under a header that cannot come true")
	}
}

// The wait usually ends on the Round itself: the agent posts, and the
// first poll that answers carries the whole review rather than an empty daemon.
func TestARoundArrivingEndsTheWaitOnTheOverview(t *testing.T) {
	m := waitingModel(t)

	updated, _ := m.Update(refreshMsg{view: &daemon.ViewWire{
		Posted:    true,
		StepCount: 2,
		StepNames: []string{"one", "two"},
		Seen:      []bool{false, false},
	}})
	after := updated.(model)

	if after.waiting {
		t.Error("a posted Round should end the wait")
	}
	if header := after.headerLine(); !strings.Contains(header, "Overview") || !strings.Contains(header, "2 Steps ahead") {
		t.Errorf("the wait should end on the Overview, got %q", header)
	}
	if after.mode != modeReview {
		t.Errorf("the wait should end on the walking screen, got mode %v", after.mode)
	}
}

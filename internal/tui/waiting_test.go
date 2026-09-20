package tui

import (
	"encoding/json"
	"fmt"
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

// closedPort is a port nothing is listening on: taken and released, so it is
// free for as long as this test needs it to answer "connection refused".
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

// waitingModel is the model as attach leaves it when no daemon answered, sized
// so the view functions have room to render.
func waitingModel() model {
	return model{
		port:         7373,
		waiting:      true,
		waitingSince: time.Now(),
		width:        80,
		height:       24,
		viewport:     viewport.New(80, 18),
		ready:        true,
	}
}

func TestTheWaitingScreenSaysWhatItIsWaitingFor(t *testing.T) {
	m := waitingModel()

	header, body := m.headerLine(), m.content()

	if !strings.Contains(header, "waiting for the dbn daemon") {
		t.Errorf("the header should say it is waiting for the daemon, got %q", header)
	}
	if strings.Contains(header, "lost the daemon") {
		t.Errorf("waiting for a first daemon is not losing one, got %q", header)
	}
	if !strings.Contains(body, "Authoring Agent") || !strings.Contains(body, "Walkthrough") {
		t.Errorf("the body should say the daemon starts when the Authoring Agent posts a Walkthrough, got %q", body)
	}
}

// A wait with no end in sight needs a nudge: the daemon only ever appears if the
// agent is actually wired up to dbn, and the port it would appear on is the
// first thing to check.
func TestALongWaitAddsAHintNamingThePort(t *testing.T) {
	m := waitingModel()
	m.waitingSince = time.Now().Add(-time.Minute)
	m.hintAfter = 30 * time.Second

	body := m.content()

	if !strings.Contains(body, "still waiting") {
		t.Errorf("a wait past the threshold should add the hint, got %q", body)
	}
	if !strings.Contains(body, "7373") {
		t.Errorf("the hint should name the port being watched, got %q", body)
	}
	if !strings.Contains(body, "-port") || !strings.Contains(body, "$DBN_PORT") {
		t.Errorf("the hint should say how to watch another port, got %q", body)
	}
}

// The Reviewer who opens the TUI a beat before their agent posts is in the
// ordinary case, and should not be told to go and check their configuration.
func TestAFreshWaitHoldsTheHintBack(t *testing.T) {
	m := waitingModel()
	m.hintAfter = time.Hour

	body := m.content()

	if strings.Contains(body, "still waiting") {
		t.Errorf("a wait inside the threshold should hold the hint back, got %q", body)
	}
}

func TestADaemonAppearingEndsTheWait(t *testing.T) {
	m := waitingModel()

	updated, _ := m.Update(refreshMsg{view: &daemon.ViewWire{}})
	after := updated.(model)

	if after.waiting {
		t.Error("a daemon that answered should end the wait")
	}
	if header := after.headerLine(); !strings.Contains(header, "no Walkthrough posted") {
		t.Errorf("after connecting the header should be the ordinary one, got %q", header)
	}
}

// The daemon that turns up may be a different build from this TUI — a stale one
// left running from before an update is exactly how that happens — so the first
// connect asks who it is, just as a reconnect does.
func TestTheFirstConnectAsksWhichBuildTheDaemonIs(t *testing.T) {
	port := servePort(t, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(daemon.StatusWire{Version: "0.9.9"})
	})))
	m := waitingModel()
	m.client = client{base: fmt.Sprintf("http://127.0.0.1:%d", port)}

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
	m := waitingModel()

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
	m := waitingModel()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})

	if !isQuit(cmd) {
		t.Error("q should exit while waiting for a daemon")
	}
	if keys := m.globalKeys(); !strings.Contains(keys, "q"+nbsp+"exit") {
		t.Errorf("the keybar should offer q exit while waiting, got %q", keys)
	}
}

package tui

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// The handed-off screens say "Your agent has been told" only when a `dbn wait`
// really was handed the Hand Off, and otherwise send the Reviewer to tell it
// (ADR-0016). Driven against a real daemon, since the screen is only as
// trustworthy as what the daemon reports.

// handedOff opens the review in a window attached to the daemon, optionally
// raises a Comment first, and hands it off with h — with a `dbn wait` poll open
// when waiting is set.
func handedOff(t *testing.T, raiseOne, waiting bool) model {
	t.Helper()
	server := httptest.NewServer(daemon.New().Handler())
	port := servePort(t, server)
	reviewID := postedReview(t, server.URL)
	if raiseOne {
		reviewerAct(t, server.URL, reviewID, "goto/1", nil)
		reviewerAct(t, server.URL, reviewID, "comment", map[string]any{
			"excerpt_index": 0, "start": map[string]any{"line": 4}, "end": map[string]any{"line": 4}, "note": "rename this",
		})
	}
	if waiting {
		go func() {
			if response, err := http.Get(server.URL + "/reviews/" + reviewID + "/wait"); err == nil {
				response.Body.Close()
			}
		}()
		time.Sleep(100 * time.Millisecond) // the poll is open before the Hand Off
	}
	m, err := attach(port)
	if err != nil {
		t.Fatalf("attaching failed: %v", err)
	}
	m.width, m.height, m.viewport, m.ready = 100, 30, viewport.New(100, 24), true
	m = settle(pressEnter(m))

	return settle(press(m, "h"))
}

// reviewerAct sends one of the Reviewer's acts straight to the daemon.
func reviewerAct(t *testing.T, baseURL, reviewID, act string, body any) {
	t.Helper()
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		payload = bytes.NewReader(encoded)
	}
	response, err := http.Post(baseURL+"/reviews/"+reviewID+"/"+act, "application/json", payload)
	if err != nil {
		t.Fatalf("POST %s: %v", act, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		answer, _ := io.ReadAll(response.Body)
		t.Fatalf("POST %s answered %s: %s", act, response.Status, answer)
	}
}

// screen is the handed-off screen as drawn, with wrapping undone so a sentence
// is found wherever the width broke it.
func screen(m model) string {
	return strings.Join(strings.Fields(m.View()), " ")
}

func TestTheHandedOffScreenSaysTheAgentWasToldWhenItWas(t *testing.T) {
	m := handedOff(t, true, true)

	if got := screen(m); !strings.Contains(got, "Your agent has been told.") || strings.Contains(got, "Tell your agent") {
		t.Errorf("expected the screen to say the agent was told, got:\n%s", m.View())
	}
}

func TestTheHandedOffScreenSaysToTellTheAgentWhenNothingWasListening(t *testing.T) {
	m := handedOff(t, true, false)

	if got := screen(m); !strings.Contains(got, "Tell your agent you've handed off.") || strings.Contains(got, "has been told") {
		t.Errorf("expected the screen to send the Reviewer to their agent, got:\n%s", m.View())
	}
}

func TestTheCompletionScreenSaysTheAgentWasToldWhenItWas(t *testing.T) {
	m := handedOff(t, false, true)

	got := screen(m)
	if !strings.Contains(got, "Review complete") {
		t.Fatalf("expected the completion screen, got:\n%s", m.View())
	}
	if !strings.Contains(got, "Your agent has been told.") || strings.Contains(got, "Tell your agent") {
		t.Errorf("expected the screen to say the agent was told, got:\n%s", m.View())
	}
}

func TestTheCompletionScreenSaysToTellTheAgentWhenNothingWasListening(t *testing.T) {
	m := handedOff(t, false, false)

	got := screen(m)
	if !strings.Contains(got, "Review complete") {
		t.Fatalf("expected the completion screen, got:\n%s", m.View())
	}
	if !strings.Contains(got, "Tell your agent you've handed off.") || strings.Contains(got, "has been told") {
		t.Errorf("expected the screen to send the Reviewer to their agent, got:\n%s", m.View())
	}
}

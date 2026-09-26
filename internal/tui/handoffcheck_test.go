package tui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// pathRecorder keeps the path of every request the TUI makes, so a test can
// tell whether a Hand Off was sent.
type pathRecorder struct {
	mu    sync.Mutex
	paths []string
}

func (r *pathRecorder) sent(path string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.paths {
		if strings.HasSuffix(p, path) {
			return true
		}
	}
	return false
}

// checkingModel is a review on its last Step with the given questions in the
// Round, talking to a server that records what it is sent.
func checkingModel(t *testing.T, mode mode, questions ...daemon.QuestionWire) (model, *pathRecorder) {
	t.Helper()
	rec := &pathRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.paths = append(rec.paths, r.URL.Path)
		rec.mu.Unlock()
	}))
	t.Cleanup(server.Close)
	m := model{
		client: client{base: server.URL, review: "a1"},
		mode:   mode,
		view: &daemon.ViewWire{Posted: true, Position: 2, StepCount: 2,
			Step: tallStep(), Questions: questions},
		width: 100, height: 30, ready: true,
	}
	m.syncCursor()
	return m, rec
}

func onStep(step int, id int, text, answer string) daemon.QuestionWire {
	return daemon.QuestionWire{ID: id, Step: step, Text: text, Answer: answer}
}

func TestHandingOffWithAnUnansweredQuestionAsksFirst(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode mode
		arm  func(model) model
	}{
		{"from a Step", modeReview, func(m model) model { return m }},
		{"from the conclusion screen", modeConclusion, func(m model) model { return m }},
		{"from the quit guard", modeReview, func(m model) model { m.confirmingQuit = true; return m }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, rec := checkingModel(t, tc.mode, onStep(1, 1, "3 or 5?", ""), onStep(2, 2, "keep the name?", "yes"))
			m = tc.arm(m)

			after, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
			am := after.(model)

			if am.mode != modeUnanswered {
				t.Fatalf("expected the unanswered question to be listed first, got mode %v", am.mode)
			}
			if rec.sent("/finish") {
				t.Error("the check comes before the Hand Off, not after it")
			}
			out := am.handOffCheckView()
			if !strings.Contains(out, "3 or 5?") || strings.Contains(out, "keep the name?") {
				t.Errorf("expected only the unanswered question listed, got:\n%s", out)
			}
		})
	}
}

func TestConfirmingTheCheckHandsOff(t *testing.T) {
	m, rec := checkingModel(t, modeReview, onStep(1, 1, "3 or 5?", ""))
	checking := press(m, "h")

	after := press(checking, "h")

	if after.mode != modeDone {
		t.Errorf("expected h to hand off anyway, got mode %v", after.mode)
	}
	if !rec.sent("/finish") {
		t.Error("expected the Hand Off to be sent once confirmed")
	}
}

func TestDecliningTheCheckReturnsToWhereTheReviewerWas(t *testing.T) {
	for _, from := range []mode{modeReview, modeConclusion} {
		m, rec := checkingModel(t, from, onStep(1, 1, "3 or 5?", ""))
		checking := press(m, "h")

		back, _ := checking.Update(tea.KeyMsg{Type: tea.KeyEsc})

		if back.(model).mode != from {
			t.Errorf("expected esc to return to mode %v, got %v", from, back.(model).mode)
		}
		if rec.sent("/finish") {
			t.Error("declining must not hand off")
		}
	}
}

func TestChoosingAListedQuestionGoesToWhereItWasAsked(t *testing.T) {
	for _, tc := range []struct {
		name     string
		question daemon.QuestionWire
		want     string
	}{
		{"a Step's", onStep(2, 1, "3 or 5?", ""), "/goto/2"},
		{"the Round's", onStep(0, 1, "right approach?", ""), "/goto/0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, rec := checkingModel(t, modeConclusion, tc.question)
			checking := press(m, "h")

			chosen, _ := checking.Update(tea.KeyMsg{Type: tea.KeyEnter})

			if chosen.(model).mode != modeReview {
				t.Errorf("expected to be back reviewing, got mode %v", chosen.(model).mode)
			}
			if !rec.sent(tc.want) {
				t.Errorf("expected %s, got %v", tc.want, rec.paths)
			}
		})
	}
}

func TestHandingOffWithEveryQuestionAnsweredDoesNotAsk(t *testing.T) {
	m, rec := checkingModel(t, modeReview, onStep(1, 1, "3 or 5?", "3"))

	after := press(m, "h")

	if after.mode != modeDone || !rec.sent("/finish") {
		t.Errorf("expected an answered Round to hand off at once, got mode %v", after.mode)
	}
}

package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// cursorReports stands in for the daemon's /cursor: it records each cursor
// the TUI reports, in order.
type cursorReports struct {
	mu   sync.Mutex
	sent []daemon.CursorWire
}

func (r *cursorReports) last(t *testing.T) daemon.CursorWire {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.sent) == 0 {
		t.Fatal("expected the cursor reported, but nothing was")
	}
	return r.sent[len(r.sent)-1]
}

func (r *cursorReports) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sent)
}

// followedModel is review a1 open on Step 1 showing step, talking to a daemon
// that records the cursor reports it gets.
func followedModel(t *testing.T, step *daemon.StepWire) (model, *cursorReports) {
	t.Helper()
	reports := &cursorReports{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/reviews/a1/cursor") {
			http.NotFound(w, r)
			return
		}
		var cursor daemon.CursorWire
		if err := json.NewDecoder(r.Body).Decode(&cursor); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		reports.mu.Lock()
		reports.sent = append(reports.sent, cursor)
		reports.mu.Unlock()
	}))
	t.Cleanup(server.Close)
	m := model{
		client:     client{base: server.URL, review: "a1"},
		openReview: "a1",
		mode:       modeReview,
		view:       &daemon.ViewWire{Posted: true, ReviewID: "a1", Position: 1, StepCount: 1, Step: step},
		width:      100, height: 40, ready: true,
	}
	m.syncCursor()
	return m, reports
}

func TestMovingTheCursorOntoABlockReportsTheBlock(t *testing.T) {
	m, reports := followedModel(t, twoParagraphs())

	press(m, "down") // from the heading onto the first paragraph

	want := daemon.CursorWire{Step: 1, Excerpt: 0, Block: 2}
	if got := reports.last(t); got != want {
		t.Errorf("expected the cursor reported as %+v, got %+v", want, got)
	}
}

func TestMovingTheCursorOntoALineOfCodeReportsTheLine(t *testing.T) {
	m, reports := followedModel(t, tallStep())

	press(m, "down")

	want := daemon.CursorWire{Step: 1, Excerpt: 0, Side: "new", Line: 2}
	if got := reports.last(t); got != want {
		t.Errorf("expected the cursor reported as %+v, got %+v", want, got)
	}
}

// The page draws every Document rendered, so a line of one shown here as
// source is followed as the block it lies in.
func TestALineOfADocumentShownAsSourceReportsItsBlock(t *testing.T) {
	m, reports := followedModel(t, twoParagraphs())
	m = press(m, "s")

	press(press(press(m, "down"), "down"), "down") // onto line 4, the first paragraph's second

	want := daemon.CursorWire{Step: 1, Excerpt: 0, Block: 2}
	if got := reports.last(t); got != want {
		t.Errorf("expected the cursor reported as %+v, got %+v", want, got)
	}
}

func TestAKeyThatLeavesTheCursorWhereItIsReportsNothingMore(t *testing.T) {
	m, reports := followedModel(t, tallStep())
	m = press(m, "down")
	before := reports.count()

	press(m, "w") // wrapping redraws the pane but moves nothing

	if after := reports.count(); after != before {
		t.Errorf("expected no further report for a cursor that did not move, got %d more", after-before)
	}
}

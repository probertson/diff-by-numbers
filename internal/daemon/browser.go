package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// The Browser Renderer: a page per Review, drawn on the daemon's own port, that
// shows the Step the Reviewer is on in the TUI and follows them as they move.
// It only draws. Nothing on it changes the review, so every route here is a GET,
// and the page is rendered here from the same view the TUI draws (ViewWire)
// rather than from a JSON API of its own (#139).
//
// Routes, beside the Reviewer's endpoints:
//   - /reviews/{review}/page         the whole page
//   - /reviews/{review}/page/stop    the stop the Reviewer is at, which the
//     page's script swaps in
//   - /reviews/{review}/page/events  the review's change stream (server-sent
//     events)
//   - /reviews/{review}/page/cursor  where the TUI's cursor is, which the
//     page's script brightens
//
// The one thing the page follows that the review does not hold is the TUI's
// cursor, so the TUI reports it (POST /reviews/{review}/cursor) each time it
// moves, and dbn holds it for the review beside the Step position (#187).

// PagePath is where a review's page is served, under the daemon's base URL. The
// TUI opens and shows it; the daemon serves it.
func PagePath(reviewID string) string { return "/reviews/" + reviewID + "/page" }

// pageRedraw is the event telling a page to fetch its stop again: the
// Reviewer moved, or something the stop draws changed. It carries nothing,
// since the page always reads what is current.
const pageRedraw = "redraw"

// pageCursor is the event telling a page the TUI's cursor moved. Like a
// redraw it carries nothing: the page reads where the cursor is now.
const pageCursor = "cursor"

// CursorWire is where the TUI's cursor is on a Step, as the TUI reports it and
// the page reads it: a block of a Document Excerpt, or a line (a side and a
// line number). Step is the review's position the cursor was reported at, so a
// cursor left on one Step is never drawn on another. Excerpt is the Step's own
// Excerpt it is in, or -1 for anywhere the page does not draw (an
// Acknowledgement).
type CursorWire struct {
	Step    int    `json:"step"`
	Excerpt int    `json:"excerpt"`
	Block   int    `json:"block,omitempty"`
	Side    string `json:"side,omitempty"`
	Line    int    `json:"line,omitempty"`
}

// on reports whether a row of an Excerpt is where the cursor is: any row of
// its block, or else its own line.
func (c *CursorWire) on(line LineWire, excerpt ExcerptWire) bool {
	switch {
	case c == nil || line.Signpost:
		return false
	case c.Block > 0:
		return line.Block == c.Block
	}
	return line.Number == c.Line && sideOf(line, excerpt) == c.Side
}

// in is the cursor if it is in the Step's own Excerpt ei, or nil.
func (c *CursorWire) in(ei int) *CursorWire {
	if c == nil || c.Excerpt != ei {
		return nil
	}
	return c
}

// pageFeed is every open page's subscription to its review's change stream.
type pageFeed struct {
	mu   sync.Mutex
	subs map[string]map[chan string]struct{}
}

// subscribe opens a subscription to a review's events, and returns the
// function that closes it.
func (f *pageFeed) subscribe(reviewID string) (<-chan string, func()) {
	events := make(chan string, 8)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subs == nil {
		f.subs = map[string]map[chan string]struct{}{}
	}
	if f.subs[reviewID] == nil {
		f.subs[reviewID] = map[chan string]struct{}{}
	}
	f.subs[reviewID][events] = struct{}{}
	return events, func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		delete(f.subs[reviewID], events)
		if len(f.subs[reviewID]) == 0 {
			delete(f.subs, reviewID)
		}
	}
}

// publish sends an event to every page open on a review. It never blocks: a
// page that has fallen behind already has an event waiting, and since an event
// only says "read again", one waiting is as good as several.
func (f *pageFeed) publish(reviewID, event string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for events := range f.subs[reviewID] {
		select {
		case events <- event:
		default:
		}
	}
}

// changed tells the pages open on a review that what they draw may have
// changed.
func (d *Daemon) changed(reviewID string) { d.pages.publish(reviewID, pageRedraw) }

// browserRoutes adds the Browser Renderer's routes to the daemon's mux.
func (d *Daemon) browserRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /reviews/{review}/page", d.onReview(func(session *review.Session, w http.ResponseWriter, _ *http.Request) {
		d.servePage(w, "page", d.pageOf(session))
	}))
	mux.HandleFunc("GET /reviews/{review}/page/stop", d.onReview(func(session *review.Session, w http.ResponseWriter, _ *http.Request) {
		d.servePage(w, "stop", d.pageOf(session).Stop)
	}))
	mux.HandleFunc("GET /reviews/{review}/page/events", d.onReview(d.serveEvents))
	mux.HandleFunc("GET /reviews/{review}/page/cursor", d.onReview(func(session *review.Session, w http.ResponseWriter, _ *http.Request) {
		var cursor *CursorWire
		_ = d.locked(func() error { cursor = d.cursorOf(session); return nil })
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(cursor)
	}))
	mux.HandleFunc("POST /reviews/{review}/cursor", d.onReview(d.holdCursor))
}

// holdCursor keeps where the TUI says its cursor is, and tells the review's
// pages it moved.
func (d *Daemon) holdCursor(session *review.Session, w http.ResponseWriter, r *http.Request) {
	var cursor CursorWire
	if err := json.NewDecoder(r.Body).Decode(&cursor); err != nil {
		http.Error(w, "the cursor must be JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	_ = d.locked(func() error {
		if d.cursors == nil {
			d.cursors = map[string]CursorWire{}
		}
		d.cursors[session.ReviewID()] = cursor
		return nil
	})
	d.pages.publish(session.ReviewID(), pageCursor)
	fmt.Fprintln(w, "ok")
}

// cursorOf is where the TUI last said its cursor is on a review, or nil before
// it has said. The caller holds the lock.
func (d *Daemon) cursorOf(session *review.Session) *CursorWire {
	held, ok := d.cursors[session.ReviewID()]
	if !ok {
		return nil
	}
	return &held
}

// pageOf reads what a review's page draws, under the lock, from the view the
// TUI is sent. It does not mark the review opened: the page only draws.
func (d *Daemon) pageOf(session *review.Session) pageData {
	var view ViewWire
	label := ""
	var cursor *CursorWire
	_ = d.locked(func() error {
		view = toViewWire(session.View())
		label = session.Label()
		cursor = d.cursorOf(session)
		return nil
	})
	return newPageData(label, view, cursor)
}

// servePage renders one of the page's templates. It renders into a buffer
// first so a failure is an error page rather than half a page.
func (d *Daemon) servePage(w http.ResponseWriter, name string, data any) {
	var out bytes.Buffer
	if err := pageTemplates.ExecuteTemplate(&out, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = out.WriteTo(w)
}

// serveEvents is a review's change stream: an event each time what its page
// draws may have changed, for as long as the page stays open or the daemon
// keeps serving.
func (d *Daemon) serveEvents(session *review.Session, w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}
	// Subscribed before anything is written, so an event published once the
	// page has its answer is never missed.
	events, unsubscribe := d.pages.subscribe(session.ReviewID())
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, ": following\n\n")
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-d.quit:
			return
		case event := <-events:
			// An event with no data is never dispatched, so it carries its name.
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, event)
			flusher.Flush()
		}
	}
}

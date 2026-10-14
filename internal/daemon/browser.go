package daemon

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// The Browser Renderer: a page per Review, drawn on the daemon's own port, that
// shows the Step the Reviewer is on in the TUI and follows them as they move.
// It only draws. Nothing on it changes the review, so every route it reads is a
// GET, and the page is rendered here from the same view the TUI draws
// (ViewWire) rather than from a JSON API of its own (#139).
//
// Routes, beside the Reviewer's endpoints:
//   - /reviews/{review}/page         the whole page
//   - /reviews/{review}/page/stop    the stop the Reviewer is at, which the
//     page's script swaps in
//   - /reviews/{review}/page/events  the review's change stream (server-sent
//     events)
//   - /reviews/{review}/page/image/{file}?repository={root}  an image a
//     Document refers to (browserimage.go)
//
// and two the TUI posts to, neither changing anything of the review, which
// tell dbn what it does not hold itself (pagePlaces):
//   - /reviews/{review}/screen, its report of a screen dbn holds no stop
//     for, which the page then names;
//   - /reviews/{review}/cursor, its report of where its cursor is each time
//     it moves, which the page brightens (#187).

// PagePath is where a review's page is served, under the daemon's base URL. The
// TUI opens and shows it; the daemon serves it.
func PagePath(reviewID string) string { return "/reviews/" + reviewID + "/page" }

// pageEvent is an event on a review's change stream: its name, and the data
// the page reads from it.
type pageEvent struct {
	name, data string
}

// redrawEvent tells a page to fetch its stop again: the Reviewer moved, or
// something the stop draws changed. It carries nothing, since the page always
// reads what is current; an event with no data is never dispatched, so its
// data is its name.
var redrawEvent = pageEvent{name: "redraw", data: "redraw"}

// cursorEvent tells a page the TUI's cursor moved, and where to: the cursor as
// the TUI reported it, and the place on the page it is at (CursorWire.place),
// which the page's script brightens without fetching anything.
func cursorEvent(cursor CursorWire) pageEvent {
	data, _ := json.Marshal(struct {
		CursorWire
		Place string `json:"place"`
	}{cursor, cursor.place()})
	return pageEvent{name: "cursor", data: string(data)}
}

// pageFeed is every open page's subscription to its review's change stream.
type pageFeed struct {
	mu   sync.Mutex
	subs map[string]map[chan pageEvent]struct{}
}

// subscribe opens a subscription to a review's events, and returns the
// function that closes it.
func (f *pageFeed) subscribe(reviewID string) (<-chan pageEvent, func()) {
	events := make(chan pageEvent, 8)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subs == nil {
		f.subs = map[string]map[chan pageEvent]struct{}{}
	}
	if f.subs[reviewID] == nil {
		f.subs[reviewID] = map[chan pageEvent]struct{}{}
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
// page that has fallen behind drops it, and draws what is current, cursor
// included, on the next redraw.
func (f *pageFeed) publish(reviewID string, event pageEvent) {
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
func (d *Daemon) changed(reviewID string) { d.pages.publish(reviewID, redrawEvent) }

// mutate makes a Reviewer's change to a review under the lock, and answers it:
// a conflict for a change the review refuses, or else "ok", once the review's
// pages are told. Every Reviewer endpoint that changes what a page draws goes
// through here, so none can forget to tell them.
func (d *Daemon) mutate(session *review.Session, w http.ResponseWriter, change func() error) {
	if err := d.locked(change); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	d.changed(session.ReviewID())
	fmt.Fprintln(w, "ok")
}

// Screen is a screen a TUI reports showing for a review, since dbn holds no
// stop for some of what the Reviewer can be looking at: the conclusion screen
// leaves dbn on the last Step, and the Inbox is the window's, not the review's.
// On any other screen (ScreenReview) the page draws the stop dbn holds.
type Screen string

const (
	ScreenReview     Screen = "review"
	ScreenConclusion Screen = "conclusion"
	ScreenInbox      Screen = "inbox"
)

// ScreenWire is the TUI's report of which screen it shows for a review.
type ScreenWire struct {
	Screen Screen `json:"screen"`
}

func (s *ScreenWire) check() error {
	switch s.Screen {
	case ScreenReview, ScreenConclusion, ScreenInbox:
		return nil
	}
	return fmt.Errorf("no screen %q", s.Screen)
}

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

func (c *CursorWire) check() error {
	switch {
	case c.Step < 1:
		return errors.New("a cursor is on a Step, numbered from 1")
	case c.Excerpt < -1:
		return fmt.Errorf("no Excerpt %d", c.Excerpt)
	case c.Excerpt == -1 || c.Block > 0:
		return nil
	case c.Line < 1:
		return errors.New("a cursor in an Excerpt is on a block or a line")
	}
	switch review.Side(c.Side) {
	case review.OldSide, review.NewSide, review.PreviousSide:
		return nil
	}
	return fmt.Errorf("no side %q", c.Side)
}

// place is where on the page the cursor is (placeOfBlock, placeOfLine), or ""
// for anywhere the page does not draw.
func (c CursorWire) place() string {
	switch {
	case c.Excerpt < 0:
		return ""
	case c.Block > 0:
		return placeOfBlock(c.Step, c.Excerpt, c.Block)
	}
	return placeOfLine(c.Step, c.Excerpt, c.Side, c.Line)
}

// placeOfBlock and placeOfLine name the places on a Step's page the TUI's
// cursor can be at: a block of one of its Excerpts, or a line of one. Each
// element the page draws carries the places it is at (data-place), so the
// daemon, marking the cursor as it draws a stop, and the page's script, marking
// it on a cursor event, find the same elements by name alone. The Step is in
// the name, so a cursor on another Step is at nothing drawn.
func placeOfBlock(step, excerpt, block int) string {
	return fmt.Sprintf("%d.%d.b%d", step, excerpt, block)
}

func placeOfLine(step, excerpt int, side string, line int) string {
	return fmt.Sprintf("%d.%d.%s%d", step, excerpt, side, line)
}

// pagePlaces holds what the TUI reports of where the Reviewer is that dbn does
// not hold itself, for each review: the screen it shows, and where its cursor
// is. A review never reported is on ScreenReview with no cursor.
type pagePlaces struct {
	mu sync.Mutex
	of map[string]pagePlace
}

type pagePlace struct {
	screen Screen
	cursor *CursorWire
}

func (p *pagePlaces) get(reviewID string) pagePlace {
	p.mu.Lock()
	defer p.mu.Unlock()
	if place, ok := p.of[reviewID]; ok {
		return place
	}
	return pagePlace{screen: ScreenReview}
}

// update changes what is held for a review.
func (p *pagePlaces) update(reviewID string, change func(*pagePlace)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.of == nil {
		p.of = map[string]pagePlace{}
	}
	place, ok := p.of[reviewID]
	if !ok {
		place.screen = ScreenReview
	}
	change(&place)
	p.of[reviewID] = place
}

// forget lets go of what is held for a review dbn has let go of.
func (p *pagePlaces) forget(reviewID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.of, reviewID)
}

// readReport reads a TUI's report, answering one that cannot be read or makes
// no sense as every report is answered: 400, saying what was wrong. It reports
// whether the report is good.
func readReport(w http.ResponseWriter, r *http.Request, what string, report interface{ check() error }) bool {
	err := json.NewDecoder(r.Body).Decode(report)
	if err == nil {
		err = report.check()
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("bad %s report: %v", what, err), http.StatusBadRequest)
		return false
	}
	return true
}

// reportScreen takes a TUI's report of which screen it shows for a review, and
// redraws the review's pages.
func (d *Daemon) reportScreen(session *review.Session, w http.ResponseWriter, r *http.Request) {
	var report ScreenWire
	if !readReport(w, r, "screen", &report) {
		return
	}
	d.places.update(session.ReviewID(), func(place *pagePlace) { place.screen = report.Screen })
	d.changed(session.ReviewID())
	fmt.Fprintln(w, "ok")
}

// reportCursor takes a TUI's report of where its cursor is on a review, and
// tells the review's pages where it moved to.
func (d *Daemon) reportCursor(session *review.Session, w http.ResponseWriter, r *http.Request) {
	var report CursorWire
	if !readReport(w, r, "cursor", &report) {
		return
	}
	d.places.update(session.ReviewID(), func(place *pagePlace) { place.cursor = &report })
	d.pages.publish(session.ReviewID(), cursorEvent(report))
	fmt.Fprintln(w, "ok")
}

// browserRoutes adds the Browser Renderer's routes to the daemon's mux.
func (d *Daemon) browserRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /reviews/{review}/page", d.onReview(func(session *review.Session, w http.ResponseWriter, _ *http.Request) {
		d.servePage(w, "page", d.pageOf(session))
	}))
	mux.HandleFunc("GET /reviews/{review}/page/stop", d.onReview(func(session *review.Session, w http.ResponseWriter, _ *http.Request) {
		d.servePage(w, "stop", d.pageOf(session).Stop)
	}))
	mux.HandleFunc("GET /reviews/{review}/page/events", d.onReview(d.serveEvents))
	mux.HandleFunc("GET /reviews/{review}/page/image/{file...}", d.onReview(d.serveImage))
	mux.HandleFunc("POST /reviews/{review}/screen", d.onReview(d.reportScreen))
	mux.HandleFunc("POST /reviews/{review}/cursor", d.onReview(d.reportCursor))
	d.diagramRoutes(mux)
}

// pageOf reads what a review's page draws, under the lock, from the view the
// TUI is sent. It does not mark the review opened: the page only draws.
func (d *Daemon) pageOf(session *review.Session) pageData {
	var view ViewWire
	label := ""
	var images documentImages
	_ = d.locked(func() error {
		view = toViewWire(session.View())
		label = session.Label()
		images = imagesOf(session)
		return nil
	})
	place := d.places.get(session.ReviewID())
	return newPageData(label, view, pageContext{images: images, screen: place.screen, cursor: place.cursor})
}

// pagePolicy is the Content-Security-Policy the page is served with: nothing
// runs or is styled but the page's own script and style, inlined and allowed
// by their hashes; images, the stop and the change stream come from dbn only;
// and nothing else is loaded, framed or posted anywhere.
//
// Two allowances are for Mermaid diagrams (browserdiagram.go): the page frames
// dbn's own diagrams frame, which draws them, and shows each drawing as an
// image it makes from what the frame hands back (a blob: URL). Neither lets
// the page run a script or apply a style of anything but its own.
var pagePolicy = "default-src 'none'; " +
	"script-src " + inlineHash(string(pageScript)) + "; " +
	"style-src " + inlineHash(string(pageStyle)) + "; " +
	"img-src 'self' blob:; connect-src 'self'; frame-src 'self'; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// inlineHash is how a Content-Security-Policy names an inline script or style
// it allows.
func inlineHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
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
	w.Header().Set("Content-Security-Policy", pagePolicy)
	w.Header().Set("X-Content-Type-Options", "nosniff")
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
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.name, event.data)
			flusher.Flush()
		}
	}
}

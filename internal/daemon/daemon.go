package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/probertson/diff-by-numbers/internal/buildinfo"
	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
	"github.com/probertson/diff-by-numbers/internal/workingtree"
)

// DefaultPort is where dbn listens unless told otherwise. It is fixed rather
// than negotiated so a single MCP registration works for every session.
const DefaultPort = 7373

// Daemon owns the review Session and serves it over MCP. It holds the lock
// because it is the part that is concurrent; the core stays free of it.
type Daemon struct {
	mu sync.Mutex
	// reviews are the Reviews the daemon is holding, keyed by review id: a
	// Session each, since a Session holds one Review for its whole life. Several
	// agent sessions posting at once is the workflow dbn is for (ADR-0015).
	reviews map[string]*review.Session
	// order is the ids in the order they were posted, so the Inbox lists the
	// oldest first however Go happens to walk the map.
	order []string

	// selfExit is set when the daemon was auto-started (by the stdio shim) rather
	// than run by hand. An auto-started daemon lets go of itself once nothing
	// needs it; a hand-run one (a login-item daemon) never does.
	selfExit bool
	// lastActive is the UnixNano of the most recent request of any kind — an agent
	// call, the shim's keepalive, or a TUI poll — the clock the idle grace is
	// measured against. While anyone is here, something keeps it fresh.
	lastActive atomic.Int64
	// quit is closed once, by Shutdown, to stop serving. It belongs to the Daemon
	// rather than to Serve so a request handler — /shutdown, which `dbn update`
	// calls — can take the same way out as a signal.
	quit     chan struct{}
	quitOnce sync.Once
	// port is where this daemon listens, so an agent can be told how the
	// Reviewer reaches it. It is the default until Serve says otherwise.
	port int

	// waiting is what the daemon knows of the `dbn wait`s on each review, keyed
	// by review id. waitHold is how long a poll is held; waiterGrace how long a
	// waiter counts as listening after its poll closes.
	waiting     map[string]*waiters
	waitHold    time.Duration
	waiterGrace time.Duration
}

// Option configures a Daemon at construction.
type Option func(*Daemon)

// WithSelfExit makes the daemon exit on its own once no review needs it and no
// client is attached. It is set only for an auto-started daemon.
func WithSelfExit() Option {
	return func(d *Daemon) { d.selfExit = true }
}

func New(opts ...Option) *Daemon {
	d := &Daemon{
		reviews:     map[string]*review.Session{},
		quit:        make(chan struct{}),
		port:        DefaultPort,
		waiting:     map[string]*waiters{},
		waitHold:    defaultWaitHold,
		waiterGrace: defaultWaiterGrace,
	}
	d.touch()
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// newSession is an empty Session, ready for a Review's first Round.
func newSession() *review.Session {
	return review.NewSession(workingtree.NewResolver(), git.NewDeriver())
}

// review returns the Session holding the review named by id. Every call about a
// review names it, so an id dbn is not holding is the caller's to hear about.
func (d *Daemon) review(id string) (*review.Session, bool) {
	session, ok := d.reviews[id]
	return session, ok
}

// hold puts a newly opened review among those the daemon is serving, keeping the
// order it was posted in.
func (d *Daemon) hold(session *review.Session) {
	d.reviews[session.ReviewID()] = session
	d.order = append(d.order, session.ReviewID())
}

// release lets go of a review the Authoring Agent is done with: its results have
// been fetched, or it said so itself with conclude. Until then a concluded review
// is still held, so a late fetch is answered rather than read as a wrong id.
func (d *Daemon) release(id string) {
	delete(d.reviews, id)
	delete(d.waiting, id)
	for i, held := range d.order {
		if held == id {
			d.order = append(d.order[:i], d.order[i+1:]...)
			break
		}
	}
}

// held walks the reviews in the order they were posted.
func (d *Daemon) held() []*review.Session {
	sessions := make([]*review.Session, 0, len(d.order))
	for _, id := range d.order {
		if session, ok := d.reviews[id]; ok {
			sessions = append(sessions, session)
		}
	}
	return sessions
}

// touch records that the daemon just saw activity, resetting the idle clock.
func (d *Daemon) touch() { d.lastActive.Store(time.Now().UnixNano()) }

// idleFor reports how long it has been since the last request of any kind.
func (d *Daemon) idleFor() time.Duration {
	return time.Since(time.Unix(0, d.lastActive.Load()))
}

// Serve listens on the loopback interface only. A review surface has no reason
// to be reachable from the network. It runs until q is pressed (when a terminal
// is attached) or an interrupt/terminate signal arrives, then shuts down cleanly.
func (d *Daemon) Serve(port int) error {
	d.port = port
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("dbn could not listen on port %d: %w", port, err)
	}

	server := &http.Server{Handler: d.Handler()}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		d.Shutdown()
	}()

	// An auto-started daemon watches for the moment nothing needs it any more and
	// quits itself. Start the idle clock fresh here so the grace is measured from
	// when serving began, not from construction.
	if d.selfExit {
		d.touch()
		go d.monitorForExit()
	}

	// With a terminal attached, offer a single-key q. Raw mode disables the
	// kernel's Ctrl-C, so watch for it explicitly. Headless (launchd, a pipe),
	// there is no keyboard and the signal handler is the only way out.
	fmt.Printf("dbn listening on http://127.0.0.1:%d (MCP at /mcp)\n", port)
	if term.IsTerminal(os.Stdin.Fd()) {
		// Bold the key so the quit hint stands out on its own line. Safe here:
		// this branch only runs with a terminal attached.
		const bold, reset = "\033[1m", "\033[0m"
		fmt.Printf("press %sq%s to quit\n", bold, reset)
		if restore, err := watchForQuitKey(d.Shutdown); err == nil {
			defer restore()
		}
	}

	serveErr := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	select {
	case err := <-serveErr:
		return err
	case <-d.quit:
	}

	fmt.Print("\rdbn shutting down\n")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// A live agent session holds a server→client stream open, which cannot drain
	// inside the grace window; the resulting DeadlineExceeded is the expected shape
	// of a clean shutdown, not a failure worth printing.
	if err := server.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

// Shutdown stops a serving daemon, the same way an interrupt does: the listener
// closes and Serve returns once in-flight requests have had their grace. It is
// safe to call more than once, and harmless before Serve — the next Serve on
// this Daemon would simply return at once, which no caller does.
func (d *Daemon) Shutdown() { d.quitOnce.Do(func() { close(d.quit) }) }

// maxCheckInterval caps how often the self-exit decision is polled.
const maxCheckInterval = 5 * time.Second

// exitGraceDuration is how long the daemon stays idle before letting go. It is
// measured from the last activity — a request, or a client stream closing — so it
// also covers a brief reconnect gap. Overridable with DBN_EXIT_GRACE, chiefly so
// tests need not wait a real minute.
func exitGraceDuration() time.Duration {
	if raw := os.Getenv("DBN_EXIT_GRACE"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
	}
	return 60 * time.Second
}

// shouldExit is the whole of the self-exit decision, kept pure. The daemon stays
// alive while a review is still live, and otherwise until it has been idle
// through the grace window — where "not idle" means an agent call, a TUI poll, or
// the shim keepalive touched it recently, i.e. someone is still here.
//
// A review that has ended — concluded or dismissed — does not hold the daemon
// open. Its agent has nothing to collect but the fact that it is over, and the
// Reviewer who ended it is the one who tells them so.
func shouldExit(activeReview bool, idle, grace time.Duration) bool {
	if activeReview {
		return false
	}
	return idle >= grace
}

// monitorForExit polls the self-exit decision and signals a quit the first time
// it is satisfied. The poll interval tracks the grace so a short (test) grace is
// noticed promptly and a long (real) one is not polled needlessly often.
func (d *Daemon) monitorForExit() {
	grace := exitGraceDuration()
	interval := grace / 4
	if interval < 50*time.Millisecond {
		interval = 50 * time.Millisecond
	}
	if interval > maxCheckInterval {
		interval = maxCheckInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-d.quit:
			return
		case <-ticker.C:
			if shouldExit(d.activeReview(), d.idleFor(), grace) {
				d.Shutdown()
				return
			}
		}
	}
}

// watchForQuitKey puts the terminal in raw mode and quits when q (or Ctrl-C,
// which raw mode would otherwise swallow) is pressed. It returns a function that
// restores the terminal, to be deferred by the caller.
func watchForQuitKey(onQuit func()) (func(), error) {
	fd := os.Stdin.Fd()
	state, err := term.MakeRaw(fd)
	if err != nil {
		return func() {}, err
	}
	restore := func() { _ = term.Restore(fd, state) }
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				return
			}
			if n == 0 {
				continue
			}
			switch buf[0] {
			case 'q', 'Q', 0x03: // q or Ctrl-C
				onQuit()
				return
			}
		}
	}()
	return restore, nil
}

// anchorRequest is a Reviewer's selection as it arrives from the TUI: the Excerpt
// it lies in and the row at each end of it. The rows between them are the core's
// to derive, so the client never states a range (#57). AcknowledgementIndex is
// present only for a selection in an expanded Acknowledgement, whose Excerpts
// ExcerptIndex then counts.
type anchorRequest struct {
	AcknowledgementIndex *int         `json:"acknowledgement_index,omitempty"`
	ExcerptIndex         int          `json:"excerpt_index"`
	Start                endpointWire `json:"start"`
	End                  endpointWire `json:"end"`
}

type endpointWire struct {
	Side string `json:"side"`
	Line int    `json:"line"`
}

func (r anchorRequest) target() review.AnchorTarget {
	return review.AnchorTarget{
		Acknowledgement: r.AcknowledgementIndex,
		ExcerptIndex:    r.ExcerptIndex,
		Start:           review.AnchorEndpoint{Side: review.Side(r.Start.Side), Line: r.Start.Line},
		End:             review.AnchorEndpoint{Side: review.Side(r.End.Side), Line: r.End.Line},
	}
}

// Handler is the daemon's HTTP surface: MCP at /mcp, plus the Reviewer's own
// endpoints. Exported so it can be exercised over a real connection.
func (d *Daemon) Handler() http.Handler {
	mux := http.NewServeMux()
	// The SDK applies no cross-origin protection when this is nil, and dbn is
	// about to be a surface that renders source code.
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return d.mcpServer() },
		&mcp.StreamableHTTPOptions{CrossOriginProtection: &http.CrossOriginProtection{}}))

	// /ping is the shim's keepalive: while an agent session is alive its shim
	// pings this, which (through withActivity) keeps the idle clock fresh so an
	// auto-started daemon does not exit out from under a connected-but-idle
	// session. It carries no state.
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	// /status is how anything outside the daemon learns what it is: which build it
	// runs, which binary that build came from, and whether interrupting it would
	// cost someone a review in progress. `dbn update` asks all three; the TUI asks
	// the version so it can warn when it no longer matches its own.
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		executable, err := os.Executable()
		if err != nil {
			// Not worth failing the request over: a caller that cannot learn the path
			// falls back to saying nothing about it, which is what an empty string says.
			executable = ""
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(StatusWire{
			Version:      buildinfo.Version(),
			Executable:   executable,
			ActiveReview: d.activeReview(),
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// /shutdown is the graceful stop `dbn update` uses so a replaced binary is the
	// one the next daemon runs. It is the same path as SIGTERM: under launchd or
	// systemd the manager starts a fresh daemon, and a shim-started one comes back
	// the next time an agent needs it. Loopback-only, like every endpoint here.
	mux.HandleFunc("POST /shutdown", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "shutting down")
		// Answer first, then quit: the caller wants to know the daemon accepted,
		// and Shutdown closes the listener out from under this response otherwise.
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		d.Shutdown()
	})

	// Every review the daemon holds, since dump is a Reviewer's whole-daemon
	// look at what is going on rather than a read of one review.
	mux.HandleFunc("GET /dump", func(w http.ResponseWriter, _ *http.Request) {
		var out strings.Builder
		d.mu.Lock()
		for _, session := range d.held() {
			fmt.Fprintf(&out, "Review %s", session.ReviewID())
			if label := session.Label(); label != "" {
				fmt.Fprintf(&out, " (%s)", label)
			}
			fmt.Fprintf(&out, "\n\n%s\n", session.Dump())
		}
		d.mu.Unlock()
		if out.Len() == 0 {
			fmt.Fprint(w, "no Round is posted\n")
			return
		}
		fmt.Fprint(w, out.String())
	})

	// One review, for a Reviewer who knows which they want.
	mux.HandleFunc("GET /reviews/{review}/dump", d.onReview(func(session *review.Session, w http.ResponseWriter, _ *http.Request) {
		var dump string
		_ = d.locked(func() error {
			dump = session.Dump()
			return nil
		})
		fmt.Fprint(w, dump)
	}))

	mux.HandleFunc("GET /inbox", func(w http.ResponseWriter, _ *http.Request) {
		d.mu.Lock()
		rows := d.inbox()
		d.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(InboxWire{Reviews: rows}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("POST /reviews/{review}/comment", d.onReview(func(session *review.Session, w http.ResponseWriter, r *http.Request) {
		var req struct {
			anchorRequest
			Note string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad comment", http.StatusBadRequest)
			return
		}
		err := d.locked(func() error {
			_, err := session.RaiseComment(req.target(), req.Note)
			return err
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		fmt.Fprintln(w, "ok")
	}))

	mux.HandleFunc("PUT /reviews/{review}/comment/{id}", d.onReview(func(session *review.Session, w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "id must be a number", http.StatusBadRequest)
			return
		}
		var req struct {
			Note string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad edit", http.StatusBadRequest)
			return
		}
		if err := d.locked(func() error { return session.EditComment(id, req.Note) }); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		fmt.Fprintln(w, "ok")
	}))

	mux.HandleFunc("DELETE /reviews/{review}/comment/{id}", d.onReview(func(session *review.Session, w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "id must be a number", http.StatusBadRequest)
			return
		}
		if err := d.locked(func() error { return session.WithdrawComment(id) }); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		fmt.Fprintln(w, "ok")
	}))

	// A Hand Off is one of the events a `dbn wait` is waiting for, so it wakes
	// every poll open on the review. Whether the agent is told is then a fact
	// about this Hand Off, not an earlier one the Reviewer since took back.
	mux.HandleFunc("POST /reviews/{review}/finish", d.navHandler(func(session *review.Session) error {
		if err := session.Finish(); err != nil {
			return err
		}
		d.waitersOf(session.ReviewID()).told = false
		d.wake(session.ReviewID())
		return nil
	}))
	mux.HandleFunc("POST /reviews/{review}/reopen", d.navHandler((*review.Session).Reopen))

	mux.HandleFunc("POST /reviews/{review}/reraise/{id}", d.onReview(func(session *review.Session, w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "id must be a number", http.StatusBadRequest)
			return
		}
		// The note is optional: an empty body re-raises the Comment as it stood,
		// which is what a Reviewer who simply disagrees wants.
		var req struct {
			Note string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "bad re-raise", http.StatusBadRequest)
			return
		}
		err = d.locked(func() error {
			_, err := session.ReRaise(id, req.Note)
			return err
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		fmt.Fprintln(w, "ok")
	}))

	mux.HandleFunc("POST /reviews/{review}/anchor", d.onReview(func(session *review.Session, w http.ResponseWriter, r *http.Request) {
		var req anchorRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad anchor request", http.StatusBadRequest)
			return
		}
		var anchor review.Anchor
		err := d.locked(func() error {
			var err error
			anchor, err = session.Anchor(req.target())
			return err
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, anchor.Render())
	}))

	mux.HandleFunc("GET /reviews/{review}/view", d.onReview(func(session *review.Session, w http.ResponseWriter, r *http.Request) {
		var view ViewWire
		_ = d.locked(func() error {
			// Reading a review is what opening it means, and the Inbox says which
			// reviews the Reviewer has yet to look at.
			session.MarkOpened()
			view = toViewWire(session.View())
			view.AgentTold = view.Finished && d.agentTold(session.ReviewID())
			return nil
		})
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(view); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))

	mux.HandleFunc("GET /reviews/{review}/expand/{step}/{ack}", d.onReview(func(session *review.Session, w http.ResponseWriter, r *http.Request) {
		step, err := strconv.Atoi(r.PathValue("step"))
		if err != nil {
			http.Error(w, "step must be a number", http.StatusBadRequest)
			return
		}
		ack, err := strconv.Atoi(r.PathValue("ack"))
		if err != nil {
			http.Error(w, "ack must be a number", http.StatusBadRequest)
			return
		}
		var views []review.ExcerptView
		err = d.locked(func() error {
			var err error
			views, err = session.ExpandAcknowledgement(step, ack)
			return err
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(toExcerptWires(views)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))

	// The long poll `dbn wait` sits on. It answers for an id dbn is not holding
	// rather than refusing it, because that answer ends the wait (ADR-0016).
	mux.HandleFunc("GET /reviews/{review}/wait", d.serveWait)

	mux.HandleFunc("POST /reviews/{review}/advance", d.navHandler((*review.Session).Advance))
	mux.HandleFunc("POST /reviews/{review}/since-previous", d.navHandler((*review.Session).ToggleSincePreviousRound))
	mux.HandleFunc("POST /reviews/{review}/back", d.navHandler((*review.Session).Back))
	mux.HandleFunc("POST /reviews/{review}/goto/{n}", d.onReview(func(session *review.Session, w http.ResponseWriter, r *http.Request) {
		n, err := strconv.Atoi(r.PathValue("n"))
		if err != nil {
			http.Error(w, "position must be a number", http.StatusBadRequest)
			return
		}
		if err := d.locked(func() error { return session.GoTo(n) }); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		fmt.Fprintln(w, "ok")
	}))

	// Dismissal is the Reviewer's act, so it is theirs to reach and not exposed
	// as an MCP tool: an agent cannot dismiss a review of its own work. The
	// review stays held as its own tombstone until the agent has been told.
	mux.HandleFunc("POST /reviews/{review}/dismiss", d.onReview(func(session *review.Session, w http.ResponseWriter, r *http.Request) {
		err := d.locked(func() error {
			if err := session.Dismiss(); err != nil {
				return err
			}
			d.wake(session.ReviewID())
			return nil
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		fmt.Fprintln(w, "Review dismissed")
	}))
	return d.withActivity(mux)
}

// activeReview reports whether a review is still live: one the Reviewer is in
// the middle of. It is what `dbn update` asks before restarting the daemon.
func (d *Daemon) activeReview() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, session := range d.reviews {
		if session.Active() {
			return true
		}
	}
	return false
}

// inbox lists the reviews the Reviewer can still pick from: everything the
// daemon holds that is not concluded, with what needs them first and each group
// oldest first. It answers under the caller's lock.
//
// The order is dbn's to decide rather than each window's, so two windows agree
// and a window need know nothing about what the states mean.
func (d *Daemon) inbox() []InboxRowWire {
	rows := make([]InboxRowWire, 0, len(d.order))
	var waiting []InboxRowWire
	for _, session := range d.held() {
		open, ok := session.Open()
		if !ok {
			continue
		}
		row := InboxRowWire{
			ID:           open.ID,
			Label:        open.Label,
			State:        inboxState(open),
			Repositories: inboxRepositories(open.Repositories),
			Round:        open.Round,
			Position:     open.Position,
			StepCount:    open.StepCount,
			CommentCount: open.CommentCount,
			PostedAt:     open.Posted,
		}
		// A review handed off is the Authoring Agent's turn: the Reviewer can do
		// nothing with it, so it goes below the ones they can.
		if row.State == StateWaitingOnAgent {
			waiting = append(waiting, row)
			continue
		}
		rows = append(rows, row)
	}
	// Oldest first within each group, measured from the round on screen — the
	// same moment the row's age counts from, so the order and the age agree. A
	// review whose agent has just posted again is the freshest thing there, and
	// the one nobody has touched for an hour is the one going stale.
	byAge(rows)
	byAge(waiting)
	return append(rows, waiting...)
}

// byAge puts the least recently posted first.
func byAge(rows []InboxRowWire) {
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].PostedAt.Before(rows[j].PostedAt) })
}

// inboxRepositories puts the repositories under review on the wire.
func inboxRepositories(repositories []review.OpenRepository) []InboxRepositoryWire {
	wires := make([]InboxRepositoryWire, 0, len(repositories))
	for _, repository := range repositories {
		wires = append(wires, InboxRepositoryWire{Name: repository.Name, Branch: repository.Branch})
	}
	return wires
}

// inboxState says whose turn a review is, which is what the Reviewer picks by.
func inboxState(open review.OpenReview) string {
	switch {
	case open.HandedOff:
		return StateWaitingOnAgent
	case open.Opened:
		return StateNeedsReviewer
	}
	return StateNew
}

// withActivity resets the idle clock on every request, so a Reviewer's TUI poll,
// the shim's keepalive, and an agent's call all count equally as the daemon being
// needed. This one signal — plus whether a review is still active — is the whole
// of what keeps an auto-started daemon alive.
func (d *Daemon) withActivity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.touch()
		next.ServeHTTP(w, r)
	})
}

// navHandler adapts a no-argument navigation intent to an HTTP handler under the
// daemon lock, so navigation stays serialized against posts and fetches.
func (d *Daemon) navHandler(intent func(*review.Session) error) http.HandlerFunc {
	return d.onReview(func(session *review.Session, w http.ResponseWriter, _ *http.Request) {
		if err := d.locked(func() error { return intent(session) }); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		fmt.Fprintln(w, "ok")
	})
}

// onReview runs act on the review a Reviewer's request names, holding the lock
// for it and no longer. A request naming a review dbn is not holding — one
// released since the window last looked, usually — is answered 404, which is how
// a window finds out its review has gone and returns to the Inbox.
//
// The answer is written after the lock is dropped: a Reviewer's terminal reading
// slowly must not hold up the agent's next call.
func (d *Daemon) onReview(act func(*review.Session, http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("review")
		d.mu.Lock()
		session, ok := d.review(id)
		d.mu.Unlock()
		if !ok {
			http.Error(w, fmt.Sprintf("no review with id %q", id), http.StatusNotFound)
			return
		}
		act(session, w, r)
	}
}

// locked runs act on a Session under the daemon's lock, which is what every
// call into the core needs: the core is free of locking on purpose.
func (d *Daemon) locked(act func() error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return act()
}

func (d *Daemon) mcpServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "dbn",
		Version: buildinfo.Version(),
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name: "post_round",
		Description: "Post a Round of your changes for the Reviewer to work through: a Brief and ordered Steps. " +
			"Send it once and completely: the Reviewer navigates it without involving you. " +
			"Order Steps so each is comprehensible given only the Steps before it, and send " +
			"line ranges rather than code — dbn reads the working tree itself. " +
			"A new review needs a label, and returns a review id: record it, and name it in every " +
			"later call about this review. Post a Revision Round with revises set to that id, once " +
			"the Reviewer has handed off; to change the round under review in place, post with " +
			"replaces set to it instead.",
	}, d.postRound)

	mcp.AddTool(server, &mcp.Tool{
		Name: "describe_changes",
		Description: "Describe the changes under review as dbn derives them, without posting: per " +
			"file, its status and the Changed Line ranges a Round must cover, and the edits " +
			"whose removed lines ride along with their replacement. It is worked out exactly as " +
			"post_round checks coverage, so plan Excerpts from it rather than from git diff. " +
			"Give review_id when you are planning a Revision Round and it reports what that " +
			"review has already shown; leave it out to plan a new review's first round. It " +
			"changes nothing, so call it as often as you like.",
	}, d.describeChanges)

	mcp.AddTool(server, &mcp.Tool{
		Name: "fetch_results",
		Description: "Ask how the review named by review_id went. Returns immediately whether or " +
			"not the Reviewer has handed off; it never waits. Call it once the Reviewer says they " +
			"are done. If you have lost the id, call it without one: you will be refused, and told " +
			"which reviews dbn is holding, by id and label. " +
			"If it reports the review complete (the Reviewer handed off having raised nothing), " +
			"the loop is over and dbn treats the review as concluded.",
	}, d.fetchResults)

	mcp.AddTool(server, &mcp.Tool{
		Name: "conclude",
		Description: "Conclude a review you are done with, by its id, so dbn can release it. " +
			"Use it when you will post no further Revision Round — for instance the Reviewer " +
			"handed off having raised nothing, or you have decided to stop. A review the Reviewer " +
			"hands off with nothing raised is already treated as concluded; calling this is the " +
			"explicit way to end one otherwise. It does not discard anything.",
	}, d.conclude)

	return server
}

func (d *Daemon) postRound(_ context.Context, _ *mcp.CallToolRequest, in wireRound) (*mcp.CallToolResult, postResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	session, err := d.accept(in)
	if err != nil {
		var rejection *review.Rejection
		if errors.As(err, &rejection) {
			return nil, postResult{Accepted: false, Problems: problemsOf(rejection)}, nil
		}
		return nil, postResult{}, err
	}
	return nil, postResult{
		Accepted:    true,
		ReviewID:    session.ReviewID(),
		Message:     postedMessage(session.LastPost(), d.port),
		WaitCommand: waitCommand(session.ReviewID(), d.port),
	}, nil
}

// accept puts a post where it belongs: a Revision Round or a Replacement into
// the Session holding the review it names, and anything else into a Session of
// its own. A new review's Session joins those held only once its first Round is
// accepted, so a refused post leaves the daemon exactly as it was.
func (d *Daemon) accept(in wireRound) (*review.Session, error) {
	switch {
	case in.Replaces != "":
		session, ok := d.review(in.Replaces)
		if !ok {
			return nil, unknownReview(in.Replaces)
		}
		return session, session.Replace(in.Replaces, in.toDomain())
	case in.Revises != "":
		session, ok := d.review(in.Revises)
		if !ok {
			return nil, unknownReview(in.Revises)
		}
		return session, session.Revise(in.Revises, in.toDomain())
	}
	fresh := newSession()
	if err := fresh.Post(in.toDomain()); err != nil {
		return nil, err
	}
	d.hold(fresh)
	return fresh, nil
}

// unknownReview is what a call naming a review the daemon is not holding gets:
// the same refusal whether the id is wrong or the review has been released.
func unknownReview(id string) error {
	return &review.Rejection{Problems: []review.Problem{{
		Reason: review.RejectedUnknownReview,
		Detail: unknownReviewDetail(id),
	}}}
}

// unknownReviewDetail is what an agent is told about an id dbn is not holding,
// whichever call named it: the same words, and the way to find the right one.
func unknownReviewDetail(id string) string {
	return fmt.Sprintf("no review with id %q; call fetch_results without an id to see which reviews dbn is holding", id)
}

func (d *Daemon) describeChanges(_ context.Context, _ *mcp.CallToolRequest, in describeInput) (*mcp.CallToolResult, describeResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var description review.ChangeDescription
	var err error
	if in.ReviewID == "" {
		// Nothing to scope against: a first round is described from the working
		// tree alone, so a Session of its own is all it takes.
		description, err = newSession().DescribeChanges(toChangeSet(in.Repositories))
	} else if session, held := d.review(in.ReviewID); held {
		description, err = session.DescribeRevision(in.ReviewID, toChangeSet(in.Repositories))
	} else {
		err = unknownReview(in.ReviewID)
	}
	if err != nil {
		var rejection *review.Rejection
		if errors.As(err, &rejection) {
			return nil, describeResult{Problems: problemsOf(rejection)}, nil
		}
		return nil, describeResult{}, err
	}
	return nil, toDescribeResult(description), nil
}

func (d *Daemon) conclude(_ context.Context, _ *mcp.CallToolRequest, in concludeInput) (*mcp.CallToolResult, concludeResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	session, ok := d.review(in.ReviewID)
	if !ok {
		return nil, concludeResult{Message: unknownReview(in.ReviewID).Error()}, nil
	}
	if err := session.Conclude(in.ReviewID); err != nil {
		var rejection *review.Rejection
		if errors.As(err, &rejection) {
			return nil, concludeResult{
				Concluded: false,
				Problems:  problemsOf(rejection),
				Message:   rejection.Error(),
			}, nil
		}
		return nil, concludeResult{}, err
	}
	// Concluding is the agent saying it is done, so nothing is waiting to be
	// fetched: the review can go now.
	d.release(in.ReviewID)
	return nil, concludeResult{Concluded: true, Message: "the review is concluded and released"}, nil
}

func (d *Daemon) fetchResults(_ context.Context, _ *mcp.CallToolRequest, in fetchInput) (*mcp.CallToolResult, fetchResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if in.ReviewID == "" {
		return nil, d.refuseUnnamedFetch(), nil
	}
	session, ok := d.review(in.ReviewID)
	if !ok {
		return nil, fetchResult{
			Message:  unknownReviewDetail(in.ReviewID),
			Problems: []problemWire{{Reason: string(review.RejectedUnknownReview), Detail: unknownReviewDetail(in.ReviewID)}},
		}, nil
	}

	results, err := session.Results()
	if err != nil {
		return nil, fetchResult{}, err
	}
	// The agent has what it came for, so a review that is over can go — and a
	// dismissed one has just served its whole purpose as a tombstone.
	if session.Concluded() || session.Dismissed() {
		defer d.release(in.ReviewID)
	}

	message := "no Round is posted; post one before asking how the review went"
	switch session.Outcome() {
	case review.Dismissed:
		message = "the Reviewer dismissed this review — it is over, and a Revision Round of it will be refused. Anything they raised before dismissing it is below; post new work as a new review"
	case review.HandedOffNothingRaised:
		message = "the Reviewer handed off having raised nothing — the review is complete; there is no Revision Round to post"
	case review.HandedOffWithComments:
		message = "the Reviewer has handed off; respond to each Comment below (make the change, answer the question, or decline), then post a Revision Round"
	default:
		if results.Posted {
			message = "the Reviewer has not handed off the Round yet"
		}
	}

	return nil, toFetchResult(results, message), nil
}

// refuseUnnamedFetch answers a fetch that named no review: it lists what is
// open, which is how an agent that lost its id — to compaction, usually — gets
// it back. Guessing on its behalf is what ADR-0015 rules out.
func (d *Daemon) refuseUnnamedFetch() fetchResult {
	open := d.openReviews()
	message := "no review is open, so there is nothing to ask about"
	if len(open) > 0 {
		message = "fetch_results needs the review_id post_round gave you; the reviews dbn is holding are listed below"
	}
	return fetchResult{
		Message: message,
		Problems: []problemWire{{
			Reason: string(review.RejectedMissingReviewID),
			Detail: "name the review to ask about in review_id",
		}},
		OpenReviews: open,
	}
}

// openReviews lists the reviews the daemon is holding, in the order it holds
// them. It answers under the caller's lock.
func (d *Daemon) openReviews() []openReviewWire {
	reviews := make([]openReviewWire, 0, len(d.order))
	for _, session := range d.held() {
		open, ok := session.Open()
		if !ok {
			continue
		}
		reviews = append(reviews, openReviewWire{ID: open.ID, Label: open.Label, State: inboxState(open)})
	}
	return reviews
}

// problemsOf puts a rejection's problems on the wire in the order dbn found
// them, so an agent reads them in the order it would fix them.
func problemsOf(rejection *review.Rejection) []problemWire {
	problems := make([]problemWire, 0, len(rejection.Problems))
	for _, problem := range rejection.Problems {
		problems = append(problems, problemWire{Reason: string(problem.Reason), Detail: problem.Detail})
	}
	return problems
}

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
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
	mu      sync.Mutex
	session *review.Session

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
		session: review.NewSession(workingtree.NewResolver(), git.NewDeriver()),
		quit:    make(chan struct{}),
	}
	d.touch()
	for _, opt := range opts {
		opt(d)
	}
	return d
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
// alive while a review is still active, and otherwise until it has been idle
// through the grace window — where "not idle" means an agent call, a TUI poll, or
// the shim keepalive touched it recently, i.e. someone is still here.
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

	mux.HandleFunc("GET /dump", func(w http.ResponseWriter, _ *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		fmt.Fprint(w, d.session.Dump())
	})

	mux.HandleFunc("POST /comment", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			anchorRequest
			Note string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad comment", http.StatusBadRequest)
			return
		}
		d.mu.Lock()
		_, err := d.session.RaiseComment(req.target(), req.Note)
		d.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		fmt.Fprintln(w, "ok")
	})

	mux.HandleFunc("PUT /comment/{id}", func(w http.ResponseWriter, r *http.Request) {
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
		d.mu.Lock()
		err = d.session.EditComment(id, req.Note)
		d.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		fmt.Fprintln(w, "ok")
	})

	mux.HandleFunc("DELETE /comment/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "id must be a number", http.StatusBadRequest)
			return
		}
		d.mu.Lock()
		err = d.session.WithdrawComment(id)
		d.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		fmt.Fprintln(w, "ok")
	})

	mux.HandleFunc("POST /finish", d.navHandler(func() error { return d.session.Finish() }))
	mux.HandleFunc("POST /reopen", d.navHandler(func() error { return d.session.Reopen() }))

	mux.HandleFunc("POST /reraise/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "id must be a number", http.StatusBadRequest)
			return
		}
		d.mu.Lock()
		_, err = d.session.ReRaise(id)
		d.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		fmt.Fprintln(w, "ok")
	})

	mux.HandleFunc("POST /anchor", func(w http.ResponseWriter, r *http.Request) {
		var req anchorRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad anchor request", http.StatusBadRequest)
			return
		}
		d.mu.Lock()
		anchor, err := d.session.Anchor(req.target())
		d.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, anchor.Render())
	})

	mux.HandleFunc("GET /view", func(w http.ResponseWriter, _ *http.Request) {
		d.mu.Lock()
		view := toViewWire(d.session.View())
		d.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(view); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("GET /expand/{step}/{ack}", func(w http.ResponseWriter, r *http.Request) {
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
		d.mu.Lock()
		views, err := d.session.ExpandAcknowledgement(step, ack)
		d.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(toExcerptWires(views)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("POST /advance", d.navHandler(func() error { return d.session.Advance() }))
	mux.HandleFunc("POST /back", d.navHandler(func() error { return d.session.Back() }))
	mux.HandleFunc("POST /goto/{n}", func(w http.ResponseWriter, r *http.Request) {
		n, err := strconv.Atoi(r.PathValue("n"))
		if err != nil {
			http.Error(w, "position must be a number", http.StatusBadRequest)
			return
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		if err := d.session.GoTo(n); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		fmt.Fprintln(w, "ok")
	})

	// Abandoning is the Reviewer's act, so it is reachable from the CLI and not
	// exposed as an MCP tool: an agent cannot dismiss a review of its own work.
	mux.HandleFunc("POST /abandon", func(w http.ResponseWriter, _ *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if err := d.session.Abandon(); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		fmt.Fprintln(w, "Walkthrough abandoned")
	})
	return d.withActivity(mux)
}

// activeReview reports whether a review is still live, under the lock.
func (d *Daemon) activeReview() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.session.Active()
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
func (d *Daemon) navHandler(intent func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if err := intent(); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		fmt.Fprintln(w, "ok")
	}
}

func (d *Daemon) mcpServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "dbn",
		Version: buildinfo.Version(),
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name: "post_walkthrough",
		Description: "Post a Walkthrough of your changes for the Reviewer to work through. " +
			"Send it once and completely: the Reviewer navigates it without involving you. " +
			"Order Steps so each is comprehensible given only the Steps before it, and send " +
			"line ranges rather than code — dbn reads the working tree itself. " +
			"It returns a review id; record it, and pass it to conclude when the review is done.",
	}, d.postWalkthrough)

	mcp.AddTool(server, &mcp.Tool{
		Name: "fetch_results",
		Description: "Ask how the review went. Returns immediately whether or not the Reviewer " +
			"has handed off; it never waits. Call it once the Reviewer says they are done. " +
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

func (d *Daemon) postWalkthrough(_ context.Context, _ *mcp.CallToolRequest, in wireWalkthrough) (*mcp.CallToolResult, postResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.session.Post(in.toDomain()); err != nil {
		var rejection *review.Rejection
		if errors.As(err, &rejection) {
			return nil, postResult{
				Accepted: false,
				Reason:   string(rejection.Reason),
				Detail:   rejection.Detail,
			}, nil
		}
		return nil, postResult{}, err
	}
	return nil, postResult{Accepted: true, ReviewID: d.session.ReviewID()}, nil
}

func (d *Daemon) conclude(_ context.Context, _ *mcp.CallToolRequest, in concludeInput) (*mcp.CallToolResult, concludeResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.session.Conclude(in.ReviewID); err != nil {
		var rejection *review.Rejection
		if errors.As(err, &rejection) {
			return nil, concludeResult{Concluded: false, Reason: string(rejection.Reason), Message: rejection.Detail}, nil
		}
		return nil, concludeResult{}, err
	}
	return nil, concludeResult{Concluded: true, Message: "the review is concluded; dbn will release it once nothing else needs it"}, nil
}

func (d *Daemon) fetchResults(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, fetchResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	results, err := d.session.Results()
	if err != nil {
		return nil, fetchResult{}, err
	}

	message := "no Walkthrough is posted; post one before asking how the review went"
	switch {
	case results.Posted && results.Finished && len(results.Comments) == 0:
		message = "the Reviewer handed off having raised nothing — the review is complete; there is no Revision Round to post"
	case results.Posted && results.Finished:
		message = "the Reviewer has handed off; respond to each Comment below (make the change, answer the question, or decline), then post a Revision Round"
	case results.Posted:
		message = "the Reviewer has not handed off the Walkthrough yet"
	}

	return nil, toFetchResult(results, message), nil
}

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
}

func New() *Daemon {
	return &Daemon{session: review.NewSession(workingtree.NewResolver(), git.NewDeriver())}
}

// Serve listens on the loopback interface only. A review surface has no reason
// to be reachable from the network.
func (d *Daemon) Serve(port int) error {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("dbn could not listen on port %d: %w", port, err)
	}
	fmt.Printf("dbn listening on http://127.0.0.1:%d (MCP at /mcp)\n", port)
	return http.Serve(listener, d.Handler())
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

	mux.HandleFunc("GET /dump", func(w http.ResponseWriter, _ *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		fmt.Fprint(w, d.session.Dump())
	})

	mux.HandleFunc("POST /changerequest", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ExcerptIndex int    `json:"excerpt_index"`
			FirstLine    int    `json:"first_line"`
			LastLine     int    `json:"last_line"`
			Note         string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad change-request", http.StatusBadRequest)
			return
		}
		d.mu.Lock()
		_, err := d.session.RaiseChangeRequest(review.AnchorTarget{
			ExcerptIndex: req.ExcerptIndex, FirstLine: req.FirstLine, LastLine: req.LastLine,
		}, req.Note)
		d.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		fmt.Fprintln(w, "ok")
	})

	mux.HandleFunc("DELETE /changerequest/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "id must be a number", http.StatusBadRequest)
			return
		}
		d.mu.Lock()
		err = d.session.WithdrawChangeRequest(id)
		d.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		fmt.Fprintln(w, "ok")
	})

	mux.HandleFunc("POST /finish", d.navHandler(func() error { return d.session.Finish() }))

	mux.HandleFunc("POST /anchor", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ExcerptIndex int `json:"excerpt_index"`
			FirstLine    int `json:"first_line"`
			LastLine     int `json:"last_line"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad anchor request", http.StatusBadRequest)
			return
		}
		d.mu.Lock()
		anchor, err := d.session.Anchor(review.AnchorTarget{
			ExcerptIndex: req.ExcerptIndex, FirstLine: req.FirstLine, LastLine: req.LastLine,
		})
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
	return mux
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
		Version: "0.1.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name: "post_walkthrough",
		Description: "Post a Walkthrough of your changes for the Reviewer to work through. " +
			"Send it once and completely: the Reviewer navigates it without involving you. " +
			"Order Steps so each is comprehensible given only the Steps before it, and send " +
			"line ranges rather than code — dbn reads the working tree itself.",
	}, d.postWalkthrough)

	mcp.AddTool(server, &mcp.Tool{
		Name: "fetch_results",
		Description: "Ask how the review went. Returns immediately whether or not the Reviewer " +
			"has finished; it never waits. Call it once the Reviewer says they are done.",
	}, d.fetchResults)

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
	return nil, postResult{Accepted: true}, nil
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
	case results.Posted && results.Finished:
		message = "the Reviewer has finished; work the Change Requests below, then post a Revision Round"
	case results.Posted:
		message = "the Reviewer has not finished the Walkthrough yet"
	}

	return nil, toFetchResult(results, message), nil
}

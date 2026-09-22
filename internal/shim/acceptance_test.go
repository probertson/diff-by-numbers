package shim

// These are acceptance tests: they build the real dbn binary and drive `dbn mcp`
// as a subprocess over a real MCP stdio connection, observing daemon listeners
// and process lifetime. The bind-or-connect, detached-spawn and proxy behaviour
// is OS-level and cannot be unit-tested — this mirrors the installer's own shell
// harness. Prior art: internal/installer, internal/git/acceptance_test.go.

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestShimAutoStartsADaemonThenServesTheTools(t *testing.T) {
	bin := buildDBN(t)
	port := freePort(t)
	root := featureRepo(t)
	env := shimEnv(port)

	if listening(port) {
		t.Fatalf("precondition: something is already listening on port %d", port)
	}

	ctx := context.Background()
	session := connectShim(t, ctx, bin, env)

	outcome := post(t, ctx, session, minimalRound(root))

	if !outcome.Accepted {
		t.Fatalf("expected the post via the shim to be accepted, got %s", outcome.summary())
	}
	if outcome.ReviewID == "" {
		t.Error("expected a review id back through the shim")
	}
	if !listening(port) {
		t.Errorf("expected the shim to have auto-started a daemon on port %d", port)
	}

	t.Cleanup(func() { concludeAndWait(port, env, outcome.ReviewID) })
}

func TestShimConnectsToAnExistingDaemonRatherThanStartingASecond(t *testing.T) {
	bin := buildDBN(t)
	port := freePort(t)
	root := featureRepo(t)
	env := shimEnv(port)

	// A daemon already up, with a review already posted straight to it.
	startServe(t, bin, env, port)
	ctx := context.Background()
	direct := connectHTTP(t, ctx, port)
	posted := post(t, ctx, direct, minimalRound(root))
	direct.Close()
	if !posted.Accepted {
		t.Fatalf("precondition: direct post rejected: %s", posted.summary())
	}

	// The shim must reach that same daemon — it sees the already-posted review,
	// which an empty second daemon could not.
	session := connectShim(t, ctx, bin, env)
	defer session.Close()

	if !fetch(t, ctx, session).Posted {
		t.Error("expected the shim to reach the existing daemon holding the posted review, not a fresh one")
	}
}

func TestDaemonSurvivesShimExitWhileAReviewIsUnconcluded(t *testing.T) {
	bin := buildDBN(t)
	port := freePort(t)
	root := featureRepo(t)
	env := shimEnv(port)

	ctx := context.Background()
	session := connectShim(t, ctx, bin, env)
	outcome := post(t, ctx, session, minimalRound(root))
	if !outcome.Accepted {
		t.Fatalf("expected the post to be accepted, got %s", outcome.summary())
	}

	// Ending the shim (as the agent session ending would) must not take the daemon
	// with it while the review is still un-concluded.
	session.Close()
	time.Sleep(600 * time.Millisecond)

	if !listening(port) {
		t.Error("daemon exited on shim close despite an un-concluded review")
	}

	t.Cleanup(func() { concludeAndWait(port, env, outcome.ReviewID) })
}

func TestDaemonSelfExitsOnceAReviewConcludesAndTheSessionEnds(t *testing.T) {
	bin := buildDBN(t)
	port := freePort(t)
	root := featureRepo(t)
	env := shimEnv(port) // a short DBN_EXIT_GRACE

	ctx := context.Background()
	session := connectShim(t, ctx, bin, env)
	outcome := post(t, ctx, session, minimalRound(root))
	if !outcome.Accepted {
		t.Fatalf("expected the post to be accepted, got %s", outcome.summary())
	}

	// Conclude the review, then end the session so nothing keeps the daemon warm.
	concludeVia(t, ctx, session, outcome.ReviewID)
	session.Close()

	// With no active review and no one attached, it should let go within a few
	// grace windows (plus the shutdown drain).
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if !listening(port) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("expected the daemon to self-exit after the review concluded and the session ended")
}

// --- helpers ----------------------------------------------------------------

var (
	buildOnce sync.Once
	builtBin  string
	buildErr  error
)

// buildDBN builds the real binary once and shares it across tests.
func buildDBN(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "dbn-bin")
		if err != nil {
			buildErr = err
			return
		}
		bin := filepath.Join(dir, "dbn")
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/dbn")
		cmd.Dir = filepath.Join("..", "..") // repo root, relative to internal/shim
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = err
			t.Logf("go build:\n%s", out)
			return
		}
		builtBin = bin
	})
	if buildErr != nil {
		t.Fatalf("could not build dbn: %v", buildErr)
	}
	return builtBin
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not find a free port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// shimEnv points the shim (and the daemon it spawns) at the test port, with a
// short exit grace so a self-exiting daemon cleans up promptly.
func shimEnv(port int) []string {
	return append(os.Environ(),
		"DBN_PORT="+strconv.Itoa(port),
		"DBN_EXIT_GRACE=400ms",
	)
}

func connectShim(t *testing.T, ctx context.Context, bin string, env []string) *mcp.ClientSession {
	t.Helper()
	cmd := exec.Command(bin, "mcp")
	cmd.Env = env
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("could not connect to the shim: %v", err)
	}
	return session
}

func connectHTTP(t *testing.T, ctx context.Context, port int) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "http://" + addr(port) + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("could not connect to the daemon on port %d: %v", port, err)
	}
	return session
}

// startServe launches a hand-run daemon (no self-exit) and reaps it at test end.
// It returns the process so a test can end it on its own terms.
func startServe(t *testing.T, bin string, env []string, port int) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(bin, "serve", "--port", strconv.Itoa(port))
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		t.Fatalf("could not start the daemon: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	if err := waitForDaemon(port, 5*time.Second); err != nil {
		t.Fatalf("daemon did not come up: %v", err)
	}
	return cmd
}

// concludeAndWait ends a review so its self-exiting daemon can let go, then waits
// for the port to free, so a test leaves nothing behind.
func concludeAndWait(port int, env []string, reviewID string) {
	if listening(port) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		client := mcp.NewClient(&mcp.Implementation{Name: "cleanup", Version: "0"}, nil)
		if session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "http://" + addr(port) + "/mcp"}, nil); err == nil {
			_, _ = session.CallTool(ctx, &mcp.CallToolParams{Name: "conclude", Arguments: map[string]any{"review_id": reviewID}})
			session.Close()
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !listening(port) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

type postOutcome struct {
	Accepted bool          `json:"accepted"`
	ReviewID string        `json:"review_id"`
	Problems []problemJSON `json:"problems"`
}

// problemJSON mirrors one entry of the wire's problems array. These outcomes
// only ever appear in failure messages here, but an empty one would defeat the
// purpose of printing it.
type problemJSON struct {
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

func (o postOutcome) summary() string {
	var parts []string
	for _, problem := range o.Problems {
		parts = append(parts, problem.Reason+": "+problem.Detail)
	}
	return strings.Join(parts, "; ")
}

type fetchOutcome struct {
	Posted bool `json:"posted"`
}

func post(t *testing.T, ctx context.Context, session *mcp.ClientSession, wt map[string]any) postOutcome {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "post_round", Arguments: wt})
	if err != nil {
		t.Fatalf("post_round failed: %v", err)
	}
	return decode[postOutcome](t, result)
}

func concludeVia(t *testing.T, ctx context.Context, session *mcp.ClientSession, reviewID string) {
	t.Helper()
	if _, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "conclude", Arguments: map[string]any{"review_id": reviewID}}); err != nil {
		t.Fatalf("conclude failed: %v", err)
	}
}

func fetch(t *testing.T, ctx context.Context, session *mcp.ClientSession) fetchOutcome {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "fetch_results", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("fetch_results failed: %v", err)
	}
	return decode[fetchOutcome](t, result)
}

func decode[T any](t *testing.T, result *mcp.CallToolResult) T {
	t.Helper()
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("could not read the tool result: %v", err)
	}
	var out T
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("could not decode the tool result: %v", err)
	}
	return out
}

// featureRepo makes a temp repo with one added line and one mechanical file, so a
// derivation has something real to find. Mirrors the daemon test's fixture.
func featureRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	path := filepath.Join(root, "FILE-src/fetch.ts")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("init", "-q", "-b", "main")
	git("add", ".")
	git("commit", "-qm", "initial")
	git("checkout", "-q", "-b", "feature")
	if err := os.WriteFile(path, []byte("a\nb\nc\nADDED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "LOCKFILE"), []byte("dep-1\ndep-2\ndep-3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "LOCKFILE")
	return root
}

func minimalRound(root string) map[string]any {
	return map[string]any{
		"brief": map[string]any{
			"goal":     "goal",
			"approach": "approach",
		},
		"repositories": []any{map[string]any{"root": root, "base": "main"}},
		"steps": []any{
			map[string]any{
				"name":        "the change",
				"explanation": "why",
				"excerpts": []any{map[string]any{
					"repository": root, "file": "FILE-src/fetch.ts", "side": "new", "first_line": 1, "last_line": 4,
				}},
			},
			map[string]any{
				"name":             "the lockfile",
				"explanation":      "mechanical",
				"acknowledgements": []any{map[string]any{"repository": root, "files": []any{"LOCKFILE"}, "reason": "regenerated"}},
			},
		},
	}
}

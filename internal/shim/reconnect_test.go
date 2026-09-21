package shim

// Acceptance tests for the shim outliving the daemon it connected to. Like the
// rest of this package's tests they drive the real binary: the failure being
// covered is one process losing another, which has no in-process stand-in.

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAToolCallSurvivesTheDaemonBeingReplaced(t *testing.T) {
	bin := buildDBN(t)
	port := freePort(t)
	env := shimEnv(port)

	// A hand-run daemon the test can kill, and a shim connected to it.
	daemon := startServe(t, bin, env, port)
	ctx := context.Background()
	session := connectShim(t, ctx, bin, env)
	defer session.Close()
	if !fetchOK(t, ctx, session) {
		t.Fatal("precondition: the shim could not reach the daemon it started against")
	}

	// The daemon goes away mid-session, as `dbn update` restarting it or a crash
	// would. Nothing tells the shim; it finds out on its next call.
	kill(t, daemon, port)

	if !fetchOK(t, ctx, session) {
		t.Error("a tool call after the daemon was killed did not reach a fresh daemon")
	}
	if !listening(port) {
		t.Error("expected the shim to have started a replacement daemon")
	}

	t.Cleanup(func() { concludeAndWait(port, env, "") })
}

// A refusal is the daemon answering, not the connection failing — it must come
// back as-is, and must not cost the caller its session. The second conclude is
// the proof: a shim that had torn down and restarted the daemon would be talking
// to one that never saw the Walkthrough, and could not conclude it either.
func TestARefusedToolCallIsReturnedAsIsRatherThanRetried(t *testing.T) {
	bin := buildDBN(t)
	port := freePort(t)
	root := featureRepo(t)
	env := shimEnv(port)

	ctx := context.Background()
	session := connectShim(t, ctx, bin, env)
	defer session.Close()
	outcome := post(t, ctx, session, minimalWalkthrough(root))
	if !outcome.Accepted {
		t.Fatalf("precondition: the post was rejected: %s", outcome.summary())
	}

	refused := conclude(t, ctx, session, "not-"+outcome.ReviewID)

	if refused.Concluded {
		t.Error("expected conclude with an unknown id to be refused")
	}
	if accepted := conclude(t, ctx, session, outcome.ReviewID); !accepted.Concluded {
		t.Errorf("the session did not survive a refusal: %s", accepted.Message)
	}
}

// --- helpers ----------------------------------------------------------------

type concludeOutcome struct {
	Concluded bool   `json:"concluded"`
	Message   string `json:"message"`
}

func conclude(t *testing.T, ctx context.Context, session *mcp.ClientSession, reviewID string) concludeOutcome {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "conclude", Arguments: map[string]any{"review_id": reviewID}})
	if err != nil {
		t.Fatalf("conclude failed at the protocol level: %v", err)
	}
	return decode[concludeOutcome](t, result)
}

// fetchOK reports whether a forwarded call reaches a daemon at all, without
// failing the test — the point of these tests is which calls come back.
func fetchOK(t *testing.T, ctx context.Context, session *mcp.ClientSession) bool {
	t.Helper()
	_, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "fetch_results", Arguments: map[string]any{}})
	if err != nil {
		t.Logf("fetch_results: %v", err)
	}
	return err == nil
}

// kill stops a daemon outright — no graceful shutdown, the way a crash or a
// `kill -9` leaves a shim holding a session no one will answer.
func kill(t *testing.T, daemon *exec.Cmd, port int) {
	t.Helper()
	if err := daemon.Process.Kill(); err != nil {
		t.Fatalf("could not kill the daemon: %v", err)
	}
	_, _ = daemon.Process.Wait()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !listening(port) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the killed daemon is still listening on port %d", port)
}

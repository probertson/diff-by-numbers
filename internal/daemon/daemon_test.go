package daemon_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// featureRepo makes a temp repo on main, branches, and adds one line to FILE-src/fetch.ts
// at line 4242's stand-in — small and real, so derivation has something to find.
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
	os.WriteFile(path, []byte("a\nb\nc\n"), 0o644)
	git("init", "-q", "-b", "main")
	git("add", ".")
	git("commit", "-qm", "initial")
	git("checkout", "-q", "-b", "feature")
	os.WriteFile(path, []byte("a\nb\nc\nADDED\n"), 0o644) // adds new-side line 4
	// A mechanical file the Walkthrough will acknowledge rather than excerpt.
	// Staged so it appears in the diff against the base, like any tracked change.
	os.WriteFile(filepath.Join(root, "LOCKFILE"), []byte("dep-1\ndep-2\ndep-3\n"), 0o644)
	git("add", "LOCKFILE")
	return root
}

// The wire types are deliberately separate from the domain types, which means a
// hand-written mapping between them. This test exists for the one failure that
// mapping can have: silently dropping a field. Every value below is distinctive,
// so anything lost on the way in is missing on the way out.
func TestEveryPostedFieldSurvivesTheRoundTrip(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()

	root := featureRepo(t)
	posted := map[string]any{
		"brief": map[string]any{
			"ask":      "ASK-tenant-scoping",
			"approach": "APPROACH-thread-the-id-through",
			"provenance": map[string]any{
				"kind":     "stated",
				"citation": "CITATION-session-51e67df2",
			},
		},
		"repositories": []any{
			map[string]any{"root": root, "range": "main"},
		},
		"steps": []any{
			map[string]any{
				"name":                   "NAME-add-the-retrier",
				"explanation":            "EXPLANATION-wraps-the-transport",
				"oversize_justification": "JUSTIFICATION-the-state-machine-only-makes-sense-whole",
				"excerpts": []any{
					map[string]any{
						"repository": root,
						"file":       "FILE-src/fetch.ts",
						"side":       "new",
						"first_line": 1,
						"last_line":  4,
					},
				},
			},
			map[string]any{
				"name":        "NAME-regenerate-the-lockfile",
				"explanation": "EXPLANATION-mechanical-dependency-bump",
				"acknowledgements": []any{
					map[string]any{
						"repository": root,
						"files":      []any{"LOCKFILE"},
						"reason":     "REASON-regenerated-by-the-package-manager",
					},
				},
			},
		},
	}

	postWalkthrough(t, server.URL, posted)

	dump := get(t, server.URL+"/dump")
	for _, want := range []string{
		"ASK-tenant-scoping",
		"APPROACH-thread-the-id-through",
		"stated",
		"CITATION-session-51e67df2",
		root,
		"main",
		"NAME-add-the-retrier",
		"EXPLANATION-wraps-the-transport",
		"JUSTIFICATION-the-state-machine-only-makes-sense-whole",
		"FILE-src/fetch.ts",
		"new",
		"FILE-src/fetch.ts",
		"NAME-regenerate-the-lockfile",
		"EXPLANATION-mechanical-dependency-bump",
		"LOCKFILE",
		"REASON-regenerated-by-the-package-manager",
	} {
		if !strings.Contains(dump, want) {
			t.Errorf("%q did not survive the round trip\n--- dump ---\n%s", want, dump)
		}
	}
}

func postWalkthrough(t *testing.T, baseURL string, walkthrough map[string]any) {
	t.Helper()
	ctx := context.Background()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: baseURL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("could not connect to the daemon: %v", err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "post_walkthrough",
		Arguments: walkthrough,
	})
	if err != nil {
		t.Fatalf("post_walkthrough failed: %v", err)
	}

	// The daemon reports a refusal as structured output rather than a protocol
	// error, so acceptance has to be read out of the result. Without this the
	// test would report every field as lost when the real cause was a rejection.
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("could not read the post result: %v", err)
	}
	var outcome struct {
		Accepted bool   `json:"accepted"`
		Reason   string `json:"reason"`
		Detail   string `json:"detail"`
	}
	if err := json.Unmarshal(encoded, &outcome); err != nil {
		t.Fatalf("could not decode the post result: %v", err)
	}
	if !outcome.Accepted {
		t.Fatalf("expected the Walkthrough to be accepted, got %s: %s", outcome.Reason, outcome.Detail)
	}
}

func get(t *testing.T, url string) string {
	t.Helper()

	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("could not GET %s: %v", url, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("could not read %s: %v", url, err)
	}
	return string(body)
}

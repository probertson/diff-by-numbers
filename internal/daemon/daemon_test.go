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
	// A mechanical file the Round will acknowledge rather than excerpt.
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
			"goal":     "GOAL-tenant-scoping",
			"approach": "APPROACH-thread-the-id-through",
		},
		"repositories": []any{
			map[string]any{"root": root, "base": "main"},
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

	postRound(t, server.URL, posted)

	dump := get(t, server.URL+"/dump")
	for _, want := range []string{
		"GOAL-tenant-scoping",
		"APPROACH-thread-the-id-through",
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

// callTool opens a fresh MCP session, calls one tool, and returns its result.
// The daemon reports refusals as structured output rather than protocol errors,
// so the caller reads acceptance out of the decoded result.
func callTool(t *testing.T, baseURL, name string, args any) *mcp.CallToolResult {
	t.Helper()
	ctx := context.Background()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: baseURL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("could not connect to the daemon: %v", err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s failed: %v", name, err)
	}
	return result
}

func decodeResult[T any](t *testing.T, result *mcp.CallToolResult) T {
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

type postOutcome struct {
	Accepted bool          `json:"accepted"`
	Problems []problemJSON `json:"problems"`
	ReviewID string        `json:"review_id"`
	Message  string        `json:"message"`
}

// problemJSON mirrors one entry of the wire's problems array.
type problemJSON struct {
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

// summary renders an outcome's problems for a failure message.
func (o postOutcome) summary() string {
	var parts []string
	for _, problem := range o.Problems {
		parts = append(parts, problem.Reason+": "+problem.Detail)
	}
	return strings.Join(parts, "; ")
}

// has reports whether any problem carries this reason.
func (o postOutcome) has(reason string) bool {
	for _, problem := range o.Problems {
		if problem.Reason == reason {
			return true
		}
	}
	return false
}

func postRound(t *testing.T, baseURL string, walkthrough map[string]any) postOutcome {
	t.Helper()
	outcome := decodeResult[postOutcome](t, callTool(t, baseURL, "post_round", walkthrough))
	if !outcome.Accepted {
		t.Fatalf("expected the Round to be accepted, got %s", outcome.summary())
	}
	return outcome
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

// roundWithRepository is the smallest valid post, with the repository
// entry supplied by the caller so a test can vary just that.
func roundWithRepository(root string, repository map[string]any) map[string]any {
	return map[string]any{
		"brief": map[string]any{
			"goal":     "GOAL-tenant-scoping",
			"approach": "APPROACH-thread-the-id-through",
		},
		"repositories": []any{repository},
		"steps": []any{
			map[string]any{
				"name":        "NAME-the-change",
				"explanation": "EXPLANATION-what-it-does",
				"excerpts": []any{
					map[string]any{
						"repository": root, "file": "FILE-src/fetch.ts",
						"side": "new", "first_line": 1, "last_line": 4,
					},
				},
				"acknowledgements": []any{
					map[string]any{
						"repository": root, "files": []any{"LOCKFILE"},
						"reason": "regenerated lockfile",
					},
				},
			},
		},
	}
}

// With one repository under review there is nothing else `repository` could
// mean, so the schema lets the Authoring Agent leave it out — and dbn reports
// the resolved value back, so nothing downstream sees a blank.
func TestASingleRepositoryPostMayOmitTheRepository(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()

	root := featureRepo(t)
	postRound(t, server.URL, map[string]any{
		"brief": map[string]any{
			"goal":     "GOAL-tenant-scoping",
			"approach": "APPROACH-thread-the-id-through",
		},
		"repositories": []any{map[string]any{"root": root, "base": "main"}},
		"steps": []any{map[string]any{
			"name":        "NAME-the-change",
			"explanation": "EXPLANATION-what-it-does",
			"excerpts": []any{
				map[string]any{"file": "FILE-src/fetch.ts", "side": "new", "first_line": 1, "last_line": 4},
			},
			"acknowledgements": []any{
				map[string]any{"files": []any{"LOCKFILE"}, "reason": "regenerated lockfile"},
			},
		}},
	})

	dump := get(t, server.URL+"/dump")

	// The Change Set section names the root regardless, so the assertion has to
	// be on the Excerpt and the Acknowledgement — the entries that omitted it.
	for _, want := range []string{
		root + " FILE-src/fetch.ts:1-4 (new side)",
		"acknowledged in " + root + ": LOCKFILE",
	} {
		if !strings.Contains(dump, want) {
			t.Errorf("expected %q in the dump\n--- dump ---\n%s", want, dump)
		}
	}
}

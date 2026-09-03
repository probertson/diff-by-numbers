package daemon_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// The wire types are deliberately separate from the domain types, which means a
// hand-written mapping between them. This test exists for the one failure that
// mapping can have: silently dropping a field. Every value below is distinctive,
// so anything lost on the way in is missing on the way out.
func TestEveryPostedFieldSurvivesTheRoundTrip(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()

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
			map[string]any{"root": "/ROOT-argus-portal", "range": "RANGE-merge-base"},
		},
		"steps": []any{
			map[string]any{
				"name":                   "NAME-add-the-retrier",
				"explanation":            "EXPLANATION-wraps-the-transport",
				"oversize_justification": "JUSTIFICATION-the-state-machine-only-makes-sense-whole",
				"excerpts": []any{
					map[string]any{
						"repository": "/ROOT-argus-portal",
						"file":       "FILE-src/fetch.ts",
						"side":       "old",
						"first_line": 4242,
						"last_line":  4343,
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
		"/ROOT-argus-portal",
		"RANGE-merge-base",
		"NAME-add-the-retrier",
		"EXPLANATION-wraps-the-transport",
		"JUSTIFICATION-the-state-machine-only-makes-sense-whole",
		"FILE-src/fetch.ts",
		"old",
		"4242",
		"4343",
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

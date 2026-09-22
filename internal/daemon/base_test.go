package daemon_test

import (
	"strings"
	"testing"

	"net/http/httptest"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// The wire carries `base`, not `range`. The rename ships with no alias, and the
// tool schema is closed, so an agent still sending the old name is told which
// field it got wrong rather than having it silently ignored — which would leave
// the Change Set undefined for a reason nothing on the wire explains.
func TestTheWireRejectsTheOldRangeFieldByName(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)

	result := callTool(t, server.URL, "post_round",
		roundWithRepository(root, map[string]any{"root": root, "range": "main"}))

	if !result.IsError {
		t.Fatal("the old `range` field is gone and must not quietly work")
	}
	if text := resultText(t, result); !strings.Contains(text, `["range"]`) {
		t.Errorf("the rejection should name the field that is no longer there, got %q", text)
	}
}

// The other half of the rename: the new name has to actually work, or the test
// above would pass against a wire that rejects everything.
func TestTheWireAcceptsABaseRef(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)

	outcome := decodeResult[postOutcome](t, callTool(t, server.URL, "post_round",
		roundWithRepository(root, map[string]any{"root": root, "base": "main"})))

	if !outcome.Accepted {
		t.Fatalf("a plain base ref is exactly what the field is for, got %s", outcome.summary())
	}
}

// A range where a ref belongs is named for what it is, before any git call.
func TestTheWireRejectsARangeAsABase(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)

	outcome := decodeResult[postOutcome](t, callTool(t, server.URL, "post_round",
		roundWithRepository(root, map[string]any{"root": root, "base": "HEAD~1..HEAD"})))

	if outcome.Accepted {
		t.Fatal("A..B is not a base ref")
	}
	if !outcome.has("malformed_base") {
		t.Errorf("expected a malformed_base problem, got %s", outcome.summary())
	}
	if !strings.Contains(outcome.summary(), "base takes a single ref, not A..B") {
		t.Errorf("the rejection should explain the field, got %q", outcome.summary())
	}
}

// resultText is everything the tool said, for the errors the MCP layer raises
// before a structured rejection can exist.
func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	var all strings.Builder
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			all.WriteString(text.Text)
		}
	}
	return all.String()
}

// The rejection an agent actually receives carries every problem, so it can fix
// them all before posting the whole Round again.
func TestARefusedPostCarriesEveryProblemOnTheWire(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)

	// A Step covering nothing, plus an Acknowledgement of a file with no
	// changes: two independent stage-2 faults.
	walkthrough := roundWithRepository(root, map[string]any{"root": root, "base": "main"})
	steps := walkthrough["steps"].([]any)
	step := steps[0].(map[string]any)
	step["excerpts"] = []any{
		map[string]any{
			"repository": root, "file": "FILE-src/fetch.ts",
			"side": "new", "first_line": 1, "last_line": 1,
		},
	}
	step["acknowledgements"] = []any{
		map[string]any{"repository": root, "files": []any{"NOT-A-CHANGED-FILE"}, "reason": "nothing"},
	}

	outcome := decodeResult[postOutcome](t, callTool(t, server.URL, "post_round", walkthrough))

	if outcome.Accepted {
		t.Fatal("expected the post to be refused")
	}
	if len(outcome.Problems) < 2 {
		t.Fatalf("expected every problem, got %d: %s", len(outcome.Problems), outcome.summary())
	}
	if !outcome.has("empty_acknowledgement") || !outcome.has("uncovered_changes") {
		t.Errorf("expected both faults reported together, got %s", outcome.summary())
	}
	for _, problem := range outcome.Problems {
		if problem.Reason == "" || problem.Detail == "" {
			t.Errorf("every problem needs a reason and a detail, got %+v", problem)
		}
	}
}

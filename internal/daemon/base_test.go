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

	result := callTool(t, server.URL, "post_walkthrough",
		walkthroughWithRepository(root, map[string]any{"root": root, "range": "main"}))

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

	outcome := decodeResult[postOutcome](t, callTool(t, server.URL, "post_walkthrough",
		walkthroughWithRepository(root, map[string]any{"root": root, "base": "main"})))

	if !outcome.Accepted {
		t.Fatalf("a plain base ref is exactly what the field is for, got %s: %s", outcome.Reason, outcome.Detail)
	}
}

// A range where a ref belongs is named for what it is, before any git call.
func TestTheWireRejectsARangeAsABase(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)

	outcome := decodeResult[postOutcome](t, callTool(t, server.URL, "post_walkthrough",
		walkthroughWithRepository(root, map[string]any{"root": root, "base": "HEAD~1..HEAD"})))

	if outcome.Accepted {
		t.Fatal("A..B is not a base ref")
	}
	if outcome.Reason != "malformed_base" {
		t.Errorf("expected malformed_base, got %s: %s", outcome.Reason, outcome.Detail)
	}
	if !strings.Contains(outcome.Detail, "base takes a single ref, not A..B") {
		t.Errorf("the rejection should explain the field, got %q", outcome.Detail)
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

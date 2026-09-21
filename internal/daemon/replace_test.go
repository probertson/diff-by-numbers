package daemon_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// TestAReviewIsReplacedInPlaceThroughTheDaemon drives #56 over the real
// surfaces: a second post over a live review is refused with the way forward,
// posting again with replaces takes its place under the same id, and the
// Reviewer's Comment carries over to both the TUI and the agent.
func TestAReviewIsReplacedInPlaceThroughTheDaemon(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	walkthrough := func() map[string]any {
		return map[string]any{
			"brief":        map[string]any{"ask": "x", "approach": "y", "provenance": map[string]any{"kind": "stated", "citation": "s"}},
			"repositories": []any{map[string]any{"root": root, "base": "main"}},
			"steps": []any{
				map[string]any{
					"name": "The code", "explanation": "fetch.ts gains a line",
					"excerpts": []any{map[string]any{"repository": root, "file": "FILE-src/fetch.ts", "side": "new", "first_line": 1, "last_line": 4}},
				},
				map[string]any{
					"name": "Mechanical", "explanation": "the lockfile",
					"acknowledgements": []any{map[string]any{"repository": root, "files": []any{"LOCKFILE"}, "reason": "generated"}},
				},
			},
		}
	}
	first := postWalkthrough(t, server.URL, walkthrough())
	httpPost(t, server.URL+"/goto/1")
	raiseComment(t, server.URL, 0, 4, 4, "please rename this")

	refused := decodeResult[postOutcome](t, callTool(t, server.URL, "post_walkthrough", walkthrough()))
	replacing := walkthrough()
	replacing["replaces"] = first.ReviewID
	replaced := postWalkthrough(t, server.URL, replacing)

	if refused.Accepted || !strings.Contains(refused.summary(), `replaces: "`+first.ReviewID+`"`) {
		t.Errorf("expected a second post to be refused, naming replaces, got %+v", refused)
	}
	if !strings.HasPrefix(first.Message, "Posted. The Reviewer opens it by running dbn in a terminal") {
		t.Errorf("expected a first post to say how the Reviewer opens it, got %q", first.Message)
	}
	if !strings.HasPrefix(replaced.Message, "The Walkthrough is replaced. The Reviewer opens it by running dbn") {
		t.Errorf("expected a replacement to say so, got %q", replaced.Message)
	}
	if replaced.ReviewID != first.ReviewID {
		t.Errorf("expected the replacement to keep review %q, got %q", first.ReviewID, replaced.ReviewID)
	}
	view := replacedView(t, server.URL)
	if !view.Replaced || len(view.Comments) != 1 || !view.Comments[0].CarriedOver {
		t.Errorf("expected the view to show a replacement with one carried-over Comment, got %+v", view)
	}
	results := decodeResult[struct {
		Comments []struct {
			CarriedOver bool `json:"carried_over"`
		} `json:"comments"`
	}](t, callTool(t, server.URL, "fetch_results", struct{}{}))
	if len(results.Comments) != 1 || !results.Comments[0].CarriedOver {
		t.Errorf("expected carried_over to reach the agent, got %+v", results.Comments)
	}
}

type replacedShape struct {
	Replaced bool `json:"replaced"`
	Comments []struct {
		CarriedOver bool `json:"carried_over"`
	} `json:"comments"`
}

func replacedView(t *testing.T, baseURL string) replacedShape {
	t.Helper()
	response, err := http.Get(baseURL + "/view")
	if err != nil {
		t.Fatalf("GET /view: %v", err)
	}
	defer response.Body.Close()
	var view replacedShape
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		t.Fatalf("decode /view: %v", err)
	}
	return view
}

package daemon_test

import (
	"net/http/httptest"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// minimalWalkthrough covers exactly what featureRepo changed: the added line in
// fetch.ts and the mechanical LOCKFILE. It is the smallest post the daemon will
// accept for that repo.
func minimalWalkthrough(root string) map[string]any {
	return map[string]any{
		"brief": map[string]any{
			"ask":        "ask",
			"approach":   "approach",
			"provenance": map[string]any{"kind": "stated", "citation": "session-x"},
		},
		"repositories": []any{map[string]any{"root": root, "range": "main"}},
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

func TestPostWalkthroughReturnsAReviewID(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)

	outcome := postWalkthrough(t, server.URL, minimalWalkthrough(root))

	if outcome.ReviewID == "" {
		t.Error("expected post_walkthrough to return a review id")
	}
}

type concludeOutcome struct {
	Concluded bool   `json:"concluded"`
	Message   string `json:"message"`
}

func TestConcludeToolEndsTheReviewByID(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	id := postWalkthrough(t, server.URL, minimalWalkthrough(root)).ReviewID

	wrong := decodeResult[concludeOutcome](t, callTool(t, server.URL, "conclude", map[string]any{"review_id": "not-" + id}))
	if wrong.Concluded {
		t.Errorf("expected conclude to refuse an unknown id, got concluded=true (%s)", wrong.Message)
	}

	right := decodeResult[concludeOutcome](t, callTool(t, server.URL, "conclude", map[string]any{"review_id": id}))
	if !right.Concluded {
		t.Errorf("expected conclude to succeed for the real id, got: %s", right.Message)
	}
}

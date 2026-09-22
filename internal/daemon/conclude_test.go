package daemon_test

import (
	"net/http/httptest"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// minimalRound covers exactly what featureRepo changed: the added line in
// fetch.ts and the mechanical LOCKFILE. It is the smallest post the daemon will
// accept for that repo.
func minimalRound(root string) map[string]any {
	return map[string]any{
		"label": "LABEL-the-review",
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

func TestPostRoundReturnsAReviewID(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)

	outcome := postRound(t, server.URL, minimalRound(root))

	if outcome.ReviewID == "" {
		t.Error("expected post_round to return a review id")
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
	id := postRound(t, server.URL, minimalRound(root)).ReviewID

	wrong := decodeResult[concludeOutcome](t, callTool(t, server.URL, "conclude", map[string]any{"review_id": "not-" + id}))
	if wrong.Concluded {
		t.Errorf("expected conclude to refuse an unknown id, got concluded=true (%s)", wrong.Message)
	}

	right := decodeResult[concludeOutcome](t, callTool(t, server.URL, "conclude", map[string]any{"review_id": id}))
	if !right.Concluded {
		t.Errorf("expected conclude to succeed for the real id, got: %s", right.Message)
	}
}

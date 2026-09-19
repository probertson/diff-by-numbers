package daemon_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

func httpPost(t *testing.T, url string) {
	t.Helper()
	response, err := http.Post(url, "text/plain", nil)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("POST %s answered %s: %s", url, response.Status, body)
	}
}

func raiseComment(t *testing.T, baseURL string, excerpt, first, last int, note string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"excerpt_index": excerpt,
		"start":         map[string]any{"line": first},
		"end":           map[string]any{"line": last},
		"note":          note,
	})
	response, err := http.Post(baseURL+"/comment", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("raise comment: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(response.Body)
		t.Fatalf("raise comment answered %s: %s", response.Status, b)
	}
}

type viewShape struct {
	Dispositions []struct {
		CommentID int    `json:"comment_id"`
		Status    string `json:"status"`
		Response  string `json:"response"`
	} `json:"dispositions"`
	Comments []struct {
		ID int `json:"id"`
	} `json:"comments"`
}

func getView(t *testing.T, baseURL string) viewShape {
	t.Helper()
	response, err := http.Get(baseURL + "/view")
	if err != nil {
		t.Fatalf("GET /view: %v", err)
	}
	defer response.Body.Close()
	var view viewShape
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		t.Fatalf("decode /view: %v", err)
	}
	return view
}

// TestARevisionRoundFlowsThroughTheDaemon drives the whole loop over the real
// surfaces: post, raise a Comment, finish, post a Revision Round that
// declines it, see the decline on the view, and re-raise it.
func TestARevisionRoundFlowsThroughTheDaemon(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)

	steps := []any{
		map[string]any{
			"name": "The code", "explanation": "fetch.ts gains a line",
			"excerpts": []any{map[string]any{"repository": root, "file": "FILE-src/fetch.ts", "side": "new", "first_line": 1, "last_line": 4}},
		},
		map[string]any{
			"name": "Mechanical", "explanation": "the lockfile",
			"acknowledgements": []any{map[string]any{"repository": root, "files": []any{"LOCKFILE"}, "reason": "generated"}},
		},
	}
	brief := map[string]any{
		"ask": "x", "approach": "y",
		"provenance": map[string]any{"kind": "stated", "citation": "s"},
	}

	// Round 1: post, flag the code Step, finish.
	postWalkthrough(t, server.URL, map[string]any{
		"brief": brief, "repositories": []any{map[string]any{"root": root, "range": "main"}}, "steps": steps,
	})
	httpPost(t, server.URL+"/goto/1")
	raiseComment(t, server.URL, 0, 4, 4, "please rename this")
	httpPost(t, server.URL+"/finish")

	// Round 2: a Revision Round declining the one Comment.
	postWalkthrough(t, server.URL, map[string]any{
		"brief": brief, "repositories": []any{map[string]any{"root": root, "range": "main"}}, "steps": steps,
		"dispositions": []any{map[string]any{"comment_id": 1, "status": "declined", "response": "the name is deliberate"}},
	})

	view := getView(t, server.URL)
	if len(view.Dispositions) != 1 || view.Dispositions[0].Status != "declined" || view.Dispositions[0].Response == "" {
		t.Fatalf("expected one declined disposition with a response on the view, got %+v", view.Dispositions)
	}
	if len(view.Comments) != 0 {
		t.Fatalf("expected the Revision Round to start with no Comments, got %d", len(view.Comments))
	}

	// The Reviewer re-raises the decline.
	httpPost(t, server.URL+"/reraise/1")

	after := getView(t, server.URL)
	if len(after.Comments) != 1 {
		t.Fatalf("expected the re-raised Comment to stand, got %d", len(after.Comments))
	}
}

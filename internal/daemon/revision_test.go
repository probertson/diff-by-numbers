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

func raiseChangeRequest(t *testing.T, baseURL string, excerpt, first, last int, note string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"excerpt_index": excerpt, "first_line": first, "last_line": last, "note": note})
	response, err := http.Post(baseURL+"/changerequest", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("raise change request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(response.Body)
		t.Fatalf("raise change request answered %s: %s", response.Status, b)
	}
}

type viewShape struct {
	Dispositions []struct {
		ChangeRequestID int    `json:"change_request_id"`
		Status          string `json:"status"`
		Reasoning       string `json:"reasoning"`
	} `json:"dispositions"`
	ChangeRequests []struct {
		ID int `json:"id"`
	} `json:"change_requests"`
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
// surfaces: post, raise a Change Request, finish, post a Revision Round that
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
	raiseChangeRequest(t, server.URL, 0, 4, 4, "please rename this")
	httpPost(t, server.URL+"/finish")

	// Round 2: a Revision Round declining the one Change Request.
	postWalkthrough(t, server.URL, map[string]any{
		"brief": brief, "repositories": []any{map[string]any{"root": root, "range": "main"}}, "steps": steps,
		"dispositions": []any{map[string]any{"change_request_id": 1, "status": "declined", "reasoning": "the name is deliberate"}},
	})

	view := getView(t, server.URL)
	if len(view.Dispositions) != 1 || view.Dispositions[0].Status != "declined" || view.Dispositions[0].Reasoning == "" {
		t.Fatalf("expected one declined disposition with reasoning on the view, got %+v", view.Dispositions)
	}
	if len(view.ChangeRequests) != 0 {
		t.Fatalf("expected the Revision Round to start with no Change Requests, got %d", len(view.ChangeRequests))
	}

	// The Reviewer re-raises the decline.
	httpPost(t, server.URL+"/reraise/1")

	after := getView(t, server.URL)
	if len(after.ChangeRequests) != 1 {
		t.Fatalf("expected the re-raised Change Request to stand, got %d", len(after.ChangeRequests))
	}
}

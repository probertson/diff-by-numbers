package daemon_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
		ID           int `json:"id"`
		ReRaisedFrom int `json:"re_raised_from"`
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
		"brief": brief, "repositories": []any{map[string]any{"root": root, "base": "main"}}, "steps": steps,
	})
	httpPost(t, server.URL+"/goto/1")
	raiseComment(t, server.URL, 0, 4, 4, "please rename this")
	httpPost(t, server.URL+"/finish")

	// Round 2: a Revision Round declining the one Comment.
	revised := postWalkthrough(t, server.URL, map[string]any{
		"brief": brief, "repositories": []any{map[string]any{"root": root, "base": "main"}}, "steps": steps,
		"dispositions": []any{map[string]any{"comment_id": 1, "status": "declined", "response": "the name is deliberate"}},
	})

	if !strings.HasPrefix(revised.Message, "The Revision Round is posted. The Reviewer opens it by running dbn") {
		t.Errorf("expected the Revision Round to be announced as one, got %q", revised.Message)
	}
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
	// #80: the re-raise names the decline it disputes, on the view and in what the
	// agent fetches, so neither has to guess which point came back.
	if after.Comments[0].ReRaisedFrom != 1 {
		t.Errorf("expected the re-raise to name Comment 1, got %d", after.Comments[0].ReRaisedFrom)
	}

	httpPost(t, server.URL+"/finish")
	results := decodeResult[struct {
		Comments []struct {
			ID           int `json:"id"`
			ReRaisedFrom int `json:"re_raised_from"`
		} `json:"comments"`
	}](t, callTool(t, server.URL, "fetch_results", struct{}{}))

	if len(results.Comments) != 1 || results.Comments[0].ReRaisedFrom != 1 {
		t.Errorf("expected re_raised_from to reach the agent, got %+v", results.Comments)
	}
}

// TestARaisedResolutionCannotBeReRaisedTwice holds the daemon to the core rule
// (#80): the picker hides what is already re-raised, and the endpoint refuses it
// even if something asks anyway.
func TestARaisedResolutionCannotBeReRaisedTwice(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)

	steps := []any{map[string]any{
		"name": "The code", "explanation": "fetch.ts gains a line",
		"excerpts": []any{map[string]any{"repository": root, "file": "FILE-src/fetch.ts", "side": "new", "first_line": 1, "last_line": 4}},
	}, map[string]any{
		"name": "Mechanical", "explanation": "the lockfile",
		"acknowledgements": []any{map[string]any{"repository": root, "files": []any{"LOCKFILE"}, "reason": "generated"}},
	}}
	brief := map[string]any{"ask": "x", "approach": "y",
		"provenance": map[string]any{"kind": "stated", "citation": "s"}}
	body := map[string]any{"brief": brief,
		"repositories": []any{map[string]any{"root": root, "base": "main"}}, "steps": steps}

	postWalkthrough(t, server.URL, body)
	httpPost(t, server.URL+"/goto/1")
	raiseComment(t, server.URL, 0, 4, 4, "please rename this")
	httpPost(t, server.URL+"/finish")
	withDecline := map[string]any{"brief": brief,
		"repositories": []any{map[string]any{"root": root, "base": "main"}}, "steps": steps,
		"dispositions": []any{map[string]any{"comment_id": 1, "status": "declined", "response": "deliberate"}}}
	postWalkthrough(t, server.URL, withDecline)
	httpPost(t, server.URL+"/reraise/1")

	second, err := http.Post(server.URL+"/reraise/1", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Body.Close()

	if second.StatusCode == http.StatusOK {
		t.Error("a decline with a re-raise standing must not be re-raised again")
	}
	if got := len(getView(t, server.URL).Comments); got != 1 {
		t.Errorf("the refused re-raise must not leave a duplicate, got %d Comments", got)
	}
}

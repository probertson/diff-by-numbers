package daemon_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// editedRepo makes a temp repo whose feature branch *rewrites* a line rather than
// appending one, so git derives a before/after correspondence and the Step renders
// as a unified diff with a removed row to select.
func editedRepo(t *testing.T) string {
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
	path := filepath.Join(root, "fetch.ts")
	os.WriteFile(path, []byte("keep\nthe old guard\ntail\n"), 0o644)
	git("init", "-q", "-b", "main")
	git("add", ".")
	git("commit", "-qm", "initial")
	git("checkout", "-q", "-b", "feature")
	os.WriteFile(path, []byte("keep\nthe new guard\ntail\n"), 0o644)
	return root
}

// The TUI sends the two ends of the Reviewer's selection and the daemon derives
// the rows between them, so this is the seam where the two can disagree without
// anyone noticing: correct segments derived from endpoints that named other rows.
func TestACommentSpansTheSidesItsEndpointsReach(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := editedRepo(t)
	postWalkthrough(t, server.URL, map[string]any{
		"brief": map[string]any{
			"ask": "rework the guard", "approach": "renamed it",
			"provenance": map[string]any{"kind": "stated", "citation": "session-1"},
		},
		"repositories": []any{map[string]any{"root": root, "base": "main"}},
		"steps": []any{map[string]any{
			"name": "Rework the guard", "explanation": "one line became another",
			"excerpts": []any{map[string]any{
				"repository": root, "file": "fetch.ts", "side": "new", "first_line": 1, "last_line": 3,
			}},
		}},
	})
	post(t, server.URL+"/advance", nil)

	// From the removed row through the row that replaced it.
	post(t, server.URL+"/comment", map[string]any{
		"excerpt_index": 0,
		"start":         map[string]any{"side": "old", "line": 2},
		"end":           map[string]any{"side": "new", "line": 2},
		"note":          "this rename loses the plural",
	})

	var view daemon.ViewWire
	if err := json.Unmarshal([]byte(get(t, server.URL+"/view")), &view); err != nil {
		t.Fatalf("could not read the view: %v", err)
	}
	if len(view.Comments) != 1 {
		t.Fatalf("expected one Comment, got %d", len(view.Comments))
	}
	comment := view.Comments[0]
	want := []daemon.SegmentWire{
		{Side: "old", FirstLine: 2, LastLine: 2},
		{Side: "new", FirstLine: 2, LastLine: 2},
	}
	if len(comment.Segments) != len(want) {
		t.Fatalf("expected the Anchor to span both sides, got %+v", comment.Segments)
	}
	for i := range want {
		if comment.Segments[i] != want[i] {
			t.Errorf("segment %d: expected %+v, got %+v", i, want[i], comment.Segments[i])
		}
	}
	if comment.Location != "fetch.ts — before 2 — after 2" {
		t.Errorf("unexpected location %q", comment.Location)
	}
	// Both rows are marked, so the Reviewer sees the comment from either side.
	if !comment.Covers("fetch.ts", "old", 2) || !comment.Covers("fetch.ts", "new", 2) {
		t.Error("expected the Comment to cover its rows on both sides")
	}
}

func TestAnAnchorEndpointNamingNoRowIsRefused(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := editedRepo(t)
	postWalkthrough(t, server.URL, map[string]any{
		"brief": map[string]any{
			"ask": "rework the guard", "approach": "renamed it",
			"provenance": map[string]any{"kind": "stated", "citation": "session-1"},
		},
		"repositories": []any{map[string]any{"root": root, "base": "main"}},
		"steps": []any{map[string]any{
			"name": "Rework the guard", "explanation": "one line became another",
			"excerpts": []any{map[string]any{
				"repository": root, "file": "fetch.ts", "side": "new", "first_line": 1, "last_line": 3,
			}},
		}},
	})
	post(t, server.URL+"/advance", nil)

	// Line 3 exists on the after-side, but nothing removed a before-side line 3.
	status := postStatus(t, server.URL+"/anchor", map[string]any{
		"excerpt_index": 0,
		"start":         map[string]any{"side": "old", "line": 3},
		"end":           map[string]any{"side": "new", "line": 3},
	})

	if status != http.StatusConflict {
		t.Errorf("expected an endpoint naming no rendered row to be refused, got %d", status)
	}
}

func post(t *testing.T, url string, body map[string]any) {
	t.Helper()

	if status := postStatus(t, url, body); status != http.StatusOK {
		t.Fatalf("POST %s answered %d", url, status)
	}
}

func postStatus(t *testing.T, url string, body map[string]any) int {
	t.Helper()

	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	response, err := http.Post(url, "application/json", reader)
	if err != nil {
		t.Fatalf("could not POST %s: %v", url, err)
	}
	defer response.Body.Close()
	return response.StatusCode
}

// A selection in an expanded Acknowledgement names the Acknowledgement as well as
// the Excerpt, so the daemon resolves it against the expansion the TUI drew and
// the Anchor says it disputes a "mechanical" claim.
func TestACommentCanBeRaisedInAcknowledgedCode(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := editedRepo(t)
	postWalkthrough(t, server.URL, map[string]any{
		"brief": map[string]any{
			"ask": "rework the guard", "approach": "renamed it",
			"provenance": map[string]any{"kind": "stated", "citation": "session-1"},
		},
		"repositories": []any{map[string]any{"root": root, "base": "main"}},
		"steps": []any{map[string]any{
			"name": "Mechanical", "explanation": "a rename, nothing to read",
			"acknowledgements": []any{map[string]any{
				"repository": root, "files": []any{"fetch.ts"}, "reason": "renamed by the IDE",
			}},
		}},
	})
	post(t, server.URL+"/advance", nil)

	post(t, server.URL+"/comment", map[string]any{
		"acknowledgement_index": 0,
		"excerpt_index":         0,
		"start":                 map[string]any{"side": "old", "line": 2},
		"end":                   map[string]any{"side": "new", "line": 2},
		"note":                  "this is not mechanical",
	})

	var view daemon.ViewWire
	if err := json.Unmarshal([]byte(get(t, server.URL+"/view")), &view); err != nil {
		t.Fatalf("could not read the view: %v", err)
	}
	if len(view.Comments) != 1 {
		t.Fatalf("expected one Comment, got %d", len(view.Comments))
	}
	comment := view.Comments[0]
	if comment.Location != "fetch.ts — before 2 — after 2" {
		t.Errorf("unexpected location %q", comment.Location)
	}
	if comment.Acknowledgement == nil || *comment.Acknowledgement != 0 {
		t.Errorf("expected the Comment to name Acknowledgement 0, got %v", comment.Acknowledgement)
	}
	if !strings.Contains(comment.Anchor, `acknowledged in Step "Mechanical" (renamed by the IDE)`) {
		t.Errorf("expected the Anchor to name the Acknowledgement it disputes:\n%s", comment.Anchor)
	}
}

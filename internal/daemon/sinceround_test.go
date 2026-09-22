package daemon_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

type roundView struct {
	Round              int  `json:"round"`
	PreviousRound      int  `json:"previous_round"`
	SincePreviousRound bool `json:"since_previous_round"`
	Withdrawn          []struct {
		File  string   `json:"file"`
		After int      `json:"after"`
		Lines []string `json:"lines"`
	} `json:"withdrawn"`
	UnchangedSincePrevious []bool `json:"unchanged_since_previous"`
	Step                   *struct {
		Excerpts []struct {
			Lines []struct {
				Number  int    `json:"number"`
				Text    string `json:"text"`
				Side    string `json:"side"`
				Changed bool   `json:"changed"`
			} `json:"lines"`
		} `json:"excerpts"`
	} `json:"step"`
}

func getRoundView(t *testing.T, baseURL string) roundView {
	t.Helper()
	response, err := http.Get(baseURL + "/view")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var view roundView
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	return view
}

// TestARevisionRoundIsShadedByWhatMovedThroughTheDaemon drives two real rounds:
// the agent rewrites one line between them, and the second round shows that
// line against the first round's text, with a toggle back to the merge-base.
func TestARevisionRoundIsShadedByWhatMovedThroughTheDaemon(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t) // fetch.ts gains line 4 "ADDED"; LOCKFILE is new
	walkthrough := map[string]any{
		"brief":        map[string]any{"goal": "x", "approach": "y"},
		"repositories": []any{map[string]any{"root": root, "base": "main"}},
		"steps": []any{map[string]any{
			"name": "all", "explanation": "e",
			"excerpts":         []any{map[string]any{"file": "FILE-src/fetch.ts", "side": "new", "first_line": 1, "last_line": 4}},
			"acknowledgements": []any{map[string]any{"files": []any{"LOCKFILE"}, "reason": "generated"}},
		}},
	}
	postRound(t, server.URL, walkthrough)
	// A Comment keeps the review going into a Revision Round: a hand-off with
	// nothing raised would end it.
	httpPost(t, server.URL+"/goto/1")
	raiseComment(t, server.URL, 0, 4, 4, "rename this")
	httpPost(t, server.URL+"/finish")
	if err := os.WriteFile(filepath.Join(root, "FILE-src/fetch.ts"), []byte("a\nb\nc\nREWRITTEN\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	walkthrough["dispositions"] = []any{map[string]any{"comment_id": 1, "status": "addressed"}}
	postRound(t, server.URL, walkthrough)
	httpPost(t, server.URL+"/goto/1")

	since := getRoundView(t, server.URL)
	httpPost(t, server.URL+"/since-previous")
	all := getRoundView(t, server.URL)

	if since.Round != 2 || since.PreviousRound != 1 || !since.SincePreviousRound {
		t.Fatalf("expected round 2 shaded since round 1, got %+v", since)
	}
	lines := since.Step.Excerpts[0].Lines
	if len(lines) != 5 || lines[3].Side != "previous" || lines[3].Text != "ADDED" || lines[4].Text != "REWRITTEN" || !lines[4].Changed || lines[0].Changed {
		t.Errorf("expected ADDED from round 1 drawn above REWRITTEN, and nothing else shaded, got %+v", lines)
	}
	if len(since.UnchangedSincePrevious) != 1 || since.UnchangedSincePrevious[0] {
		t.Errorf("expected the one Step marked as changed, got %v", since.UnchangedSincePrevious)
	}
	if all.SincePreviousRound || len(all.Step.Excerpts[0].Lines) != 4 {
		t.Errorf("expected the toggle to show all changes against the merge-base, got %+v", all)
	}
}

package daemon_test

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

type describedFile struct {
	Path          string   `json:"path"`
	Status        string   `json:"status"`
	From          string   `json:"from"`
	NewRanges     [][2]int `json:"new_ranges"`
	OldRanges     [][2]int `json:"old_ranges"`
	Modifications []struct {
		Old [2]int `json:"old"`
		New [2]int `json:"new"`
	} `json:"modifications"`
}

type described struct {
	Repositories []struct {
		Root      string          `json:"root"`
		MergeBase string          `json:"merge_base"`
		Files     []describedFile `json:"files"`
	} `json:"repositories"`
	RevisionRound bool          `json:"revision_round"`
	StillToCover  *int          `json:"still_to_cover"`
	Problems      []problemJSON `json:"problems"`
}

func TestDescribeChangesReportsTheChangeSetOverMCP(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)

	got := decodeResult[described](t, callTool(t, server.URL, "describe_changes", map[string]any{
		"repositories": []any{map[string]any{"root": root, "base": "main"}},
	}))

	if len(got.Problems) != 0 || len(got.Repositories) != 1 {
		t.Fatalf("expected one repository described, got %+v", got)
	}
	repository := got.Repositories[0]
	if repository.Root != root || len(repository.MergeBase) != 40 {
		t.Errorf("expected the root and its resolved merge-base, got %q %q", repository.Root, repository.MergeBase)
	}
	want := []describedFile{
		{Path: "FILE-src/fetch.ts", Status: "modified", NewRanges: [][2]int{{4, 4}}},
		{Path: "LOCKFILE", Status: "added", NewRanges: [][2]int{{1, 3}}},
	}
	if !reflect.DeepEqual(repository.Files, want) {
		t.Errorf("expected %+v, got %+v", want, repository.Files)
	}
	if got.RevisionRound || got.StillToCover != nil {
		t.Errorf("a first round carries no pre-marking, got %+v", got)
	}
}

func TestDescribeChangesLeavesAReviewUnderWayAlone(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	postWalkthrough(t, server.URL, map[string]any{
		"brief":        map[string]any{"ask": "x", "approach": "y", "provenance": map[string]any{"kind": "stated", "citation": "s"}},
		"repositories": []any{map[string]any{"root": root, "base": "main"}},
		"steps": []any{map[string]any{
			"name": "all", "explanation": "e",
			"excerpts":         []any{map[string]any{"file": "FILE-src/fetch.ts", "side": "new", "first_line": 1, "last_line": 4}},
			"acknowledgements": []any{map[string]any{"files": []any{"LOCKFILE"}, "reason": "generated"}},
		}},
	})
	httpPost(t, server.URL+"/goto/1")
	raiseComment(t, server.URL, 0, 4, 4, "a point")
	before := get(t, server.URL+"/view")

	callTool(t, server.URL, "describe_changes", map[string]any{
		"repositories": []any{map[string]any{"root": root, "base": "main"}},
	})

	if after := get(t, server.URL+"/view"); after != before {
		t.Errorf("describe_changes must not change the review:\nbefore %s\nafter  %s", before, after)
	}
}

func TestDescribeChangesReportsABadBaseAsAProblem(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)

	got := decodeResult[described](t, callTool(t, server.URL, "describe_changes", map[string]any{
		"repositories": []any{map[string]any{"root": root, "base": "no-such-branch"}},
	}))

	if len(got.Problems) != 1 || got.Problems[0].Reason != "derivation_failed" {
		t.Errorf("expected a derivation_failed problem, got %+v", got)
	}
}

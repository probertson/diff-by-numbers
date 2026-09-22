package daemon_test

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

type fullRow struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	State        string `json:"state"`
	Repositories []struct {
		Name   string `json:"name"`
		Branch string `json:"branch"`
	} `json:"repositories"`
	Round     int       `json:"round"`
	Position  int       `json:"position"`
	StepCount int       `json:"step_count"`
	Comments  int       `json:"comments"`
	PostedAt  time.Time `json:"posted_at"`
}

func fullInbox(t *testing.T, baseURL string) []fullRow {
	t.Helper()
	var listed struct {
		Reviews []fullRow `json:"reviews"`
	}
	if err := json.Unmarshal([]byte(get(t, baseURL+"/inbox")), &listed); err != nil {
		t.Fatalf("decode /inbox: %v", err)
	}
	return listed.Reviews
}

// A row carries enough to choose a Review without opening it.
func TestAnInboxRowCarriesWhatTheReviewerChoosesBy(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	before := time.Now()
	posted := postRound(t, server.URL, labelledRound(root, "LABEL-auth"))
	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/goto/1")
	raiseComment(t, reviewURL(server.URL, posted.ReviewID), 0, 4, 4, "a point")

	row := fullInbox(t, server.URL)[0]

	if len(row.Repositories) != 1 || row.Repositories[0].Name != filepath.Base(root) {
		t.Errorf("expected the repository named by its basename, got %+v", row.Repositories)
	}
	if row.Repositories[0].Branch != "feature" {
		t.Errorf("expected the branch the work is on, got %q", row.Repositories[0].Branch)
	}
	if row.Round != 1 || row.StepCount != 2 || row.Position != 1 {
		t.Errorf("expected round 1, Step 1 of 2, got round %d, Step %d of %d", row.Round, row.Position, row.StepCount)
	}
	if row.Comments != 1 {
		t.Errorf("expected the Comment raised to be counted, got %d", row.Comments)
	}
	if row.PostedAt.Before(before) || row.PostedAt.After(time.Now()) {
		t.Errorf("expected the time of the last post, got %v", row.PostedAt)
	}
}

func TestARevisionRoundRowCountsItsRound(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	reviewID := handedOffWithAComment(t, server.URL, root, "GOAL-first")

	postRound(t, server.URL, revisionWithBrief(root, reviewID, map[string]any{"approach": "renamed it"}))

	row := fullInbox(t, server.URL)[0]
	if row.Round != 2 {
		t.Errorf("a Revision Round is round 2, got %d", row.Round)
	}
}

// What needs the Reviewer comes first: a review waiting on its agent is the one
// they can do nothing about.
func TestTheInboxPutsWhatNeedsTheReviewerFirst(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	waiting := handedOffWithAComment(t, server.URL, root, "GOAL-handed-off")
	older := postRound(t, server.URL, labelledRound(root, "LABEL-older"))
	newer := postRound(t, server.URL, labelledRound(root, "LABEL-newer"))

	rows := fullInbox(t, server.URL)

	var order []string
	for _, row := range rows {
		order = append(order, row.ID)
	}
	want := []string{older.ReviewID, newer.ReviewID, waiting}
	if len(order) != 3 || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] {
		t.Errorf("expected what needs the Reviewer first, oldest first within the group: %v, got %v", want, order)
	}
}

// Oldest first means least recently posted, which is what the row's age counts
// from: a review whose agent just posted again is the freshest thing there.
func TestAReviewPostedToAgainGoesBelowOnesLeftLonger(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	first := handedOffWithAComment(t, server.URL, root, "GOAL-first")
	second := postRound(t, server.URL, labelledRound(root, "LABEL-second"))

	postRound(t, server.URL, revisionWithBrief(root, first, map[string]any{"approach": "renamed it"}))

	rows := fullInbox(t, server.URL)
	if len(rows) != 2 || rows[0].ID != second.ReviewID || rows[1].ID != first {
		t.Errorf("expected the review nobody has posted to since first, got %+v", rows)
	}
}

// A row names each repository's branch, which is how the Reviewer tells whose
// work it is when several are under review at once.
func TestARowNamesEveryRepositoryUnderReview(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	portal, api := featureRepo(t), featureRepo(t)
	round := labelledRound(portal, "LABEL-two-repos")
	round["repositories"] = []any{
		map[string]any{"root": portal, "base": "main"},
		map[string]any{"root": api, "base": "main"},
	}
	round["steps"] = []any{map[string]any{
		"name": "both", "explanation": "e",
		"excerpts": []any{
			map[string]any{"repository": portal, "file": "FILE-src/fetch.ts", "side": "new", "first_line": 1, "last_line": 4},
			map[string]any{"repository": api, "file": "FILE-src/fetch.ts", "side": "new", "first_line": 1, "last_line": 4},
		},
		"acknowledgements": []any{
			map[string]any{"repository": portal, "files": []any{"LOCKFILE"}, "reason": "generated"},
			map[string]any{"repository": api, "files": []any{"LOCKFILE"}, "reason": "generated"},
		},
	}}

	postRound(t, server.URL, round)

	row := fullInbox(t, server.URL)[0]
	if len(row.Repositories) != 2 {
		t.Fatalf("expected both repositories named, got %+v", row.Repositories)
	}
	for _, repository := range row.Repositories {
		if repository.Branch != "feature" {
			t.Errorf("expected each repository's branch, got %+v", repository)
		}
	}
}

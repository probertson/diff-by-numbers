package daemon_test

import (
	"html"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// assertShows fails for each text the page leaves out.
func assertShows(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("expected the page to show %q, got:\n%s", want, out)
		}
	}
}

// On the Overview the page shows what the TUI's Overview does: the Brief, what
// is under review and the Steps by name. It keeps following: the next advance
// brings a Step back.
func TestOnTheOverviewThePageShowsTheBriefAndTheSteps(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	t.Cleanup(server.Close)
	root := featureRepo(t)
	posted := postRound(t, server.URL, roundWithRepository(root, map[string]any{"root": root, "base": "main"}))
	events := follow(t, server.URL, posted.ReviewID)

	out := html.UnescapeString(stop(t, server.URL, posted.ReviewID))

	assertShows(t, out, "LABEL-the-review", "GOAL-tenant-scoping", "BACKGROUND-the-id-is-dropped-today",
		"Under review", root, "base: main", "NAME-the-change")
	for _, unwanted := range []string{"EXPLANATION-what-it-does", "On the Overview", "In your browser"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("expected the Overview without %q, got:\n%s", unwanted, out)
		}
	}

	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/advance")

	if got := nextEvent(events); got != "redraw" {
		t.Fatalf("expected a redraw event after the advance, got %q", got)
	}
	if out := stop(t, server.URL, posted.ReviewID); !strings.Contains(out, "EXPLANATION-what-it-does") {
		t.Errorf("expected the advance to bring the Step back, got:\n%s", out)
	}
}

// The Round's own questions are shown with the Brief, with their Answers. A
// Step's question is not, since it needs the Steps before it; its Step is
// marked as asking one.
func TestTheOverviewPageShowsTheRoundsQuestionsAndMarksTheStepsThatAsk(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	t.Cleanup(server.Close)
	round := askingOnStep1(featureRepo(t), "QUESTION-on-the-step")
	round["questions"] = []any{map[string]any{"text": "QUESTION-on-the-round"}}
	posted := postRound(t, server.URL, round)
	answer(t, reviewURL(server.URL, posted.ReviewID), 1, "ANSWER-to-the-round")

	out := html.UnescapeString(stop(t, server.URL, posted.ReviewID))

	assertShows(t, out, "QUESTION-on-the-round", "ANSWER-to-the-round", "asks you a question")
	if strings.Contains(out, "QUESTION-on-the-step") {
		t.Errorf("expected the Step's question left to its Step, got:\n%s", out)
	}
}

// A Round in Chapters lists its Chapters in place of its Steps, as the TUI's
// Overview does.
func TestTheOverviewPageListsARoundsChapters(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	t.Cleanup(server.Close)
	posted := postRound(t, server.URL, chapteredRound(featureRepo(t)))

	out := html.UnescapeString(stop(t, server.URL, posted.ReviewID))

	assertShows(t, out, "Chapters", "NAME-the-chapter", "Step 1")
}

// A Revision Round's Overview says what became of the last round: each
// Comment under what the agent did with it, with what the Reviewer asked and
// what the agent said, and each Agent Question with its Answer and the
// agent's response.
func TestARevisionRoundsOverviewPageShowsWhatBecameOfTheLastRound(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	t.Cleanup(server.Close)
	root := featureRepo(t)
	first := postRound(t, server.URL, askingOnStep1(root, "QUESTION-3-or-5"))
	review := reviewURL(server.URL, first.ReviewID)
	answer(t, review, 1, "ANSWER-5")
	httpPost(t, review+"/goto/1")
	raiseComment(t, review, 0, 4, 4, "NOTE-please-rename-this")
	httpPost(t, review+"/finish")
	revision := minimalRound(root)
	delete(revision, "label")
	revision["revises"] = first.ReviewID
	revision["dispositions"] = []any{map[string]any{"comment_id": 1, "status": "declined", "response": "RESPONSE-the-name-is-deliberate"}}
	revision["question_statuses"] = []any{map[string]any{"question_id": 1, "status": "addressed", "response": "RESPONSE-raised-it-to-5"}}
	postRound(t, server.URL, revision)

	out := html.UnescapeString(stop(t, server.URL, first.ReviewID))

	assertShows(t, out,
		"Since the last round", "Declined (1)", "NOTE-please-rename-this", "RESPONSE-the-name-is-deliberate",
		"Agent Questions from the last round", "Addressed (1)", "QUESTION-3-or-5", "ANSWER-5", "RESPONSE-raised-it-to-5")
	if strings.Contains(out, "re-raise one") {
		t.Errorf("expected no key hints on the page, got:\n%s", out)
	}
}

// What the last round showed and this one removed outright is listed, since
// nothing else shows it.
func TestARevisionRoundsOverviewPageListsWhatWasWithdrawn(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	t.Cleanup(server.Close)
	root := featureRepo(t) // fetch.ts gains line 4 "ADDED"; LOCKFILE is new
	first := postRound(t, server.URL, roundWithRepository(root, map[string]any{"root": root, "base": "main"}))
	review := reviewURL(server.URL, first.ReviewID)
	httpPost(t, review+"/goto/1")
	raiseComment(t, review, 0, 4, 4, "drop this")
	httpPost(t, review+"/finish")
	if err := os.WriteFile(filepath.Join(root, "FILE-src/fetch.ts"), []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	postRound(t, server.URL, map[string]any{
		"revises": first.ReviewID,
		"brief":   map[string]any{"goal": "x"}, "repositories": []any{map[string]any{"root": root, "base": "main"}},
		"steps": []any{map[string]any{
			"name": "The lockfile", "explanation": "e",
			"acknowledgements": []any{map[string]any{"files": []any{"LOCKFILE"}, "reason": "generated"}},
		}},
		"dispositions": []any{map[string]any{"comment_id": 1, "status": "addressed"}},
	})

	out := html.UnescapeString(stop(t, server.URL, first.ReviewID))

	assertShows(t, out, "Withdrawn since round 1", "FILE-src/fetch.ts", "ADDED")
}

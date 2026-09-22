package daemon_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// A Round is what the agent posts, so that is what the tool is called. The old
// name goes rather than lingering as an alias, so an agent reading a stale
// skill finds out at once instead of learning two words for one thing.
func TestTheAgentPostsARound(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)

	outcome := decodeResult[postOutcome](t, callTool(t, server.URL, "post_round",
		roundWithRepository(root, map[string]any{"root": root, "base": "main"})))

	if !outcome.Accepted {
		t.Fatalf("post_round should accept a valid Round, got %s", outcome.summary())
	}
}

func TestThereIsNoPostWalkthroughTool(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("could not connect to the daemon: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)

	if err != nil {
		t.Fatalf("could not list tools: %v", err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "post_walkthrough" {
			t.Error("post_walkthrough should be gone; the tool is post_round")
		}
	}
}

// The Brief's first half is the Goal; the old `ask` field is gone. Like `range`
// before it, it is refused by name so an agent on a stale skill is told what to fix.
func TestTheWireRejectsTheOldAskFieldByName(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	round := roundWithRepository(root, map[string]any{"root": root, "base": "main"})
	brief := round["brief"].(map[string]any)
	brief["ask"] = brief["goal"]
	delete(brief, "goal")

	result := callTool(t, server.URL, "post_round", round)

	if !result.IsError {
		t.Fatal("the old `ask` field is gone and must not quietly work")
	}
	if text := resultText(t, result); !strings.Contains(text, `["ask"]`) {
		t.Errorf("the rejection should name the field that is no longer there, got %q", text)
	}
}

func TestFetchResultsHandsBackTheGoal(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	posted := postRound(t, server.URL, roundWithRepository(root, map[string]any{"root": root, "base": "main"}))

	results := decodeResult[struct {
		Goal string `json:"goal"`
	}](t, callTool(t, server.URL, "fetch_results", map[string]any{"review_id": posted.ReviewID}))

	if results.Goal != "GOAL-tenant-scoping" {
		t.Errorf("fetch_results should hand back the Goal to re-ground the agent, got %q", results.Goal)
	}
}

// Provenance is gone: dbn could never check it, and the Reviewer asks the agent
// that wrote the work to post it. An agent still sending it is told so by name.
func TestTheWireRejectsProvenanceByName(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	round := roundWithRepository(root, map[string]any{"root": root, "base": "main"})
	round["brief"].(map[string]any)["provenance"] = map[string]any{"kind": "inferred"}

	result := callTool(t, server.URL, "post_round", round)

	if !result.IsError {
		t.Fatal("the old `provenance` field is gone and must not quietly work")
	}
	if text := resultText(t, result); !strings.Contains(text, `["provenance"]`) {
		t.Errorf("the rejection should name the field that is no longer there, got %q", text)
	}
}

func viewedGoal(t *testing.T, baseURL, reviewID string) string {
	t.Helper()
	var view struct {
		Brief struct {
			Goal string `json:"goal"`
		} `json:"brief"`
	}
	if err := json.Unmarshal([]byte(get(t, reviewURL(baseURL, reviewID)+"/view")), &view); err != nil {
		t.Fatalf("decode /view: %v", err)
	}
	return view.Brief.Goal
}

func fetchedGoal(t *testing.T, baseURL, reviewID string) string {
	t.Helper()
	return decodeResult[struct {
		Goal string `json:"goal"`
	}](t, callTool(t, baseURL, "fetch_results", map[string]any{"review_id": reviewID})).Goal
}

// handedOffWithAComment posts round 1 with the given Goal, raises one Comment and
// hands off, leaving the Review ready for a Revision Round. It returns the id.
func handedOffWithAComment(t *testing.T, baseURL, root, goal string) string {
	t.Helper()
	round := minimalRound(root)
	round["brief"].(map[string]any)["goal"] = goal
	posted := postRound(t, baseURL, round)
	httpPost(t, reviewURL(baseURL, posted.ReviewID)+"/goto/1")
	raiseComment(t, reviewURL(baseURL, posted.ReviewID), 0, 4, 4, "please rename this")
	httpPost(t, reviewURL(baseURL, posted.ReviewID)+"/finish")
	return posted.ReviewID
}

// revisionWithBrief is a Revision Round of reviewID, addressing that one Comment.
func revisionWithBrief(root, reviewID string, brief map[string]any) map[string]any {
	round := minimalRound(root)
	round["brief"] = brief
	round["revises"] = reviewID
	round["dispositions"] = []any{map[string]any{"comment_id": 1, "status": "addressed"}}
	return round
}

// The Goal belongs to the Review, not the Round: given once in round 1 and
// carried forward, so a Revision Round need not restate it.
func TestARevisionRoundCarriesTheGoalForward(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	reviewID := handedOffWithAComment(t, server.URL, root, "GOAL-first")

	postRound(t, server.URL, revisionWithBrief(root, reviewID, map[string]any{"approach": "renamed it"}))

	if goal := viewedGoal(t, server.URL, reviewID); goal != "GOAL-first" {
		t.Errorf("the Revision Round's Overview should carry round 1's Goal, got %q", goal)
	}
	if goal := fetchedGoal(t, server.URL, reviewID); goal != "GOAL-first" {
		t.Errorf("fetch_results should hand back round 1's Goal, got %q", goal)
	}
}

// When what the Reviewer wants has changed, a Revision Round restates the Goal,
// and the restated one is what every later Round is judged against.
func TestARevisionRoundMayRestateTheGoal(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	reviewID := handedOffWithAComment(t, server.URL, root, "GOAL-first")

	postRound(t, server.URL, revisionWithBrief(root, reviewID, map[string]any{"goal": "GOAL-changed", "approach": "renamed it"}))

	if goal := viewedGoal(t, server.URL, reviewID); goal != "GOAL-changed" {
		t.Errorf("a restated Goal should replace the old one, got %q", goal)
	}
}

func TestAReplacementMayLeaveTheGoalOut(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	round := minimalRound(root)
	round["brief"].(map[string]any)["goal"] = "GOAL-first"
	posted := postRound(t, server.URL, round)
	replacement := minimalRound(root)
	replacement["brief"] = map[string]any{"approach": "a better plan"}
	replacement["replaces"] = posted.ReviewID

	postRound(t, server.URL, replacement)

	if goal := viewedGoal(t, server.URL, posted.ReviewID); goal != "GOAL-first" {
		t.Errorf("a Replacement without a Goal should keep the Review's, got %q", goal)
	}
}

// Round 1 is where the Goal is given, so it cannot be left out there.
func TestRoundOneMustGiveTheGoal(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	round := minimalRound(root)
	round["brief"] = map[string]any{"approach": "approach"}

	outcome := decodeResult[postOutcome](t, callTool(t, server.URL, "post_round", round))

	if outcome.Accepted || !outcome.has("malformed_brief") {
		t.Errorf("round 1 without a Goal should be refused as a malformed Brief, got accepted=%v %s", outcome.Accepted, outcome.summary())
	}
}

// Rejection reasons speak of Rounds too, since they are values an agent acts on.
func TestReplacingAHandedOffRoundIsRefusedAsHandedOff(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	reviewID := handedOffWithAComment(t, server.URL, root, "goal")

	replacement := minimalRound(root)
	replacement["replaces"] = reviewID

	outcome := decodeResult[postOutcome](t, callTool(t, server.URL, "post_round", replacement))

	if outcome.Accepted || !outcome.has("round_handed_off") {
		t.Errorf("replacing a handed-off Round should be refused as round_handed_off, got %s", outcome.summary())
	}
}

// A window whose Review has been released — or that names one dbn never held —
// is told so plainly, which is what sends it back to the Inbox.
func TestNavigatingAReviewTheDaemonDoesNotHoldIsRefused(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()

	response, err := http.Post(reviewURL(server.URL, "no-such-id")+"/advance", "text/plain", nil)

	if err != nil {
		t.Fatalf("POST advance: %v", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for a review dbn is not holding, got %s", response.Status)
	}
	if !strings.Contains(string(body), "no-such-id") {
		t.Errorf("the refusal should name the id, got %s", body)
	}
}

// What the agent reads about the tools speaks the glossary: a Round, never a
// Walkthrough.
func TestNoToolSpeaksOfAWalkthrough(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("could not connect to the daemon: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)

	if err != nil {
		t.Fatalf("could not list tools: %v", err)
	}
	for _, tool := range tools.Tools {
		described, err := json.Marshal(tool)
		if err != nil {
			t.Fatalf("could not encode %s: %v", tool.Name, err)
		}
		if strings.Contains(strings.ToLower(string(described)), "walkthrough") {
			t.Errorf("%s still speaks of a Walkthrough:\n%s", tool.Name, described)
		}
	}
}

func TestFetchResultsSpeaksOfRoundsNotWalkthroughs(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	posted := postRound(t, server.URL, minimalRound(root))

	message := fetchResults(t, server.URL, posted.ReviewID).Message

	if strings.Contains(message, "Walkthrough") || !strings.Contains(message, "Round") {
		t.Errorf("the advisory should speak of a Round, got %q", message)
	}
}

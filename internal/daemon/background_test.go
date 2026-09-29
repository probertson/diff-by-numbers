package daemon_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// The old `approach` field was renamed `background`, and removed rather than
// kept as an alias: a post still sending it is refused, in any round, naming it.
func TestAPostStillSendingApproachIsRefused(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	roundOne := minimalRound(root)
	roundOne["brief"] = map[string]any{"goal": "goal", "approach": "what I did"}
	reviewID := handedOffWithAComment(t, server.URL, root, "goal")
	revision := revisionWithBrief(root, reviewID, map[string]any{"approach": "what I did"})

	first := callTool(t, server.URL, "post_round", roundOne)
	revised := callTool(t, server.URL, "post_round", revision)

	for name, result := range map[string]*mcp.CallToolResult{"round 1": first, "a Revision Round": revised} {
		if !result.IsError {
			t.Errorf("%s sending the old `approach` field should be refused, not quietly work", name)
		}
		if text := resultText(t, result); !strings.Contains(text, `["approach"]`) {
			t.Errorf("%s: the rejection should name the field that is no longer there, got %q", name, text)
		}
	}
}

// Round 1 sets the stage, so it cannot leave the Background out.
func TestRoundOneMustGiveTheBackground(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	round := minimalRound(root)
	round["brief"] = map[string]any{"goal": "goal"}

	outcome := decodeResult[postOutcome](t, callTool(t, server.URL, "post_round", round))

	if outcome.Accepted || !outcome.has("malformed_brief") {
		t.Errorf("round 1 without a Background should be refused as a malformed Brief, got accepted=%v %s", outcome.Accepted, outcome.summary())
	}
}

// A Replacement of round 1 is still round 1: it sets the stage afresh.
func TestAReplacementOfRoundOneMustGiveTheBackground(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	posted := postRound(t, server.URL, minimalRound(root))
	replacement := minimalRound(root)
	replacement["brief"] = map[string]any{}
	replacement["replaces"] = posted.ReviewID

	outcome := decodeResult[postOutcome](t, callTool(t, server.URL, "post_round", replacement))

	if outcome.Accepted || !outcome.has("malformed_brief") {
		t.Errorf("a Replacement of round 1 without a Background should be refused as a malformed Brief, got accepted=%v %s", outcome.Accepted, outcome.summary())
	}
}

// A Revision Round gives a Background only when there is new stage-setting
// worth reading. Leaving it out shows none, rather than round 1's again: old
// stage-setting should not read like current information.
func TestARevisionRoundMayLeaveTheBackgroundOut(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	reviewID := handedOffWithAComment(t, server.URL, root, "goal")

	postRound(t, server.URL, revisionWithBrief(root, reviewID, map[string]any{}))

	if background := viewedBrief(t, server.URL, reviewID).Background; background != "" {
		t.Errorf("the Revision Round's Overview should show no Background, got %q", background)
	}
	if background := fetchedBrief(t, server.URL, reviewID).Background; background != "" {
		t.Errorf("fetch_results should report no Background for this round, got %q", background)
	}
}

// A Replacement of a Revision Round is still a later round: the Background is
// optional, and one the replaced round gave does not carry over.
func TestAReplacementOfARevisionRoundMayLeaveTheBackgroundOut(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	reviewID := handedOffWithAComment(t, server.URL, root, "goal")
	postRound(t, server.URL, revisionWithBrief(root, reviewID, map[string]any{"background": "BACKGROUND-replaced"}))
	replacement := revisionWithBrief(root, reviewID, map[string]any{})
	delete(replacement, "revises")
	replacement["replaces"] = reviewID

	postRound(t, server.URL, replacement)

	if background := viewedBrief(t, server.URL, reviewID).Background; background != "" {
		t.Errorf("the replaced round's Background should not carry over, got %q", background)
	}
}

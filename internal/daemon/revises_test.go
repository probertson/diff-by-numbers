package daemon_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// A Revision Round names the Review it continues. Nothing is implicit: with
// several Reviews open, "the" Review means nothing (ADR-0015).
func TestARevisionRoundNamesTheReviewItRevises(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	reviewID := handedOffWithAComment(t, server.URL, root, "GOAL-first")

	posted := postRound(t, server.URL, revisionWithBrief(root, reviewID, map[string]any{"approach": "renamed it"}))

	if posted.ReviewID != reviewID {
		t.Errorf("a Revision Round continues the Review it names, got id %q for %q", posted.ReviewID, reviewID)
	}
	if !strings.HasPrefix(posted.Message, "The Revision Round is posted.") {
		t.Errorf("expected the post announced as a Revision Round, got %q", posted.Message)
	}
}

// Without `revises`, a post after a Hand Off is new work, not a Revision Round
// of whatever the daemon happened to be holding.
func TestAPostAfterAHandOffWithoutRevisesIsNotARevisionRound(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	handedOffWithAComment(t, server.URL, root, "GOAL-first")

	outcome := decodeResult[postOutcome](t, callTool(t, server.URL, "post_round", minimalRound(root)))

	if outcome.Accepted {
		t.Fatalf("a post naming no Review should not become a Revision Round of the open one, got id %q", outcome.ReviewID)
	}
	if !outcome.has("walkthrough_active") {
		t.Errorf("expected the open Review to be named as the reason, got %s", outcome.summary())
	}
}

func TestRevisesNamingAReviewStillUnderReviewIsRefused(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	posted := postRound(t, server.URL, minimalRound(root))
	revision := minimalRound(root)
	revision["revises"] = posted.ReviewID

	outcome := decodeResult[postOutcome](t, callTool(t, server.URL, "post_round", revision))

	if outcome.Accepted || !outcome.has("walkthrough_active") {
		t.Errorf("a Review the Reviewer has not handed off takes no Revision Round, got accepted=%v %s", outcome.Accepted, outcome.summary())
	}
}

func TestRevisesNamingAnUnknownReviewIsRefused(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	handedOffWithAComment(t, server.URL, root, "GOAL-first")

	revision := revisionWithBrief(root, "no-such-id", map[string]any{"approach": "renamed it"})
	outcome := decodeResult[postOutcome](t, callTool(t, server.URL, "post_round", revision))

	if outcome.Accepted || !outcome.has("unknown_review") {
		t.Errorf("expected an unknown id to be refused as unknown_review, got accepted=%v %s", outcome.Accepted, outcome.summary())
	}
}

// A Review handed off with nothing raised is concluded, so it takes no
// Revision Round: that work is a new Review.
func TestRevisesNamingAConcludedReviewIsRefused(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	posted := postRound(t, server.URL, minimalRound(root))
	httpPost(t, server.URL+"/finish")
	revision := minimalRound(root)
	revision["revises"] = posted.ReviewID

	outcome := decodeResult[postOutcome](t, callTool(t, server.URL, "post_round", revision))

	if outcome.Accepted || !outcome.has("review_over") {
		t.Errorf("a concluded Review takes no Revision Round, got accepted=%v %s", outcome.Accepted, outcome.summary())
	}
}

// A label is what the Reviewer tells several open Reviews apart by, so a new
// Review cannot go without one.
func TestANewReviewNeedsALabel(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	unlabelled := minimalRound(root)
	delete(unlabelled, "label")

	outcome := decodeResult[postOutcome](t, callTool(t, server.URL, "post_round", unlabelled))

	if outcome.Accepted || !outcome.has("missing_label") {
		t.Errorf("expected a new review without a label to be refused, got accepted=%v %s", outcome.Accepted, outcome.summary())
	}
}

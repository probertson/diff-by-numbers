package tui

import (
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// When the agent replaces the Round under review, the Reviewer is put back
// at the Overview and told why, and what became of their Comments.

func carried(n int) []daemon.CommentWire {
	var out []daemon.CommentWire
	for i := 1; i <= n; i++ {
		out = append(out, daemon.CommentWire{ID: i, CarriedOver: true, Location: "app.ts — after 2"})
	}
	return out
}

func replacedBy(m model, comments []daemon.CommentWire) model {
	after, _ := m.Update(refreshMsg{view: &daemon.ViewWire{
		Posted: true, Posting: 2, Replaced: true, Position: 0, StepCount: 2, Comments: comments,
	}})
	return after.(model)
}

func TestAReplacementIsAnnouncedWithTheCommentsItCarriedOver(t *testing.T) {
	m := arrive(model{mode: modeReview}, 1, 2, oneLineStep("before"))

	m = replacedBy(m, carried(3))

	if got := m.notice(); got != "The agent replaced this Round — 3 Comments carried over (l to review them)" {
		t.Errorf("unexpected notice: %q", got)
	}
}

func TestAReplacementWithNoCommentsIsStillAnnounced(t *testing.T) {
	m := arrive(model{mode: modeReview}, 1, 2, oneLineStep("before"))

	m = replacedBy(m, nil)

	if got := m.notice(); got != "The agent replaced this Round" {
		t.Errorf("unexpected notice: %q", got)
	}
}

func TestTheReplacementNoticeClearsOnTheNextNavigation(t *testing.T) {
	m := replacedBy(arrive(model{mode: modeReview}, 1, 2, oneLineStep("before")), carried(1))

	m = arrive(m, 2, 1, oneLineStep("after"))

	if got := m.notice(); got != "" {
		t.Errorf("expected navigating to clear the notice, got %q", got)
	}
}

func TestAnOrdinaryNewPostingIsNotAnnouncedAsAReplacement(t *testing.T) {
	m := arrive(model{mode: modeReview}, 1, 2, oneLineStep("before"))

	m = arrive(m, 2, 0, nil)

	if got := m.notice(); got != "" {
		t.Errorf("expected no notice for a posting that replaced nothing, got %q", got)
	}
}

func TestAReplacementReturnsTheReviewerFromTheEndOfTheReview(t *testing.T) {
	m := arrive(model{mode: modeReview}, 1, 2, oneLineStep("before"))
	m.mode = modeConclusion

	m = replacedBy(m, nil)

	if m.mode != modeReview {
		t.Errorf("expected the Reviewer back walking the new Round, got mode %v", m.mode)
	}
}

func TestACarriedOverCommentIsListedAsSuch(t *testing.T) {
	m := model{width: 80, view: &daemon.ViewWire{Posted: true}}

	rows := m.commentItem(0, carried(1)[0])

	if !strings.Contains(rows[0], "carried over") || strings.Contains(rows[0], "Step 0") {
		t.Errorf("expected the Comment headed as carried over, got %q", rows[0])
	}
}

func TestTheHandedOffScreenCallsOutCarriedOverComments(t *testing.T) {
	m := handedOffModel([]string{"flagged", "seen"},
		daemon.CommentWire{ID: 1, Step: 1},
		daemon.CommentWire{ID: 2, Step: 0, CarriedOver: true},
		daemon.CommentWire{ID: 3, Step: 0, ReRaisedFrom: 8})

	out := flatten(m.doneView())

	want := "3 Comments (1 re-raised, 1 carried over) across 1 Step are waiting for your agent"
	if !strings.Contains(out, want) {
		t.Errorf("expected the call-out %q, got:\n%s", want, out)
	}
}

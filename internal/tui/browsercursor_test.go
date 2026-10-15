package tui

import (
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// cursorReports stands in for dbn's /cursor: it records each cursor the TUI
// reports, in order, with the review it names.
type cursorReports struct {
	reviews []string
	sent    []daemon.CursorWire
}

func (r *cursorReports) last(t *testing.T) daemon.CursorWire {
	t.Helper()
	if len(r.sent) == 0 {
		t.Fatal("expected the cursor reported, but nothing was")
	}
	if review := r.reviews[len(r.reviews)-1]; review != "a1" {
		t.Errorf("expected the cursor reported on review a1, got %q", review)
	}
	return r.sent[len(r.sent)-1]
}

func (r *cursorReports) count() int { return len(r.sent) }

// followedModel is review a1 open on Step 1 showing step, recording the
// cursor reports it makes.
func followedModel(t *testing.T, step *daemon.StepWire) (model, *cursorReports) {
	t.Helper()
	reports := &cursorReports{}
	m := model{
		client:     client{base: "http://127.0.0.1:7373", review: "a1"},
		openReview: "a1",
		mode:       modeReview,
		view:       &daemon.ViewWire{Posted: true, ReviewID: "a1", Position: 1, StepCount: 1, Step: step},
		width:      100, height: 40, ready: true,
		cursorReport: func(reviewID string, at daemon.CursorWire) {
			reports.reviews = append(reports.reviews, reviewID)
			reports.sent = append(reports.sent, at)
		},
	}
	m.syncCursor()
	return m, reports
}

func TestMovingTheCursorOntoABlockReportsTheBlock(t *testing.T) {
	m, reports := followedModel(t, twoParagraphs())

	press(m, "down") // from the heading onto the first paragraph

	want := daemon.CursorWire{Step: 1, Excerpt: 0, Block: 2}
	if got := reports.last(t); got != want {
		t.Errorf("expected the cursor reported as %+v, got %+v", want, got)
	}
}

func TestMovingTheCursorOntoALineOfCodeReportsTheLine(t *testing.T) {
	m, reports := followedModel(t, tallStep())

	press(m, "down")

	want := daemon.CursorWire{Step: 1, Excerpt: 0, Side: "new", Line: 2}
	if got := reports.last(t); got != want {
		t.Errorf("expected the cursor reported as %+v, got %+v", want, got)
	}
}

// The page draws every Document rendered, so a line of one shown here as
// source is followed as the block it lies in.
func TestALineOfADocumentShownAsSourceReportsItsBlock(t *testing.T) {
	m, reports := followedModel(t, twoParagraphs())
	m = press(m, "s")

	press(press(press(m, "down"), "down"), "down") // onto line 4, the first paragraph's second

	want := daemon.CursorWire{Step: 1, Excerpt: 0, Block: 2}
	if got := reports.last(t); got != want {
		t.Errorf("expected the cursor reported as %+v, got %+v", want, got)
	}
}

func TestAKeyThatLeavesTheCursorWhereItIsReportsNothingMore(t *testing.T) {
	m, reports := followedModel(t, tallStep())
	m = press(m, "down")
	before := reports.count()

	press(m, "w") // wrapping redraws the pane but moves nothing

	if after := reports.count(); after != before {
		t.Errorf("expected no further report for a cursor that did not move, got %d more", after-before)
	}
}

// onStepTwo is review a1's view moved on to its second Step.
func onStepTwo() *daemon.ViewWire {
	return &daemon.ViewWire{Posted: true, ReviewID: "a1", Position: 2, StepCount: 2, Step: tallStep()}
}

// Every window open on a review follows its moves, as dbn holds one place for
// it; a window catching up to a move made in another reports nothing, so the
// page follows the window the Reviewer is using.
func TestAWindowCatchingUpToAMoveMadeElsewhereReportsNothing(t *testing.T) {
	m, reports := followedModel(t, twoParagraphs())
	m.view.StepCount = 2

	m.Update(refreshMsg{view: onStepTwo(), polled: true})

	if reports.count() != 0 {
		t.Errorf("expected a window only catching up to report nothing, got %+v", reports.sent)
	}
}

func TestAWindowReportsWhereItLandsOnAMoveItAskedFor(t *testing.T) {
	m, reports := followedModel(t, twoParagraphs())
	m.view.StepCount = 2

	m.Update(refreshMsg{view: onStepTwo()})

	if got := reports.last(t); got.Step != 2 {
		t.Errorf("expected the cursor reported on Step 2, where the move landed, got %+v", got)
	}
}

// A poll can bring the move a window asked for before the window's own
// refresh does; the window still reports where it landed.
func TestAWindowReportsItsMoveEvenWhenAPollBroughtItFirst(t *testing.T) {
	m, reports := followedModel(t, twoParagraphs())
	m.view.StepCount = 2
	polled, _ := m.Update(refreshMsg{view: onStepTwo(), polled: true})

	polled.(model).Update(refreshMsg{view: onStepTwo()})

	if got := reports.last(t); got.Step != 2 {
		t.Errorf("expected the cursor reported on Step 2 once the window's own refresh came, got %+v", got)
	}
}

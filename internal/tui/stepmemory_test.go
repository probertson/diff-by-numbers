package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// otherStep is a second Step to walk away to.
func otherStep() *daemon.StepWire {
	return oneLineStep("elsewhere")
}

// arrive delivers the daemon's view of a position, as the once-a-second refresh
// does after the Reviewer navigates.
func arrive(m model, posting, position int, step *daemon.StepWire) model {
	view := &daemon.ViewWire{Posted: true, Posting: posting, Position: position, StepCount: 2, Step: step}
	after, _ := m.Update(refreshMsg{view: view})
	return after.(model)
}

func walkAwayAndBack(m model, step *daemon.StepWire) model {
	m = arrive(m, 1, 2, otherStep())
	return arrive(m, 1, 1, step)
}

func memoryModel(t *testing.T, step *daemon.StepWire) model {
	t.Helper()
	m := model{client: client{base: expandServer(t).URL}, mode: modeReview}
	return arrive(m, 1, 1, step)
}

func TestReturningToAStepPutsTheCursorWhereItWasLeft(t *testing.T) {
	step := tallStep()
	m := memoryModel(t, step)
	for i := 0; i < 25; i++ {
		m = press(m, "down")
	}

	m = walkAwayAndBack(m, step)

	if got := m.cursor.lines[m.cursor.cursor].number; got != 26 {
		t.Errorf("expected the cursor back on line 26, got line %d", got)
	}
}

func TestAStepIsFreshTheFirstTimeItIsVisited(t *testing.T) {
	m := memoryModel(t, otherStep())

	m = arrive(m, 1, 2, tallStep())

	if m.cursor.cursor != 0 {
		t.Errorf("expected a first visit to start at the top, got index %d", m.cursor.cursor)
	}
}

func TestReturningToAStepKeepsItsAcknowledgementsExpanded(t *testing.T) {
	step := ackStep()
	m := press(press(memoryModel(t, step), "down"), "x") // expand, cursor on its first row
	m = press(m, "down")

	m = walkAwayAndBack(m, step)

	if !m.cursor.isExpanded(0) {
		t.Fatal("expected the Acknowledgement still expanded")
	}
	line := m.cursor.lines[m.cursor.cursor]
	if line.ack != 0 || line.side != "new" || line.number != 2 {
		t.Errorf("expected the cursor back inside the expansion, got %+v", line)
	}
}

func TestLeavingAStepDropsASelection(t *testing.T) {
	step := tallStep()
	m := press(memoryModel(t, step), "v")

	m = walkAwayAndBack(m, step)

	if m.cursor.sel >= 0 {
		t.Error("expected a selection not to survive leaving the Step")
	}
}

func TestANewWalkthroughForgetsHowStepsWereLeft(t *testing.T) {
	step := ackStep()
	m := press(press(memoryModel(t, step), "down"), "x")
	m = arrive(m, 1, 2, otherStep())

	m = arrive(m, 2, 1, step) // a Revision Round, back on Step 1

	if m.cursor.isExpanded(0) || m.cursor.cursor != 0 {
		t.Errorf("expected Step 1 fresh in the new Walkthrough, got cursor %d, expanded %v", m.cursor.cursor, m.cursor.isExpanded(0))
	}
}

func TestResizingKeepsTheCursorWhereItIs(t *testing.T) {
	m := memoryModel(t, tallStep())
	m = press(press(m, "down"), "down")

	after, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	if got := after.(model).cursor.cursor; got != 2 {
		t.Errorf("expected a resize to leave the cursor on index 2, got %d", got)
	}
}

package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// a Step with a multi-line explanation and more code than fits, to exercise the
// windowing budget.
func tallStep() *daemon.StepWire {
	var lines []daemon.LineWire
	for n := 1; n <= 40; n++ {
		lines = append(lines, daemon.LineWire{Number: n, Text: fmt.Sprintf("code line %d", n), Changed: true})
	}
	return &daemon.StepWire{
		Name:        "Add the retrier",
		Explanation: strings.Repeat("the explanation is the reason the reviewer is not reading a bare diff ", 5),
		Excerpts: []daemon.ExcerptWire{
			{File: "revision.go", Side: "new", FirstLine: 1, LastLine: 40, Lines: lines},
		},
	}
}

func TestRenderStepKeepsTheExplanationOnScreen(t *testing.T) {
	step := tallStep()
	const width, height = 80, 20

	// The cursor at the very bottom of the code is the hard case: the code window
	// has scrolled as far as it goes, yet the explanation must still be present.
	cur := newStepCursor(step)
	cur.cursor = len(cur.lines) - 1

	out := renderStep(step, cur, map[string]bool{}, width, height, false)

	if got := lipgloss.Height(out); got > height {
		t.Errorf("Step body is %d rows, over the %d it was given — it will clip", got, height)
	}
	if !strings.Contains(out, "the explanation is the reason") {
		t.Error("the explanation scrolled off — it must stay on screen")
	}
	if !strings.Contains(out, "revision.go") {
		t.Error("the Excerpt's file header is missing")
	}
	// Scrolled to the bottom, there is content above but none below.
	if !strings.Contains(out, "more above") {
		t.Error("expected an 'more above' indicator when scrolled down")
	}
	if strings.Contains(out, "more below") {
		t.Error("did not expect a 'more below' indicator at the bottom of the code")
	}
}

func TestRenderStepIndicatesMoreBelowAndStaysWithinHeight(t *testing.T) {
	step := tallStep()
	const width, height = 80, 20

	cur := newStepCursor(step) // cursor at the top
	out := renderStep(step, cur, map[string]bool{}, width, height, false)

	if got := lipgloss.Height(out); got > height {
		t.Errorf("Step body is %d rows, over the %d it was given", got, height)
	}
	if !strings.Contains(out, "more below") {
		t.Error("expected a 'more below' indicator at the top of a too-tall Step")
	}
	if strings.Contains(out, "more above") {
		t.Error("did not expect a 'more above' indicator at the top")
	}
}

func TestRenderStepShowsNoIndicatorsWhenEverythingFits(t *testing.T) {
	step := &daemon.StepWire{
		Name:        "Small",
		Explanation: "short",
		Excerpts: []daemon.ExcerptWire{{File: "a.ts", Side: "new", FirstLine: 1, LastLine: 2, Lines: []daemon.LineWire{
			{Number: 1, Text: "one", Changed: true},
			{Number: 2, Text: "two", Changed: true},
		}}},
	}
	cur := newStepCursor(step)

	out := renderStep(step, cur, map[string]bool{}, 80, 40, false)

	if strings.Contains(out, "more above") || strings.Contains(out, "more below") {
		t.Errorf("expected no scroll indicators when the whole Step fits, got:\n%s", out)
	}
}

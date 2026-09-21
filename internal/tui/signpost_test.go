package tui

import (
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// A Step showing the tail of a rewrite opens with a signpost saying which Steps
// hold the before-side its new lines replaced. It is drawn so the Reviewer is
// not left reading an addition out of nowhere, but it is not code: resting on it
// would offer select, copy and comment against a row that has no source behind it.

// signpostStep is a Step whose first row is a signpost and whose rest is code.
func signpostStep() *daemon.StepWire {
	return &daemon.StepWire{
		Number: 2, Name: "The tail of the rewrite", Explanation: "the rest of the new logic",
		Excerpts: []daemon.ExcerptWire{{
			Repository: "/r", File: "app.ts", Side: "new", FirstLine: 4, LastLine: 6,
			Lines: []daemon.LineWire{
				{Number: 1, Side: "old", Text: "⋯ replaces old 1-6, shown in Step 1", Signpost: true},
				{Number: 4, Side: "new", Text: "code line 4", Changed: true},
				{Number: 5, Side: "new", Text: "code line 5", Changed: true},
			},
		}},
	}
}

func TestTheCursorDoesNotOpenOnASignpost(t *testing.T) {
	cur := newStepCursor(signpostStep(), nil)

	if !cur.onCode() {
		t.Errorf("expected the cursor to open on code, got kind %v", cur.lines[cur.cursor].kind)
	}
	if cur.lines[cur.cursor].number != 4 {
		t.Errorf("expected the cursor on the first code row, got line %d", cur.lines[cur.cursor].number)
	}
}

func TestTheCursorPassesOverASignpostGoingBack(t *testing.T) {
	cur := newStepCursor(signpostStep(), nil)
	cur.move(1) // onto code line 5

	cur.move(-1)
	cur.move(-1)

	if !cur.onCode() {
		t.Errorf("expected the cursor to stay on code, got kind %v", cur.lines[cur.cursor].kind)
	}
}

func TestASignpostIsDrawnAsItsOwnText(t *testing.T) {
	cur := newStepCursor(signpostStep(), nil)

	rows := paneRows(signpostStep(), cur, nil, nil, 80, 4, false, false)

	var drawn bool
	for _, row := range rows {
		if strings.Contains(row.text, "replaces old 1-6, shown in Step 1") {
			drawn = true
		}
	}
	if !drawn {
		t.Errorf("expected the signpost's own text to be drawn, got %v", rows)
	}
}

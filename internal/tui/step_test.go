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

func TestRenderStepInterleavesBeforeAndAfterAsAUnifiedDiff(t *testing.T) {
	// A new-side Excerpt carrying a unified diff: reference line 1, then the before
	// of an edit (old line 2) immediately above its replacement (new lines 2-3),
	// then reference line 4.
	step := &daemon.StepWire{
		Name:        "Rework the guard",
		Explanation: "line two became two lines",
		Excerpts: []daemon.ExcerptWire{{File: "guard.go", Side: "new", FirstLine: 1, LastLine: 4, Lines: []daemon.LineWire{
			{Number: 1, Text: "reference one", Side: "new", Changed: false},
			{Number: 2, Text: "the old guard", Side: "old", Changed: true},
			{Number: 2, Text: "the new guard", Side: "new", Changed: true},
			{Number: 3, Text: "and its helper", Side: "new", Changed: true},
			{Number: 4, Text: "reference four", Side: "new", Changed: false},
		}}},
	}
	cur := newStepCursor(step)

	out := renderStep(step, cur, map[string]bool{}, 80, 40, false)

	before := strings.Index(out, "the old guard")
	after := strings.Index(out, "the new guard")
	if before < 0 || after < 0 {
		t.Fatalf("expected both the before and after lines to render, got:\n%s", out)
	}
	if before > after {
		t.Error("expected the removed line to render above the line that replaced it")
	}
	// The removed line is marked '-', the added lines '+', the reference lines
	// neither.
	if !strings.Contains(out, "- ") || !strings.Contains(out, "+ ") {
		t.Errorf("expected both a '-' and a '+' row in the unified diff, got:\n%s", out)
	}
}

func TestAChangeRequestAttachesToItsOwnSideNotTheRowSharingItsNumber(t *testing.T) {
	// A before-row and an after-row both numbered 2; a Change Request on the
	// before-side must attribute to the before-row alone.
	step := &daemon.StepWire{
		Number: 1, Name: "Edit", Explanation: "two became TWO",
		Excerpts: []daemon.ExcerptWire{{File: "guard.go", Side: "new", FirstLine: 2, LastLine: 2, Lines: []daemon.LineWire{
			{Number: 2, Text: "the old guard", Side: "old", Changed: true},
			{Number: 2, Text: "the new guard", Side: "new", Changed: true},
		}}},
	}
	m := model{
		view: &daemon.ViewWire{
			Posted: true, Position: 1, Step: step,
			ChangeRequests: []daemon.ChangeRequestWire{{ID: 1, Step: 1, File: "guard.go", Side: "old", FirstLine: 2, LastLine: 2}},
		},
		cursor: newStepCursor(step),
	}

	marked := m.commentedLines()

	if !marked[commentKey("guard.go", "old", 2)] {
		t.Error("expected the before-side row to be marked as commented")
	}
	if marked[commentKey("guard.go", "new", 2)] {
		t.Error("the after-side row sharing the line number must not be marked")
	}

	m.cursor.cursor = 0 // the before-side row
	if _, ok := m.commentAtCursor(); !ok {
		t.Error("expected the Change Request to be found on the before-side row")
	}
	m.cursor.cursor = 1 // the after-side row
	if _, ok := m.commentAtCursor(); ok {
		t.Error("the after-side row must not resolve to the before-side's Change Request")
	}
}

func TestRenderStepShowsADeletionAsBeforeOnly(t *testing.T) {
	// A deliberately shown deletion: an old-side Excerpt whose lines are all removed.
	step := &daemon.StepWire{
		Name:        "Drop the dead path",
		Explanation: "the legacy retry is gone",
		Excerpts: []daemon.ExcerptWire{{File: "legacy.go", Side: "old", FirstLine: 10, LastLine: 11, Lines: []daemon.LineWire{
			{Number: 10, Text: "legacy retry", Side: "old", Changed: true},
			{Number: 11, Text: "more legacy", Side: "old", Changed: true},
		}}},
	}
	cur := newStepCursor(step)

	out := renderStep(step, cur, map[string]bool{}, 80, 40, false)

	if !strings.Contains(out, "- ") || strings.Contains(out, "+ ") {
		t.Errorf("expected a before-only deletion (only '-' rows), got:\n%s", out)
	}
	if !strings.Contains(out, "legacy retry") {
		t.Error("expected the removed code to render")
	}
}

func TestRenderStepKeepsTheOversizeJustificationWithinWidth(t *testing.T) {
	// The label used to be inlined before a width-wrapped block, so "oversized: "
	// pushed the first wrapped line past the width and the terminal dropped text
	// when it reflowed. No row of the Step may exceed the width it was given.
	justification := "One new file: the Correspondence type and the ridealong predicate are a single idea; splitting them would hide the relationship."
	step := &daemon.StepWire{
		Name:                  "Add the type",
		Explanation:           "short",
		OversizeJustification: justification,
		Excerpts: []daemon.ExcerptWire{{File: "a.go", Side: "new", FirstLine: 1, LastLine: 1, Lines: []daemon.LineWire{
			{Number: 1, Text: "x", Changed: true},
		}}},
	}
	cur := newStepCursor(step)
	const width = 80

	out := renderStep(step, cur, map[string]bool{}, width, 40, false)

	if over := widestLine(out); over > width {
		t.Errorf("a row is %d cells wide, over the %d it was given — the oversize label pushed it past the edge:\n%s", over, width, out)
	}
}

func TestRowStyleComposesCommentColourAndCursorBold(t *testing.T) {
	// Colour carries "has a Change Request", weight carries "is the cursor line".
	// They must compose, not override: the bug was a commented cursor line coming
	// out white+bold because the cursor bold clobbered the comment colour.
	cases := []struct {
		name      string
		commented bool
		cursor    bool
		wantBold  bool
		wantFg    lipgloss.TerminalColor
	}{
		{"plain, not the cursor", false, false, false, lipgloss.NoColor{}},
		{"plain, the cursor line", false, true, true, lipgloss.NoColor{}},
		{"commented, not the cursor", true, false, false, commentColor},
		{"commented, the cursor line", true, true, true, commentColor},
	}

	for _, c := range cases {
		s := rowStyle(c.commented, c.cursor)

		if s.GetBold() != c.wantBold {
			t.Errorf("%s: bold = %v, want %v", c.name, s.GetBold(), c.wantBold)
		}
		if s.GetForeground() != c.wantFg {
			t.Errorf("%s: foreground = %v, want %v", c.name, s.GetForeground(), c.wantFg)
		}
	}
}

func TestManifestSuffixPluralizesTheLineCount(t *testing.T) {
	one := manifestSuffix(daemon.AcknowledgedFileWire{ChangedLines: 1})
	if !strings.Contains(one, "1 line") || strings.Contains(one, "line(s)") {
		t.Errorf("one changed line should read '1 line', got %q", one)
	}

	many := manifestSuffix(daemon.AcknowledgedFileWire{ChangedLines: 3})
	if !strings.Contains(many, "3 lines") {
		t.Errorf("three changed lines should read '3 lines', got %q", many)
	}
}

func TestExtendAnchorsThenGrowsTheSelection(t *testing.T) {
	// shift+arrow should behave like an editor: the first extend drops an anchor at
	// the cursor and moves, and each further extend grows the range from that anchor.
	step := &daemon.StepWire{
		Name: "Range", Explanation: "x",
		Excerpts: []daemon.ExcerptWire{{File: "a.go", Side: "new", FirstLine: 1, LastLine: 4, Lines: []daemon.LineWire{
			{Number: 1, Text: "one", Changed: true},
			{Number: 2, Text: "two", Changed: true},
			{Number: 3, Text: "three", Changed: true},
			{Number: 4, Text: "four", Changed: true},
		}}},
	}
	cur := newStepCursor(step) // cursor at line 1, no selection

	cur.extend(1)
	cur.extend(1)

	_, first, last, _, ok := cur.selection()
	if !ok {
		t.Fatal("expected a valid selection after extending")
	}
	if first != 1 || last != 3 {
		t.Errorf("expected the selection to span lines 1-3, got %d-%d", first, last)
	}
}

func TestExtendKeepsTheAnchorWhenReversingDirection(t *testing.T) {
	step := &daemon.StepWire{
		Name: "Range", Explanation: "x",
		Excerpts: []daemon.ExcerptWire{{File: "a.go", Side: "new", FirstLine: 1, LastLine: 4, Lines: []daemon.LineWire{
			{Number: 1, Text: "one", Changed: true},
			{Number: 2, Text: "two", Changed: true},
			{Number: 3, Text: "three", Changed: true},
			{Number: 4, Text: "four", Changed: true},
		}}},
	}
	cur := newStepCursor(step)
	cur.cursor = 2 // start on line 3

	cur.extend(-1) // anchor at line 3, move up to line 2

	_, first, last, _, ok := cur.selection()
	if !ok {
		t.Fatal("expected a valid selection after extending up")
	}
	if first != 2 || last != 3 {
		t.Errorf("expected the selection to span lines 2-3, got %d-%d", first, last)
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

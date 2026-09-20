package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// lineWith returns the single output line containing marker, failing if there is
// not exactly one — a small helper for the render tests that need to inspect one
// row's styling without depending on its exact position in the window.
func lineWith(t *testing.T, out, marker string) string {
	t.Helper()
	var found string
	hits := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, marker) {
			found = line
			hits++
		}
	}
	if hits != 1 {
		t.Fatalf("expected exactly one row containing %q, found %d in:\n%s", marker, hits, out)
	}
	return found
}

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
	cur := newStepCursor(step, nil)
	cur.cursor = len(cur.lines) - 1

	out := renderStep(step, cur, map[string]bool{}, nil, width, height, false)

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

	cur := newStepCursor(step, nil) // cursor at the top
	out := renderStep(step, cur, map[string]bool{}, nil, width, height, false)

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
	cur := newStepCursor(step, nil)

	out := renderStep(step, cur, map[string]bool{}, nil, 80, 40, false)

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

func TestACommentAttachesToItsOwnSideNotTheRowSharingItsNumber(t *testing.T) {
	// A before-row and an after-row both numbered 2; a Comment on the
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
			Comments: []daemon.CommentWire{{ID: 1, Step: 1, File: "guard.go", Segments: []daemon.SegmentWire{
				{Side: "old", FirstLine: 2, LastLine: 2},
			}}},
		},
		cursor: newStepCursor(step, nil),
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
		t.Error("expected the Comment to be found on the before-side row")
	}
	m.cursor.cursor = 1 // the after-side row
	if _, ok := m.commentAtCursor(); ok {
		t.Error("the after-side row must not resolve to the before-side's Comment")
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
	cur := newStepCursor(step, nil)

	out := renderStep(step, cur, map[string]bool{}, nil, 80, 40, false)

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
	cur := newStepCursor(step, nil)
	const width = 80

	out := renderStep(step, cur, map[string]bool{}, nil, width, 40, false)

	if over := widestLine(out); over > width {
		t.Errorf("a row is %d cells wide, over the %d it was given — the oversize label pushed it past the edge:\n%s", over, width, out)
	}
}

func TestRowStyleComposesCommentCursorAndReferenceDim(t *testing.T) {
	// Colour carries "has a Comment", weight carries "is the cursor line",
	// and a dim colour carries "unchanged reference context". Comment colour and
	// cursor weight compose (a commented cursor line is yellow AND bold). The dim
	// is the lowest-priority layer: a reference row is dimmed only when it is
	// neither commented nor the cursor, so context never hides a Comment or
	// the cursor.
	cases := []struct {
		name                         string
		commented, cursor, reference bool
		wantBold                     bool
		wantFg                       lipgloss.TerminalColor
	}{
		{"plain changed, not the cursor", false, false, false, false, lipgloss.NoColor{}},
		{"plain changed, the cursor line", false, true, false, true, lipgloss.NoColor{}},
		{"commented, not the cursor", true, false, false, false, commentColor},
		{"commented, the cursor line", true, true, false, true, commentColor},
		{"plain reference row is dimmed", false, false, true, false, subtle},
		{"reference cursor line keeps the cursor, not dim", false, true, true, true, lipgloss.NoColor{}},
		{"commented reference keeps the comment colour", true, false, true, false, commentColor},
		{"commented reference cursor line", true, true, true, true, commentColor},
	}

	for _, c := range cases {
		s := rowStyle(c.commented, c.cursor, c.reference)

		if s.GetBold() != c.wantBold {
			t.Errorf("%s: bold = %v, want %v", c.name, s.GetBold(), c.wantBold)
		}
		if s.GetForeground() != c.wantFg {
			t.Errorf("%s: foreground = %v, want %v", c.name, s.GetForeground(), c.wantFg)
		}
	}
}

func TestReferenceRowsAreDimmedInBothCodeViews(t *testing.T) {
	// lipgloss strips styling when stdout is not a TTY (as under `go test`), so to
	// see the dim at the render level we force a colour profile for this test and
	// restore whatever it was afterwards (the profile is process-global). A dimmed row then carries an ANSI
	// escape; a plain changed row carries none. This guards the !changed wiring at
	// each call site, which the rowStyle unit test alone cannot.
	orig := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(orig)

	const changed, reference = "CHANGEDROW", "REFERENCEROW"

	step := &daemon.StepWire{
		Name:        "styling",
		Explanation: "x",
		Excerpts: []daemon.ExcerptWire{{
			File: "f.go", Side: "new", FirstLine: 1, LastLine: 3,
			Lines: []daemon.LineWire{
				{Number: 1, Text: "cursorrow", Changed: true, Side: "new"}, // index 0 is the cursor; keep our probes off it
				{Number: 2, Text: changed, Changed: true, Side: "new"},
				{Number: 3, Text: reference, Changed: false, Side: "new"},
			},
		}},
	}
	cur := newStepCursor(step, nil)

	stepOut := renderStep(step, cur, map[string]bool{}, nil, 80, 40, false)

	if strings.Contains(lineWith(t, stepOut, changed), "\x1b") {
		t.Error("renderStep: a plain changed row should not be styled")
	}
	if !strings.Contains(lineWith(t, stepOut, reference), "\x1b") {
		t.Error("renderStep: a plain reference row should be dimmed")
	}

	ackStep := &daemon.StepWire{
		Name: "styling", Explanation: "x",
		Acknowledgements: []daemon.AcknowledgementWire{{Reason: "generated"}},
	}
	expanded := map[int][]daemon.ExcerptWire{0: {{
		File: "f.go", Side: "new",
		Lines: []daemon.LineWire{
			{Number: 1, Text: changed, Changed: true, Side: "new"},
			{Number: 2, Text: reference, Changed: false, Side: "new"},
		},
	}}}
	expandedOut := renderStep(ackStep, newStepCursor(ackStep, expanded), map[string]bool{}, nil, 80, 40, false)

	if strings.Contains(lineWith(t, expandedOut, changed), "\x1b") {
		t.Error("expanded Acknowledgement: a plain changed row should not be styled")
	}
	if !strings.Contains(lineWith(t, expandedOut, reference), "\x1b") {
		t.Error("expanded Acknowledgement: a plain reference row should be dimmed")
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
	cur := newStepCursor(step, nil) // cursor at line 1, no selection

	cur.extend(1)
	cur.extend(1)

	run, ok := cur.selection()

	if !ok {
		t.Fatal("expected a valid selection after extending")
	}
	if run.start.line != 1 || run.end.line != 3 || run.rows != 3 {
		t.Errorf("expected the selection to span lines 1-3, got %d-%d over %d rows", run.start.line, run.end.line, run.rows)
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
	cur := newStepCursor(step, nil)
	cur.cursor = 2 // start on line 3

	cur.extend(-1) // anchor at line 3, move up to line 2

	run, ok := cur.selection()

	if !ok {
		t.Fatal("expected a valid selection after extending up")
	}
	if run.start.line != 2 || run.end.line != 3 {
		t.Errorf("expected the selection to span lines 2-3, got %d-%d", run.start.line, run.end.line)
	}
}

// twoExcerptStep is a Step whose code lines run across an Excerpt boundary, so a
// test can push a selection at it.
func twoExcerptStep() *daemon.StepWire {
	return &daemon.StepWire{
		Name: "Two files", Explanation: "x",
		Excerpts: []daemon.ExcerptWire{
			{File: "a.go", Side: "new", FirstLine: 1, LastLine: 2, Lines: []daemon.LineWire{
				{Number: 1, Text: "one", Side: "new", Changed: true},
				{Number: 2, Text: "two", Side: "new", Changed: true},
			}},
			{File: "b.go", Side: "new", FirstLine: 1, LastLine: 2, Lines: []daemon.LineWire{
				{Number: 1, Text: "three", Side: "new", Changed: true},
				{Number: 2, Text: "four", Side: "new", Changed: true},
			}},
		},
	}
}

func TestASelectionCannotGrowPastTheExcerptItAnchoredIn(t *testing.T) {
	// A Comment anchors inside one Excerpt, so the movement that would carry
	// the selection out of it is refused rather than allowed and rejected later (#57).
	cur := newStepCursor(twoExcerptStep(), nil)

	cur.extend(1)            // anchor on a.go:1, move to a.go:2 — the last row of the Excerpt
	blocked := cur.extend(1) // would cross into b.go

	if !blocked {
		t.Error("expected the extend across the Excerpt boundary to be refused")
	}
	run, ok := cur.selection()
	if !ok {
		t.Fatal("expected the selection to survive the refused movement")
	}
	if run.excerpt != 0 || run.rows != 2 {
		t.Errorf("expected the selection to stay at 2 rows of Excerpt 0, got %+v", run)
	}
}

func TestARefusedExtendLeavesNoSelectionBehind(t *testing.T) {
	// shift+arrow drops its anchor before it knows whether it may move. When the
	// move is refused, the Reviewer must not be left holding a selection they never
	// made — which would silently clamp their plain arrows too.
	cur := newStepCursor(twoExcerptStep(), nil)
	cur.cursor = 1 // the last row of the first Excerpt, nothing selected

	blocked := cur.extend(1)

	if !blocked {
		t.Fatal("expected the extend across the Excerpt boundary to be refused")
	}
	if cur.sel >= 0 {
		t.Error("expected a refused extend to leave the Reviewer unselected")
	}
}

func TestPlainMovementIsClampedTooWhileASelectionIsAlive(t *testing.T) {
	// Selection is modal here: plain arrows grow it just as shift+arrow does, so
	// they meet the same boundary.
	cur := newStepCursor(twoExcerptStep(), nil)
	cur.toggleSelect() // anchor on a.go:1
	cur.move(1)        // to a.go:2

	blocked := cur.move(1)

	if !blocked {
		t.Error("expected the plain movement across the boundary to be refused")
	}
	if cur.cursor != 1 {
		t.Errorf("expected the cursor to stay on the last row of the Excerpt, got row %d", cur.cursor)
	}
}

func TestTheBoundaryMessageIsTakenBackOnceMovementSucceeds(t *testing.T) {
	// It describes the keypress that was refused, not a standing condition, so it
	// must not still be explaining a cursor the Reviewer can see moving.
	m := model{}

	m.noteBoundary(true)
	if m.status != oneExcerptStatus {
		t.Errorf("expected the refusal to be reported, got %q", m.status)
	}

	m.noteBoundary(false)
	if m.status != "" {
		t.Errorf("expected a successful movement to take the message back, got %q", m.status)
	}

	m.status = "Comment added"
	m.noteBoundary(false)
	if m.status != "Comment added" {
		t.Errorf("an unrelated status has nothing to do with moving the cursor, got %q", m.status)
	}
}

func TestTheCursorCrossesExcerptsFreelyWithNoSelection(t *testing.T) {
	// The clamp belongs to a live selection, not to the key: reading is unrestricted.
	cur := newStepCursor(twoExcerptStep(), nil)
	cur.move(1)

	blocked := cur.move(1)

	if blocked {
		t.Error("expected an unselected cursor to cross the Excerpt boundary")
	}
	if cur.lines[cur.cursor].excerpt != 1 {
		t.Errorf("expected the cursor to reach the second Excerpt, got Excerpt %d", cur.lines[cur.cursor].excerpt)
	}
}

func TestASelectionMayCrossFromTheBeforeSideToTheAfterSide(t *testing.T) {
	// The Reviewer reads one interleaved block, so a point about a removal and its
	// replacement is one selection, not two (#57).
	step := &daemon.StepWire{
		Name: "Edit", Explanation: "two became TWO",
		Excerpts: []daemon.ExcerptWire{{File: "guard.go", Side: "new", FirstLine: 1, LastLine: 3, Lines: []daemon.LineWire{
			{Number: 1, Text: "reference one", Side: "new"},
			{Number: 2, Text: "the old guard", Side: "old", Changed: true},
			{Number: 2, Text: "the new guard", Side: "new", Changed: true},
			{Number: 3, Text: "and its helper", Side: "new", Changed: true},
		}}},
	}
	cur := newStepCursor(step, nil)
	cur.cursor = 1 // the removed row

	cur.extend(1)
	cur.extend(1)

	run, ok := cur.selection()

	if !ok {
		t.Fatal("expected a selection crossing the sides to be anchorable")
	}
	if run.start != (rowRef{side: "old", line: 2}) || run.end != (rowRef{side: "new", line: 3}) {
		t.Errorf("expected the run to reach from the removed row to the last added row, got %+v", run)
	}
	if run.rows != 3 {
		t.Errorf("expected the run to span 3 rows, got %d", run.rows)
	}
}

func wrapStep() *daemon.StepWire {
	// Spaces in the long line, so the render tests exercise the word-aware wrap
	// (#73) rather than only its hard-break fallback.
	long := "alpha " + strings.Repeat("a", 24) + " TAIL"
	return &daemon.StepWire{
		Name: "Wrap", Explanation: "x",
		Excerpts: []daemon.ExcerptWire{{File: "a.go", Side: "new", FirstLine: 1, LastLine: 3, Lines: []daemon.LineWire{
			{Number: 1, Text: "short one", Changed: true, Side: "new"},
			{Number: 2, Text: long, Changed: true, Side: "new"},
			{Number: 3, Text: "short three", Changed: true, Side: "new"},
		}}},
	}
}

// indentOf reports the column the first non-space character of a row sits at, so
// a continuation's alignment can be compared against the row it continues.
func indentOf(row string) int {
	return len([]rune(row)) - len([]rune(strings.TrimLeft(row, " ")))
}

// colOf reports the column marker starts at, counting cells rather than bytes —
// the caret and gutter separator are multi-byte, so a byte index would be wrong.
func colOf(t *testing.T, row, marker string) int {
	t.Helper()
	i := strings.Index(row, marker)
	if i < 0 {
		t.Fatalf("expected %q in row %q", marker, row)
	}
	return len([]rune(row[:i]))
}

// oneLineStep wraps a single code line in a Step, for the indent tests.
func oneLineStep(text string) *daemon.StepWire {
	return &daemon.StepWire{
		Name: "Indent", Explanation: "x",
		Excerpts: []daemon.ExcerptWire{{File: "a.go", Side: "new", FirstLine: 1, LastLine: 1, Lines: []daemon.LineWire{
			{Number: 1, Text: text, Changed: true, Side: "new"},
		}}},
	}
}

func TestRenderStepHangsAContinuationUnderTheLinesOwnIndent(t *testing.T) {
	// A tab expands to four spaces, so the continuation should start four cells
	// right of the code column — under the "f" of "foo", not at the gutter edge.
	step := oneLineStep("\tfoo := " + strings.Repeat("a", 60) + "TAIL")
	const width, height = 60, 20
	cur := newStepCursor(step, nil)

	out := renderStep(step, cur, map[string]bool{}, nil, width, height, false)

	first := lineWith(t, out, "foo :=")
	tail := lineWith(t, out, "TAIL")

	if want, got := colOf(t, first, "foo"), indentOf(tail); want != got {
		t.Errorf("the continuation should hang under the line's own indent (column %d), got %d:\n%s", want, got, out)
	}
}

func TestRenderStepDoesNotCarryAlignmentPaddingOntoAContinuation(t *testing.T) {
	// gofmt aligns end-of-line comments with a long run of spaces. A wrap landing
	// inside that run must not push the continuation to an arbitrary column.
	step := oneLineStep(strings.Repeat("x", 40) + strings.Repeat(" ", 20) + "// TAIL")
	const width, height = 60, 20
	cur := newStepCursor(step, nil)

	out := renderStep(step, cur, map[string]bool{}, nil, width, height, false)

	first := lineWith(t, out, "xxx")
	tail := lineWith(t, out, "TAIL")

	if want, got := colOf(t, first, "xxx"), indentOf(tail); want != got {
		t.Errorf("padding at the wrap point leaked onto the continuation: code starts at column %d, continuation at %d:\n%s", want, got, out)
	}
}

func TestRenderStepWrapsTheCursorLineInPlace(t *testing.T) {
	// At width 40 the code column is 27 cells wide, so a 35-cell line's tail
	// ("TAIL") can only be seen if the cursor line soft-wraps.
	step := wrapStep()
	const width, height = 40, 40
	cur := newStepCursor(step, nil)
	cur.cursor = 1 // the long line

	out := renderStep(step, cur, map[string]bool{}, nil, width, height, false)

	if !strings.Contains(out, "TAIL") {
		t.Errorf("the cursor line's tail should wrap into view, got:\n%s", out)
	}
	tail := lineWith(t, out, "TAIL")
	if strings.Contains(tail, "│") {
		t.Errorf("a continuation row should carry a blank gutter, not the line separator, got %q", tail)
	}
	if n := strings.Count(out, "▸"); n != 1 {
		t.Errorf("the caret should mark only the first row of the wrapped line, found %d in:\n%s", n, out)
	}
	if w := widestLine(out); w > width {
		t.Errorf("a wrapped row is %d cells wide, over the %d given:\n%s", w, width, out)
	}
	if h := lipgloss.Height(out); h > height {
		t.Errorf("the wrapped Step is %d rows, over the %d given", h, height)
	}
}

func TestRenderStepTruncatesLinesThatAreNotUnderTheCursor(t *testing.T) {
	step := wrapStep()

	cur := newStepCursor(step, nil) // cursor at index 0, a short line

	out := renderStep(step, cur, map[string]bool{}, nil, 40, 40, false)

	if strings.Contains(out, "TAIL") {
		t.Errorf("a line that is not under the cursor should stay truncated, got:\n%s", out)
	}
	if !strings.Contains(out, "…") {
		t.Error("the long non-cursor line should be truncated with an ellipsis")
	}
}

func TestRenderStepCapsAnEnormousCursorLine(t *testing.T) {
	// A line far longer than half the pane is capped: past the cap the remainder
	// ("ZEND") is unreachable and the last visible row ends in an ellipsis, and the
	// reserved height must not blow the frame.
	huge := strings.Repeat("b", 4000) + "ZEND"
	step := &daemon.StepWire{
		Name: "Huge", Explanation: "x",
		Excerpts: []daemon.ExcerptWire{{File: "a.go", Side: "new", FirstLine: 1, LastLine: 1, Lines: []daemon.LineWire{
			{Number: 1, Text: huge, Changed: true, Side: "new"},
		}}},
	}
	const width, height = 40, 16
	cur := newStepCursor(step, nil)

	out := renderStep(step, cur, map[string]bool{}, nil, width, height, false)

	if h := lipgloss.Height(out); h > height {
		t.Errorf("an enormous wrapped line blew the height: %d rows over the %d given", h, height)
	}
	if strings.Contains(out, "ZEND") {
		t.Error("past the cap the remainder of the line must be unreachable")
	}
	if !strings.Contains(out, "…") {
		t.Error("the capped cursor line should end its last visible row with an ellipsis")
	}
	if w := widestLine(out); w > width {
		t.Errorf("a wrapped row is %d cells wide, over the %d given", w, width)
	}
}

func TestRenderStepStylesEveryRowOfTheWrappedCursorLine(t *testing.T) {
	// The cursor line's styling (here the cursor's bold) must span its wrapped
	// continuation rows so the whole line reads as one unit. lipgloss strips styling
	// off a non-TTY, so force a colour profile and restore it after.
	orig := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(orig)

	long := strings.Repeat("a", 30) + "TAIL"
	step := &daemon.StepWire{
		Name: "Wrap", Explanation: "x",
		Excerpts: []daemon.ExcerptWire{{File: "a.go", Side: "new", FirstLine: 1, LastLine: 1, Lines: []daemon.LineWire{
			{Number: 1, Text: long, Changed: true, Side: "new"},
		}}},
	}
	cur := newStepCursor(step, nil) // the only line, under the cursor

	out := renderStep(step, cur, map[string]bool{}, nil, 40, 40, false)

	tail := lineWith(t, out, "TAIL")
	if strings.Contains(tail, "▸") {
		t.Fatal("the continuation row should not carry the caret")
	}
	if !strings.Contains(tail, "\x1b") {
		t.Error("the cursor line's styling should span its wrapped continuation rows")
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
	cur := newStepCursor(step, nil)

	out := renderStep(step, cur, map[string]bool{}, nil, 80, 40, false)

	if strings.Contains(out, "more above") || strings.Contains(out, "more below") {
		t.Errorf("expected no scroll indicators when the whole Step fits, got:\n%s", out)
	}
}

func TestRenderStepDoesNotSplitAWordAcrossTheCursorLinesWrap(t *testing.T) {
	// At width 40 the code column is 27 cells, one short of this line — so the
	// identifier used to lose its last letter to the next row.
	step := oneLineStep("value := configurationOption")
	cur := newStepCursor(step, nil)

	out := renderStep(step, cur, map[string]bool{}, nil, 40, 20, false)

	if !strings.Contains(out, "configurationOption") {
		t.Errorf("the wrap should fall at the space, leaving the identifier whole, got:\n%s", out)
	}
}

// TestWrapCodeBreaksAtTheLastSpaceThatFits and the tests below it cover the
// wrapper directly: it breaks on spaces (#73), so a word is readable rather than
// cut in half across the wrap point.
func TestWrapCodeBreaksAtTheLastSpaceThatFits(t *testing.T) {
	got := wrapCode("alpha beta gamma", 12, 12, 10)

	want := []string{"alpha beta", "gamma"}
	if !slices.Equal(got, want) {
		t.Errorf("wrapCode = %q, want %q", got, want)
	}
}

func TestWrapCodeStartsNoRowWithASpace(t *testing.T) {
	// A run of spaces at the break point is layout, not content: the row ends
	// before it and the next row starts at the word.
	got := wrapCode("alpha  beta", 7, 7, 10)

	want := []string{"alpha", "beta"}
	if !slices.Equal(got, want) {
		t.Errorf("wrapCode = %q, want %q", got, want)
	}
}

func TestWrapCodeFillsTheRowWithATokenTooLongForAFreshOne(t *testing.T) {
	// "xxx…" is 20 cells and no row is that wide, so moving it down would only
	// waste the rest of this row before hard-breaking it anyway.
	got := wrapCode("ab "+strings.Repeat("x", 20), 10, 10, 10)

	want := []string{"ab xxxxxxx", "xxxxxxxxxx", "xxx"}
	if !slices.Equal(got, want) {
		t.Errorf("wrapCode = %q, want %q", got, want)
	}
}

func TestWrapCodeDoesNotFillARowWithAlignmentPadding(t *testing.T) {
	// The over-long token starts past the row edge, behind a run of gofmt's
	// end-of-line alignment padding. Filling the row would spend it on blanks.
	got := wrapCode("ab"+strings.Repeat(" ", 8)+strings.Repeat("x", 20), 10, 10, 10)

	want := []string{"ab", "xxxxxxxxxx", "xxxxxxxxxx"}
	if !slices.Equal(got, want) {
		t.Errorf("wrapCode = %q, want %q", got, want)
	}
}

func TestWrapCodeDoesNotBreakOnPunctuation(t *testing.T) {
	// Spaces are the only break point: a run of punctuation is part of the token.
	got := wrapCode("a,b,c,d,e,f", 5, 5, 10)

	want := []string{"a,b,c", ",d,e,", "f"}
	if !slices.Equal(got, want) {
		t.Errorf("wrapCode = %q, want %q", got, want)
	}
}

func TestWrapCodeRespectsTheContinuationWidth(t *testing.T) {
	// The first row is wider than the rest, which hang under the line's own indent.
	got := wrapCode("aaaa bbbb cccc dddd", 10, 5, 10)

	want := []string{"aaaa bbbb", "cccc", "dddd"}
	if !slices.Equal(got, want) {
		t.Errorf("wrapCode = %q, want %q", got, want)
	}
}

func TestWrapCodeTruncatesTheLastRowAtTheCap(t *testing.T) {
	got := wrapCode("alpha beta gamma delta epsilon", 12, 12, 2)

	want := []string{"alpha beta", "gamma delta…"}
	if !slices.Equal(got, want) {
		t.Errorf("wrapCode = %q, want %q", got, want)
	}
}

func TestWrapCodeKeepsTheLinesOwnIndentOnTheFirstRow(t *testing.T) {
	// The leading indent is the code's own, not padding at a wrap point, so it
	// stays — and is never itself a break point, which would leave a blank row.
	got := wrapCode("    foobarbazqux more", 12, 12, 10)

	want := []string{"    foobarba", "zqux more"}
	if !slices.Equal(got, want) {
		t.Errorf("wrapCode = %q, want %q", got, want)
	}
}

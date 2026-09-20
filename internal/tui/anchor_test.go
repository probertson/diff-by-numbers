package tui

import (
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
	"github.com/probertson/diff-by-numbers/internal/review"
)

// longAnchor is an Anchor as internal/review writes one: a "Re: …" header, a
// blank line, then marked and numbered code rows, one of them too long to fit.
func longAnchor() string {
	return "Re: internal/tui/tui.go:40-42 (new side) — Step \"Rework the guard\" in diff-by-numbers\n" +
		"\n" +
		"-    40 | was := guard(everything, that, came, before, this, call) // ENDOFOLD\n" +
		"+    41 | now := guard(everything)\n" +
		"     42 | return now\n"
}

func TestAnAnchorsWrappedCodeHangsUnderTheCodeColumn(t *testing.T) {
	rows := renderAnchorRows(longAnchor(), 46)

	first := rowWith(t, rows, "was :=")
	tail := rowWith(t, rows, "ENDOFOLD")

	if want, got := colOf(t, first, "was"), indentOf(tail); want != got {
		t.Errorf("the continuation should sit under the code column (%d), got %d:\n%s", want, got, strings.Join(rows, "\n"))
	}
	if strings.Contains(tail, "|") {
		t.Errorf("a continuation row carries a blank gutter, not the separator, got %q", tail)
	}
}

func TestAnAnchorsHeaderWrapsPlainly(t *testing.T) {
	const width = 46

	rows := renderAnchorRows(longAnchor(), width)

	header := rows[0]
	for _, row := range rows[1:] {
		if row == "" { // the blank line the header is separated from the code by
			break
		}
		if indentOf(row) != 0 {
			t.Errorf("the header's own continuation hangs at column 0, got %q", row)
		}
		header += " " + row
	}

	if header != "Re: internal/tui/tui.go:40-42 (new side) — Step \"Rework the guard\" in diff-by-numbers" {
		t.Errorf("the header should come back whole, just wrapped, got %q", header)
	}
	if len([]rune(rows[0])) > width {
		t.Errorf("the header's first row is wider than the %d given: %q", width, rows[0])
	}
}

func TestEveryKindOfAnchorRowAlignsTheSame(t *testing.T) {
	long := strings.Repeat("x", 60) + " TAIL"
	anchor := "-    40 | " + long + "\n" +
		"+    41 | " + long + "\n" +
		"     42 | " + long + "\n"

	rows := renderAnchorRows(anchor, 40)

	var indents []int
	for _, row := range rows {
		if strings.Contains(row, "TAIL") {
			indents = append(indents, indentOf(row))
		}
	}
	if len(indents) != 3 {
		t.Fatalf("expected a continuation carrying TAIL for each of the three rows, got %d:\n%s", len(indents), strings.Join(rows, "\n"))
	}
	if indents[0] != indents[1] || indents[1] != indents[2] {
		t.Errorf("a removal, an addition and an unchanged row should all align, got %v", indents)
	}
}

func TestAnAnchorRowWithNoSpacesHardBreaks(t *testing.T) {
	anchor := "+    41 | " + strings.Repeat("z", 40) + "\n"

	rows := renderAnchorRows(anchor, 30)

	if len(rows) < 2 {
		t.Fatalf("a 40-cell token in a 20-cell code column must break, got %q", rows)
	}
	for _, row := range rows {
		if len([]rune(row)) > 30 {
			t.Errorf("row %q is wider than the 30 given", row)
		}
	}
}

func TestAnAnchorAtANarrowWidthStillMakesProgress(t *testing.T) {
	// One cell of code column is the tightest the wrapping path can be asked for;
	// four cells is narrower than the gutter, which takes the clipping path. Both
	// must come back rather than loop, and neither may run past the width.
	for _, width := range []int{9, 10, 11, 12} {
		rows := renderAnchorRows(longAnchor(), width)

		if len(rows) < 5 {
			t.Errorf("width %d: every source row should still be accounted for, got %d", width, len(rows))
		}
		for _, row := range rows {
			if len([]rune(row)) > width {
				t.Errorf("width %d: row %q runs past it", width, row)
			}
		}
	}
}

func TestAnAnchorOnAModelThatIsNotSizedYetKeepsItsCode(t *testing.T) {
	// Before the first WindowSizeMsg the width is 0, which means "no limit" here
	// as it does for wrapTo. Clipping to it would blank every code row instead.
	for _, width := range []int{0, -5} {
		rows := renderAnchorRows(longAnchor(), width)

		if rowWith(t, rows, "ENDOFOLD") == "" {
			t.Errorf("width %d: the code should come through whole", width)
		}
	}
}

func TestTheAnchorGutterPatternMatchesWhatTheDaemonWrites(t *testing.T) {
	// The gutter's format lives in review.Anchor.Render; this pattern re-derives
	// it. Pin them together, or a change there would silently stop the TUI
	// wrapping — the rows would just fall through unrecognised.
	anchor := review.Anchor{
		Repository: "/tmp/diff-by-numbers",
		File:       "internal/tui/tui.go",
		StepName:   "Rework the guard",
		Segments:   []review.AnchorSegment{{Side: review.NewSide, FirstLine: 1, LastLine: 123456}},
		Lines: []review.Line{
			{Number: 1, Text: "one digit", Side: review.OldSide, Changed: true},
			{Number: 99999, Text: "five digits", Side: review.NewSide, Changed: true},
			{Number: 123456, Text: "past the padding", Side: review.NewSide, Changed: false},
			{Number: 7, Text: "", Side: review.NewSide, Changed: true},
		},
	}.Render()

	for _, row := range strings.Split(strings.TrimRight(anchor, "\n"), "\n") {
		if strings.HasPrefix(row, "Re: ") || row == "" {
			continue
		}
		if !anchorCodeRow.MatchString(row) {
			t.Errorf("the gutter pattern no longer matches what Render writes: %q", row)
		}
	}
}

func TestAnAnchorsBlankLinePassesThrough(t *testing.T) {
	rows := renderAnchorRows(longAnchor(), 46)

	blanks := 0
	for _, row := range rows {
		if row == "" {
			blanks++
		}
	}
	if blanks != 1 {
		t.Errorf("the one blank line between header and code should survive as itself, found %d:\n%s", blanks, strings.Join(rows, "\n"))
	}
}

func TestTheCommentModalAndTheListDrawAnAnchorTheSameWay(t *testing.T) {
	anchor := longAnchor()
	const width = 60
	modal := model{width: width, note: newNote(width), pendingCode: anchor}
	// The list indents each anchor row five cells, so it has five fewer to wrap in.
	list := model{
		width: width + listItemIndent, height: 40,
		view: &daemon.ViewWire{Comments: []daemon.CommentWire{{ID: 1, Step: 1, Anchor: anchor, Note: "n"}}},
	}

	modalOut, listOut := modal.noteView(), list.listView()

	for _, row := range renderAnchorRows(anchor, width) {
		if !strings.Contains(modalOut, row) {
			t.Errorf("the modal should draw the anchor row %q, got:\n%s", row, modalOut)
		}
		if !strings.Contains(listOut, row) {
			t.Errorf("the list should draw the same anchor row %q, got:\n%s", row, listOut)
		}
	}
}

// rowWith is lineWith over a slice of rows rather than a rendered block.
func rowWith(t *testing.T, rows []string, marker string) string {
	t.Helper()
	var found string
	hits := 0
	for _, row := range rows {
		if strings.Contains(row, marker) {
			found = row
			hits++
		}
	}
	if hits != 1 {
		t.Fatalf("expected exactly one row containing %q, found %d in:\n%s", marker, hits, strings.Join(rows, "\n"))
	}
	return found
}

package tui

import (
	"fmt"
	"strings"
)

// listMarkerRows is the pair of rows a windowed list reserves for its overflow
// markers, one above and one below. They are drawn blank at an edge rather than
// dropped, so the body keeps the same height as the cursor moves and the keybar
// under it never shifts.
const listMarkerRows = 2

// windowItems draws the items of a list, windowed on the one under the cursor.
// That item is always shown in full and centred where the room allows; an item
// taller than the window on its own is held to its top row and the rest is
// clipped behind the downward marker. The window is derived from the cursor on
// every render rather than stored, so a resize needs no bookkeeping — the same
// rule the Step pane follows.
func windowItems(items [][]string, cursor, height int) string {
	var rows []string
	owner := []int{} // the item each row belongs to, for counting what is off-screen
	// An item ends in a blank row that sets it off from the next. Losing that row
	// to the window's edge hides nothing, so each item's last row of content is
	// kept as well as its first.
	first := make([]int, len(items))
	lastContent := make([]int, len(items))
	curFirst, curLast := 0, 0
	for i, item := range items {
		first[i], lastContent[i] = len(rows), len(rows)
		if i == cursor {
			curFirst, curLast = len(rows), len(rows)+len(item)-1
		}
		for _, row := range item {
			if strings.TrimSpace(row) != "" {
				lastContent[i] = len(rows)
			}
			owner = append(owner, i)
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		return ""
	}
	if curLast < curFirst {
		curLast = curFirst // an item with no rows of its own
	}
	if len(rows) <= height {
		return strings.Join(rows, "\n")
	}

	// On a pane too short to spend two rows on markers, the items win: an arrow
	// and nothing else says less than a Comment does. The Step pane makes the same
	// trade.
	showMarkers := height > listMarkerRows
	budget := height
	if showMarkers {
		budget -= listMarkerRows
	}
	budget = max(1, budget)
	cursorHeight := curLast - curFirst + 1
	start := curFirst
	if budget > cursorHeight {
		start = curFirst - (budget-cursorHeight)/2
	}
	start = max(0, start)
	end := start + budget
	if end > len(rows) {
		end = len(rows)
		start = max(0, end-budget)
	}

	if !showMarkers {
		return strings.Join(rows[start:end], "\n")
	}

	// An item is off-screen when any of its content is, so one straddling an edge
	// is counted there: it is a Comment the Reviewer cannot read where they are.
	above := owner[start]
	if start > first[owner[start]] {
		above++
	}
	below := len(items) - 1 - owner[end-1]
	if end <= lastContent[owner[end-1]] {
		below++
	}

	out := make([]string, 0, budget+listMarkerRows)
	out = append(out, overflowMarker("↑", "above", above))
	out = append(out, rows[start:end]...)
	out = append(out, overflowMarker("↓", "below", below))
	return strings.Join(out, "\n")
}

// overflowMarker is one of the two rows saying how much of a list lies past the
// window, or a blank row when nothing does.
func overflowMarker(arrow, where string, n int) string {
	if n == 0 {
		return ""
	}
	return dimSt.Render(fmt.Sprintf("  %s %d more %s", arrow, n, where))
}

// listAnchorCap is how many source lines of an Anchor the Comment list quotes
// before saying how many it left out. The whole quote is one keypress away in
// the editor, and a long Anchor would otherwise push every other Comment off the
// screen.
const listAnchorCap = 5

// capAnchorRows trims an Anchor to at most n of its code rows and reports how
// many it dropped. It counts source lines rather than drawn rows, so how much of
// an Anchor a list item shows does not change with the terminal's width.
//
// Like renderAnchorRows it finds those rows with anchorCodeRow, reading
// structure back out of rendered text; see that function for why the coupling is
// acceptable here.
func capAnchorRows(anchor string, n int) (string, int) {
	n = max(0, n)
	lines := strings.Split(strings.TrimRight(anchor, "\n"), "\n")
	code := 0
	for _, line := range lines {
		if anchorCodeRow.MatchString(line) {
			code++
		}
	}
	if code <= n {
		return anchor, 0
	}
	kept := 0
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if anchorCodeRow.MatchString(line) {
			if kept == n {
				break
			}
			kept++
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n"), code - n
}

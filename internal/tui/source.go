package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// A Document can be shown as its source rather than formatted: s switches every
// Document between the two for the rest of the review. The source view is the
// lines as they stand, signed + and -, with no background bands: the changed
// text itself is coloured, a new line green in full and, in a rewrite, only the
// words that changed.

// toggleSource switches every Document between formatted and source. It is
// offered only where there is a Document to switch, so the Reviewer never flips
// a setting they cannot see.
func (m *model) toggleSource() {
	if !m.showsDocument() {
		m.status = "no Document on this Step to show as source"
		return
	}
	m.status = ""
	if len(m.cursor.lines) == 0 {
		m.showSource = !m.showSource
		m.syncCursor()
		return
	}
	row := m.cursorRow()
	m.showSource = !m.showSource
	m.relayout(m.cursor.lines[m.cursor.cursor])
	m.cursor.held, m.cursor.heldRow = true, row
}

// cursorRow is the row of the Step's pane the cursor is drawn on now.
func (m *model) cursorRow() int {
	step, showRepo := m.view.Step, m.multiRepo()
	header := stepHeader(step, m.width, showRepo, m.proseWidth)
	pane := layoutPane(header, step, m.cursor, m.commentedLines(), m.ackComments(), m.width, m.bodyHeight(), showRepo, m.wrapAll, m.proseWidth)
	return pane.cursorRow(m.cursor)
}

// sourceToggleKey names what s will do next, as wrapToggleKey does for w.
func (m model) sourceToggleKey() string {
	if m.showSource {
		return "s show formatted"
	}
	return "s show source"
}

// showsDocument reports whether the Step's pane draws any Document, in its own
// Excerpts or an expanded Acknowledgement's: the only places s changes.
func (m model) showsDocument() bool {
	for _, excerpt := range m.view.Step.Excerpts {
		if len(excerpt.Blocks) > 0 {
			return true
		}
	}
	for _, excerpts := range m.cursor.expanded {
		for _, excerpt := range excerpts {
			if len(excerpt.Blocks) > 0 {
				return true
			}
		}
	}
	return false
}

// docRed is the terminal palette's red, for the source view's removed text.
var docRed = lipgloss.ANSIColor(1)

// sourceRows draws one line of a Document shown as source. It wraps as a line of
// code does, across the pane's full width rather than the prose width, since
// the source keeps the writer's own line breaks. A changed line is signed + or -
// and its text coloured green or red: all of it on a line with nothing to compare
// it with, and only the words that changed in a rewrite. No band is drawn behind
// it, and nothing is struck through. A Comment is the gutter's mark, as in the
// formatted view, so the text colour is left to say what changed.
func sourceRows(cur stepCursor, i int, hasComment bool, width, wrapCap int, wrapAll bool) []string {
	line := cur.lines[i]
	sign, colour := " ", lipgloss.TerminalColor(lipgloss.NoColor{})
	switch {
	case line.changed && removedSide(line.side):
		sign, colour = "-", docRed
	case line.changed:
		sign, colour = "+", docGreen
	}
	mark := " "
	if hasComment {
		mark = lipgloss.NewStyle().Foreground(commentColor).Render("●")
	}
	signed := lipgloss.NewStyle().Foreground(colour).Render(sign)
	gutter := signed + " " + mark + " " + dimSt.Render(fmt.Sprintf("%5d │", line.number)) + " "

	isCursor := i == cur.cursor
	selected := cur.sel >= 0 && cur.inSelection(i)
	runes, changedRunes := expandTabs(line.text, line.emphasis)
	rows := wrapLine(runes, gutter, width-2, wrapCap, isCursor || wrapAll)

	base := lipgloss.NewStyle()
	if !line.changed && !isCursor {
		base = base.Foreground(subtle)
	}
	if isCursor {
		base = base.Bold(true)
	}
	changed := base.Foreground(colour)
	wholeLine := len(line.emphasis) == 0

	out := make([]string, 0, len(rows))
	for ri, row := range rows {
		caret := "  "
		if isCursor && ri == 0 {
			caret = caretSt.Render("▸ ")
		}
		switch {
		case row.plain:
			out = append(out, caret+base.Render(ansi.Strip(row.chunk)))
			continue
		case selected:
			out = append(out, caret+row.prefix+selSt.Render(row.chunk))
			continue
		case !line.changed || wholeLine:
			style := base
			if line.changed {
				style = changed
			}
			out = append(out, caret+row.prefix+style.Render(row.chunk))
			continue
		}
		chunk := []rune(row.chunk)
		// A clipped row ends in an ellipsis standing in for what was cut, which
		// is not a rune of the text and did not change.
		clipped := string(runes[row.start:min(row.start+len(chunk), len(runes))]) != row.chunk
		changedAt := func(at int) bool {
			if clipped && at == len(chunk)-1 {
				return false
			}
			return emphasisAt(changedRunes, row.start+at)
		}
		var b strings.Builder
		b.WriteString(caret + row.prefix)
		for from := 0; from < len(chunk); {
			on := changedAt(from)
			to := from + 1
			for to < len(chunk) && changedAt(to) == on {
				to++
			}
			style := base
			if on {
				style = changed
			}
			b.WriteString(style.Render(string(chunk[from:to])))
			from = to
		}
		out = append(out, b.String())
	}
	return out
}

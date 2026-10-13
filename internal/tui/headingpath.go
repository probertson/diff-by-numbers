package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// headingSeparator sets each heading of a heading path apart in a file header.
const headingSeparator = " › "

// fileHeaderText is the file header that opens an Excerpt and sticks at the top
// of the pane once it has scrolled away: the file, then, for a Document, the
// heading path of the block it stands over (#145). It is chrome, drawn across
// the pane's full width rather than the prose width, and never a cursor stop.
//
// A path too wide for the pane loses its outer headings first, from the
// outermost in, behind one `…`, since the innermost heading says where a change
// landed; only then is the innermost heading's text cut short. A file with no
// path, as code has none, is just the file.
func fileHeaderText(label string, path []string, width int) string {
	lead := "── " + label
	if len(path) == 0 {
		return lead
	}
	fits := func(text string) bool { return width <= 0 || ansi.StringWidth(text) <= width }
	joined := func(parts ...string) string { return strings.Join(parts, headingSeparator) }

	if full := joined(append([]string{lead}, path...)...); fits(full) {
		return full
	}
	for drop := 1; drop < len(path); drop++ {
		if shorter := joined(append([]string{lead, "…"}, path[drop:]...)...); fits(shorter) {
			return shorter
		}
	}
	prefix := lead + headingSeparator
	if len(path) > 1 {
		prefix += "…" + headingSeparator
	}
	innermost := path[len(path)-1]
	room := width - ansi.StringWidth(prefix)
	if room < 2 {
		// Not even a letter of the heading fits beside the file: the row is cut
		// as any other row too wide for the pane is.
		return ansi.Truncate(prefix+innermost, width, "…")
	}
	return prefix + strings.TrimRight(ansi.Truncate(innermost, room-1, ""), " ") + "…"
}

// headingPathOf is the heading path of what a cursor line shows of an Excerpt.
// A block's is its own. A row's (a line of the source view, or a row of a
// Document in no block) is that of the row in a block nearest above it, or else
// below it, read on the row's own side: a row an edit replaced is drawn in the
// block that replaced it, but sits under the old headings. Code has none.
func headingPathOf(excerpt daemon.ExcerptWire, line codeLine) []string {
	if line.block > 0 && line.block <= len(excerpt.Blocks) {
		return excerpt.Blocks[line.block-1].Path
	}
	if len(excerpt.Blocks) == 0 {
		return nil
	}
	for i, row := range excerpt.Lines {
		if row.Number == line.number && lineSide(row, excerpt) == line.side {
			if near := rowNear(excerpt, i); near >= 0 {
				return excerpt.Lines[near].Path
			}
			return nil
		}
	}
	return nil
}

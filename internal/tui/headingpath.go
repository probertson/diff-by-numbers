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

// headingPathOf is the heading path of what a cursor line shows of an Excerpt:
// that of its block or, for a row of a Document in no block (a blank line, or a
// line drawn as code), that of the block nearest above it, or else below it.
// Code has none.
func headingPathOf(excerpt daemon.ExcerptWire, line codeLine) []string {
	b := line.block
	if b == 0 {
		b = line.inBlock
	}
	if b == 0 && len(excerpt.Blocks) > 0 {
		for i, row := range excerpt.Lines {
			if row.Number == line.number && lineSide(row, excerpt) == line.side {
				b = blockNear(excerpt, i)
				break
			}
		}
	}
	if b < 1 || b > len(excerpt.Blocks) {
		return nil
	}
	return excerpt.Blocks[b-1].Path
}

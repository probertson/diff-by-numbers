package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// anchorCodeRow matches the gutter an Anchor puts in front of a line of code:
// its marker, its line number and the separator, written by Anchor.Render as
// "%s %5d | %s". It is also how wide a continuation row hangs, so the code lines
// up in one column however often it wraps.
var anchorCodeRow = regexp.MustCompile(`^[ +-] +\d+ \| `)

// anchorHeader opens the line Anchor.Render leads with, the one an agent can
// read at a glance. It is prose, so it wraps as prose.
const anchorHeader = "Re: "

// renderAnchorRows lays an Anchor out at width cells. The "Re: …" header
// word-wraps plainly; a code row too long for the width wraps under its own code
// column behind a blank gutter, so an Anchor reads the same wherever it is
// shown. Anything else — a blank line, say — passes through.
//
// The continuation hangs at the gutter only. The Step pane hangs it further, by
// the line's own leading indent (#26), but there the whole Excerpt is on screen
// to line up against; an Anchor is a few rows quoted out of context.
//
// There is no row cap: the code is the whole point of the Anchor, and a caller
// that wants less shows fewer source rows rather than a clipped one.
//
// It reads structure back out of text the daemon already rendered, which is the
// coupling ADR-0005 keeps dbn away from. It is deliberate and narrow: a Comment
// carries its Anchor as one string, and anchorCodeRow is pinned to the format
// that writes it.
func renderAnchorRows(anchor string, width int) []string {
	var out []string
	for _, row := range strings.Split(strings.TrimRight(anchor, "\n"), "\n") {
		prefix := anchorCodeRow.FindString(row)
		if prefix == "" {
			if !strings.HasPrefix(row, anchorHeader) {
				out = append(out, row) // a blank line, or anything else: as it came
				continue
			}
			// lipgloss pads a wrapped block out to the width; the trailing cells are
			// nothing the Reviewer can see, and they would make a row compare unequal
			// to the same row drawn anywhere else.
			for _, wrapped := range strings.Split(wrapTo(row, width), "\n") {
				out = append(out, strings.TrimRight(wrapped, " "))
			}
			continue
		}
		gutter := lipgloss.Width(prefix)
		textWidth := width - gutter
		if width < 1 {
			// Not sized yet: no limit, the same reading wrapTo gives it. Clipping
			// here would drop the code entirely rather than merely fail to wrap it.
			out = append(out, row)
			continue
		}
		if textWidth < 1 {
			// Narrower than the gutter, so there is no code column left to hang
			// anything under. Clip instead, the way the Step pane clips a row that
			// does not fit.
			out = append(out, truncateTo(row, width))
			continue
		}
		// A row has at least one rune, so its length bounds how many rows it can
		// wrap to — a cap that can never be reached, rather than none at all.
		chunks := wrapCode(row[len(prefix):], textWidth, textWidth, len(row))
		out = append(out, prefix+chunks[0])
		for _, chunk := range chunks[1:] {
			out = append(out, strings.Repeat(" ", gutter)+chunk)
		}
	}
	return out
}

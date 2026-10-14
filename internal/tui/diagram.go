package tui

import "github.com/probertson/diff-by-numbers/internal/daemon"

// diagramHint is the header-like row drawn above a Mermaid diagram — a diagram
// block's source, or an Excerpt of a diagram file — saying its picture is in
// the browser (#195). It is decor, no cursor line: the cursor passes it by, and
// nothing can select, copy or anchor it. The browser opens only on p. Like a
// file header it is one row, cut to the pane's width.
func diagramHint(width int) string {
	return dimSt.Render(truncateTo("  Mermaid diagram · p to open in browser", width))
}

// isDiagramBlock reports whether block b (from 1) of a Document Excerpt is a
// Mermaid diagram.
func isDiagramBlock(excerpt daemon.ExcerptWire, b int) bool {
	return b > 0 && b <= len(excerpt.Blocks) && excerpt.Blocks[b-1].Diagram
}

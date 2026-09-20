package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// fileLabel names a file for a Step header or manifest. When a Walkthrough spans
// more than one repository the file alone is ambiguous, so it is prefixed with
// the repository's base name; with a single repository that would be noise.
func fileLabel(repository, file string, showRepo bool) string {
	if showRepo {
		return "[" + filepath.Base(repository) + "] " + file
	}
	return file
}

// lineKind says what a row of a Step's pane is to the cursor.
type lineKind int

const (
	// kindCode is a line of code: selectable, commentable, anchorable.
	kindCode lineKind = iota
	// kindStop is an Acknowledgement's header row: the cursor rests on it to
	// expand or collapse the Acknowledgement, but there is no code to select.
	kindStop
	// kindNote stands for an expanded file with no lines to show — an Opaque
	// Change, or code that could not be read. It is drawn so nothing the
	// Acknowledgement claims goes unseen, but the cursor never lands on it.
	kindNote
)

// codeLine is one row of a Step's pane the cursor knows about: a code line, an
// Acknowledgement's stop, or a note in an expansion. ack is the Acknowledgement
// it belongs to, or -1 for the Step's own Excerpts; excerpt then indexes the
// Step's Excerpts or that Acknowledgement's expansion. Selection works within a
// single Excerpt.
type codeLine struct {
	kind    lineKind
	ack     int
	excerpt int
	number  int
	side    string
	text    string
	changed bool
}

// sameExcerpt reports whether two rows lie in the same Excerpt — a narrated one,
// or one of an Acknowledgement's expansion.
func (l codeLine) sameExcerpt(other codeLine) bool {
	return l.ack == other.ack && l.excerpt == other.excerpt
}

// sameSpot reports whether two rows are the same place in the Step, by identity
// rather than index: expanding or collapsing an Acknowledgement shifts every
// index after it, but not what a row is.
func (l codeLine) sameSpot(other codeLine) bool {
	return l.kind == other.kind && l.ack == other.ack && l.excerpt == other.excerpt &&
		l.side == other.side && l.number == other.number
}

// stepCursor holds the cursor and selection over a Step's pane: its own code,
// then each Acknowledgement's stop followed, when expanded, by the code it
// stands in for. It is rebuilt whenever the Step or an expansion changes.
type stepCursor struct {
	lines    []codeLine
	cursor   int // index into lines
	sel      int // selection start index, or -1 for none
	expanded map[int][]daemon.ExcerptWire
}

// newStepCursor lays out a Step's pane. expanded holds the Excerpts of each
// Acknowledgement the Reviewer has expanded, by index; an absent one is
// collapsed to its stop.
func newStepCursor(step *daemon.StepWire, expanded map[int][]daemon.ExcerptWire) stepCursor {
	var lines []codeLine
	for ei, excerpt := range step.Excerpts {
		for _, line := range excerpt.Lines {
			lines = append(lines, codeLine{
				kind: kindCode, ack: -1, excerpt: ei,
				number: line.Number, side: lineSide(line, excerpt), text: line.Text, changed: line.Changed,
			})
		}
	}
	for k := range step.Acknowledgements {
		lines = append(lines, codeLine{kind: kindStop, ack: k, excerpt: -1})
		for ei, excerpt := range expanded[k] {
			if len(excerpt.Lines) == 0 {
				lines = append(lines, codeLine{kind: kindNote, ack: k, excerpt: ei})
				continue
			}
			for _, line := range excerpt.Lines {
				lines = append(lines, codeLine{
					kind: kindCode, ack: k, excerpt: ei,
					number: line.Number, side: lineSide(line, excerpt), text: line.Text, changed: line.Changed,
				})
			}
		}
	}
	return stepCursor{lines: lines, cursor: 0, sel: -1, expanded: expanded}
}

// isExpanded reports whether Acknowledgement k is showing its code.
func (c stepCursor) isExpanded(k int) bool {
	_, ok := c.expanded[k]
	return ok
}

// onCode reports whether the cursor is on a line of code, the only row that can
// be selected, commented on or anchored.
func (c stepCursor) onCode() bool {
	return len(c.lines) > 0 && c.lines[c.cursor].kind == kindCode
}

// excerptOf is the Excerpt a row lies in: one of the Step's own, or one of an
// Acknowledgement's expansion.
func (c stepCursor) excerptOf(step *daemon.StepWire, line codeLine) daemon.ExcerptWire {
	if line.ack >= 0 {
		return c.expanded[line.ack][line.excerpt]
	}
	return step.Excerpts[line.excerpt]
}

// restore puts the cursor back on a remembered row. A row inside an
// Acknowledgement that is no longer expanded falls back to that Acknowledgement's
// stop; a row that is gone altogether leaves the cursor where it is.
func (c *stepCursor) restore(spot codeLine) {
	for i, line := range c.lines {
		if line.sameSpot(spot) {
			c.cursor = i
			return
		}
	}
	if spot.ack < 0 {
		return
	}
	for i, line := range c.lines {
		if line.kind == kindStop && line.ack == spot.ack {
			c.cursor = i
			return
		}
	}
}

// lineSide is the side a rendered row belongs to: its own, since a new-side
// Excerpt's lines are a unified diff mixing the after-side with the before-side it
// replaced. It falls back to the Excerpt's side for a line that carries none.
func lineSide(line daemon.LineWire, excerpt daemon.ExcerptWire) string {
	if line.Side != "" {
		return line.Side
	}
	return excerpt.Side
}

// move steps the cursor by delta rows it can land on — past any note — clamped to
// the pane and, while a selection is alive, to the Excerpt that selection
// anchored in. A Comment anchors inside one Excerpt, so rather than let the
// selection grow somewhere it cannot be raised and reject it later, the movement
// itself is refused (#57). It reports whether the boundary blocked it, so the
// caller can say why nothing moved.
//
// The clamp is conditioned on a live selection rather than on the keypress, so a
// key that first clears the selection and then moves is unaffected by it.
func (c *stepCursor) move(delta int) (blocked bool) {
	if len(c.lines) == 0 {
		return false
	}
	direction, count := 1, delta
	if delta < 0 {
		direction, count = -1, -delta
	}
	target := c.cursor
	for ; count > 0; count-- {
		next := target + direction
		for next >= 0 && next < len(c.lines) && c.lines[next].kind == kindNote {
			next += direction
		}
		if next < 0 || next >= len(c.lines) {
			break
		}
		target = next
	}
	if c.sel >= 0 && target != c.cursor {
		anchor := c.lines[c.sel]
		if c.lines[target].kind != kindCode || !c.lines[target].sameExcerpt(anchor) {
			return true
		}
	}
	c.cursor = target
	return false
}

// extend grows the selection by moving the cursor, dropping an anchor at the
// current line first if none is set. It backs shift+arrow, the editor-conventional
// way to select a range without first pressing a select key.
//
// A refused movement leaves no trace: the anchor it would have dropped is taken
// back, so a shift+arrow at an Excerpt boundary does not leave the Reviewer in a
// selection they never made and did not see begin.
func (c *stepCursor) extend(delta int) (blocked bool) {
	if len(c.lines) == 0 {
		return false
	}
	was := c.sel
	if c.sel < 0 {
		c.sel = c.cursor
	}
	if c.move(delta) {
		c.sel = was
		return true
	}
	return false
}

func (c *stepCursor) toggleSelect() {
	if c.sel == c.cursor {
		c.sel = -1
		return
	}
	c.sel = c.cursor
}

// rowRef names one rendered row by the side it belongs to and its line number on
// that side — what the daemon needs to find it again among the Excerpt's rows.
type rowRef struct {
	side string
	line int
}

// selectedRun is the run of rendered rows the Reviewer has selected: the Excerpt
// it lies in, the row at each end, and how many rows it spans. The rows between
// the ends are deliberately not named here — the daemon owns the order they are
// in, and re-deriving it client-side would be a second copy of that knowledge.
type selectedRun struct {
	ack        int // the Acknowledgement whose expansion it lies in, or -1
	excerpt    int
	start, end rowRef
	rows       int
}

// selection returns the run currently selected, and whether it can be anchored.
// It may cross from the before-side to the after-side of a unified diff: the
// Reviewer reads one interleaved block, and a point is often about the removal
// and its replacement together (#57). It may not straddle two Excerpts — the
// clamp in move keeps a live selection inside one, so this asserts rather than
// assumes it.
func (c stepCursor) selection() (selectedRun, bool) {
	if len(c.lines) == 0 {
		return selectedRun{}, false
	}
	start := c.cursor
	if c.sel >= 0 {
		start = c.sel
	}
	end := c.cursor
	if start > end {
		start, end = end, start
	}
	first, last := c.lines[start], c.lines[end]
	if first.kind != kindCode || last.kind != kindCode || !first.sameExcerpt(last) {
		return selectedRun{}, false
	}
	return selectedRun{
		ack:     first.ack,
		excerpt: c.lines[start].excerpt,
		start:   rowRef{side: c.lines[start].side, line: c.lines[start].number},
		end:     rowRef{side: c.lines[end].side, line: c.lines[end].number},
		rows:    end - start + 1,
	}, true
}

func (c stepCursor) inSelection(i int) bool {
	if c.sel < 0 {
		return i == c.cursor
	}
	lo, hi := c.sel, c.cursor
	if lo > hi {
		lo, hi = hi, lo
	}
	return i >= lo && i <= hi
}

var (
	caretSt      = lipgloss.NewStyle().Bold(true).Foreground(accent)                                         // the moving cursor
	selSt        = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#cfe6ff", Dark: "#0a3550"}) // an active selection range
	commentColor = lipgloss.AdaptiveColor{Light: "#8a6d00", Dark: "#ffd787"}                                 // a line carrying a Comment
)

// rowStyle composes the signals of a code row: colour says the line carries a
// Comment, weight says it is the cursor line, and a dim colour says it is
// unchanged reference context. Comment colour and cursor weight compose — a
// commented cursor line is yellow AND bold — so the cursor never hides that a
// line is commented. The dim is the lowest-priority layer: a reference row is
// dimmed only when it is neither commented nor the cursor (and the caller's
// selection background, drawn separately, overrides it too), so context recedes
// without ever hiding a Comment, the cursor, or a selection.
func rowStyle(commented, cursor, reference bool) lipgloss.Style {
	style := lipgloss.NewStyle()
	if commented {
		style = style.Foreground(commentColor)
	} else if reference && !cursor {
		style = style.Foreground(subtle)
	}
	if cursor {
		style = style.Bold(true)
	}
	return style
}

// truncateTo clips a plain (ANSI-free) string to w cells, marking the cut with an
// ellipsis. Code lines are truncated rather than wrapped: a wrapped code line
// throws off the terminal's line accounting and garbles the frame.
func truncateTo(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

// leadingSpaces counts the spaces a line starts with, which is how far a
// continuation row hangs so it sits under the line's own first character rather
// than at the gutter edge. Tabs are expanded before this is called.
func leadingSpaces(s string) int {
	return len(s) - len(strings.TrimLeft(s, " "))
}

// wrapCode soft-wraps a plain (ANSI-free) line of code so the line under the
// cursor can be read in full where a single row would clip it. The first row
// gets first cells and every continuation gets rest, which is narrower by the
// hanging indent the caller will prepend. It caps the result at maxRows chunks;
// when the text overruns that cap the last chunk is truncated with an ellipsis
// exactly as a single clipped line is, so an enormous line cannot grow the pane
// without bound.
//
// Rows break on spaces only (#73). Code has no other reliable word boundary, and
// breaking inside an identifier is what made a wrapped line hard to read back
// against the original. A token with no room anywhere hard-breaks at the row
// edge rather than leaving a ragged gap first.
func wrapCode(s string, first, rest, maxRows int) []string {
	if first < 1 || rest < 1 || maxRows < 1 {
		return []string{truncateTo(s, first)}
	}
	r := []rune(s)
	if len(r) == 0 {
		return []string{""}
	}
	var chunks []string
	w := first
	for len(r) > 0 {
		if len(chunks)+1 == maxRows && len(r) > w {
			// The last row allowed, with more text than fits: clip the remainder.
			return append(chunks, truncateTo(string(r), w))
		}
		n := runesForRow(r, w, rest)
		chunks = append(chunks, string(r[:n]))
		// A break landing inside a run of spaces — gofmt's end-of-line comment
		// alignment, say — would otherwise carry that padding onto the next row and
		// start it at an arbitrary column. Drop it: at a wrap point it is layout,
		// not content, and keeping it is what made the indent look random.
		r = r[skipSpaces(r, n):]
		w = rest
	}
	return chunks
}

// skipSpaces reports the first index at or after i that is not a space, or the
// length of r if there is none.
func skipSpaces(r []rune, i int) int {
	for i < len(r) && r[i] == ' ' {
		i++
	}
	return i
}

// runesForRow reports how many runes of r belong on a row width cells wide,
// given that the row after it is nextWidth cells wide. It prefers the last space
// that fits, and falls back to the row edge in the two cases where a space would
// not help: the token the break would push down is too long for the next row
// anyway and the row edge falls inside it, and the only space available is
// inside the line's own leading indent.
func runesForRow(r []rune, width, nextWidth int) int {
	if len(r) <= width {
		return len(r)
	}
	indent := skipSpaces(r, 0)
	brk := -1
	for i := width; i > indent; i-- {
		if r[i] == ' ' {
			brk = i
			break
		}
	}
	if brk < 0 {
		// Either the row edge is inside a token, or the whole row is the line's own
		// indent — which the caller's hanging indent keeps narrower than the row.
		return width
	}
	// Step back over the whole run of spaces, so the row does not end in padding.
	// The run cannot reach the indent, whose next rune is by definition not a space.
	for brk > 0 && r[brk-1] == ' ' {
		brk--
	}
	// The token that a break here would move down: if it will not fit on the next
	// row either, filling this one costs nothing and wastes no cells — but only
	// while the row edge falls inside the token. Where a run of alignment padding
	// pushes the token past the edge, filling would spend the row on blanks, so
	// the break stands and the token hard-breaks from the next row instead.
	start := skipSpaces(r, brk)
	end := start
	for end < len(r) && r[end] != ' ' {
		end++
	}
	if end-start > nextWidth && width > start {
		return width
	}
	return brk
}

// renderStep draws the Step with the cursor and selection, windowed to height
// rows so a long Step stays navigable, and every row clipped to width so nothing
// overflows the terminal. ackCommentCounts counts the Comments raised in each
// Acknowledgement's files, so a point made in acknowledged code stays visible
// when it is collapsed.
func renderStep(step *daemon.StepWire, cur stepCursor, commented map[string]bool, ackCommentCounts []int, width, height int, showRepo, wrapAll bool) string {
	wrap := func(text string) string {
		if width > 1 {
			return lipgloss.NewStyle().Width(width).Render(text)
		}
		return text
	}
	var b bytes.Buffer
	fmt.Fprint(&b, labelSt.Render(step.Name)+"\n\n"+wrap(step.Explanation)+"\n")
	if step.OversizeJustification != "" {
		// The label goes on its own line above the wrapped justification. Inlining it
		// before the block pushed the first wrapped line past the width, and the text
		// was dropped when the terminal reflowed it.
		fmt.Fprint(&b, "\n"+warnSt.Render("oversized")+"\n"+wrap(step.OversizeJustification)+"\n")
	}

	if step.Stale {
		fmt.Fprint(&b, "\n"+warnSt.Render("⚠ out of date")+"\n")
		fmt.Fprint(&b, wrap("These files changed since the review began, so the code no longer matches the explanation:")+"\n")
		for _, file := range step.StaleFiles {
			fmt.Fprint(&b, dimSt.Render("  • "+file)+"\n")
		}
		fmt.Fprint(&b, "\n"+wrap(dimSt.Render("Ask the agent to re-plan — post a fresh Walkthrough — so this Step describes what is now on disk."))+"\n")
		return b.String()
	}

	// Every Acknowledgement is a stop, so an empty pane means no Acknowledgements
	// and no code that could be read.
	if len(cur.lines) == 0 {
		fmt.Fprint(&b, "\n"+dimSt.Render("no readable code in this Step")+"\n")
		for _, e := range step.Excerpts {
			if e.Problem != "" {
				fmt.Fprint(&b, warnSt.Render("  "+e.File+": ")+e.Problem+"\n")
			}
		}
		return b.String()
	}

	// The pane gets whatever the fixed header (Step name and explanation) leaves.
	// The explanation wins ties: the pane can shrink to a single row, and the
	// Reviewer scrolls or enlarges the terminal, because the explanation is the
	// whole reason the Reviewer is here.
	available := height - lipgloss.Height(b.String())
	if available < 1 {
		available = 1
	}

	// The line under the cursor soft-wraps in place (#26): it is shown in full while
	// every other line stays one truncated row. Its wrapped height is capped at
	// roughly half the code area — with a small floor so even a short pane wraps
	// something — so one enormous line cannot crowd out all the surrounding context;
	// past the cap its last row ends in an ellipsis. The cursor line is reserved its
	// full (capped) height and is never clipped by the window: context shrinks to
	// make room, absorbed into the "more above/below" counts.
	wrapCap := available / 2
	if wrapCap < 3 {
		wrapCap = 3
	}
	if wrapCap > available {
		wrapCap = available
	}
	if wrapAll {
		// Every line wraps in full, so nothing is compared against a clipped tail
		// (#77). The half-pane cap existed to stop one line crowding out the rest;
		// with every line wrapping, the window is what keeps them in proportion.
		//
		// A line is still capped one row past the pane: the cursor moves by source
		// line, so no line can ever show more rows than the pane has, and the row
		// past it is what tells the window there is more below. Without a cap a
		// single generated line would be wrapped and styled in full on every
		// keystroke, for rows that could never be drawn.
		wrapCap = available + 1
	}
	rows := paneRows(step, cur, commented, ackCommentCounts, width, wrapCap, showRepo, wrapAll)

	curFirst, curLast := -1, -1
	for ri, row := range rows {
		if row.line == cur.cursor {
			if curFirst < 0 {
				curFirst = ri
			}
			curLast = ri
		}
	}
	cursorHeight := curLast - curFirst + 1

	// When the pane does not all fit, reserve three rows for scroll indicators: a
	// spacer and the upward arrow above, the downward arrow below. They are always
	// present while scrolling (blank at an edge), so the body height stays constant
	// and the frame below does not shift as the cursor moves. They are reserved
	// only when the cursor line's full height plus the indicators fit, so a very
	// short terminal shows code rather than arrows and nothing.
	const indicatorRows = 3
	overflow := len(rows) > available
	// The cursor line is reserved its full height, so the window never clips it.
	// Once every line wraps (#77) a single line can be taller than the whole pane;
	// then it reserves what the pane has, is held to its first row, and the rest is
	// clipped behind the downward marker.
	reserved := cursorHeight
	if reserved > available {
		reserved = max(1, available-indicatorRows)
	}
	showIndicators := overflow && available >= reserved+indicatorRows
	budget := available
	if showIndicators {
		budget -= indicatorRows
	}
	if budget < reserved {
		budget = reserved
	}

	// Centre the window on the cursor, then make room for the headers that give
	// its top row context — the file it is in and, inside acknowledged code, the
	// Acknowledgement's claim — since the rows that drew them have scrolled off.
	start := curFirst
	if budget > cursorHeight {
		start = curFirst - (budget-cursorHeight)/2
	}
	if start < 0 {
		start = 0
	}
	end := start + budget
	if end > len(rows) {
		end = len(rows)
		start = end - budget
		if start < 0 {
			start = 0
		}
	}
	var sticky []string
	for attempt := 0; attempt < 3; attempt++ {
		sticky = stickyHeaders(rows[start])
		over := len(sticky) + (end - start) - budget
		if over <= 0 {
			break
		}
		cut := curFirst - start
		if cut > over {
			cut = over
		}
		start += cut
		end -= over - cut
		// Keep the cursor line in the window: all of it where the rows the headers
		// left stretch that far, and at least its first row where they do not. The
		// headers can eat the whole budget on a very short pane, so the floor is
		// what stops the window inverting.
		if want := min(curLast+1, start+budget-len(sticky), len(rows)); end < want {
			end = want
		}
		if end <= start {
			end = min(start+1, len(rows))
		}
	}
	// A scroll indicator already sets the pane off from the explanation, so a
	// spacer row right under it would be a wasted row: spend it on the next row.
	if showIndicators && len(sticky) == 0 && start < curFirst && rows[start].blank && len(stickyHeaders(rows[start+1])) == 0 {
		start++
		if end < len(rows) {
			end++
		}
	}

	above, below := countLines(cur, rows[:start]), countLines(cur, rows[end:])
	if showIndicators {
		if above > 0 {
			fmt.Fprint(&b, "\n"+dimSt.Render(fmt.Sprintf("  ↑ %d more above", above))+"\n")
		} else {
			fmt.Fprint(&b, "\n\n")
		}
	}
	for _, header := range sticky {
		fmt.Fprint(&b, header+"\n")
	}
	for _, row := range rows[start:end] {
		fmt.Fprint(&b, row.text+"\n")
	}
	if showIndicators {
		if below > 0 {
			fmt.Fprint(&b, dimSt.Render(fmt.Sprintf("  ↓ %d more below", below))+"\n")
		} else {
			fmt.Fprint(&b, "\n")
		}
	}
	return b.String()
}

// paneRow is one drawn row of a Step's pane, with what the windowing needs to
// know about it: the cursor line it draws (-1 for a spacer, header or manifest
// entry), and the headers that give it context, redrawn at the top of the window
// once the rows that drew them have scrolled off.
type paneRow struct {
	text  string
	line  int
	blank bool
	// fileHeader is the header of the file this row lies in; isFileHeader marks
	// the row that is that header.
	fileHeader   string
	isFileHeader bool
	// ackHeader is the header of the Acknowledgement this row lies in; isAckHeader
	// marks the row that is that header — its stop.
	ackHeader   string
	isAckHeader bool
}

// stickyHeaders are the headers to redraw above a window whose top row is row:
// the Acknowledgement first, since it is the outer context, then the file.
func stickyHeaders(row paneRow) []string {
	var out []string
	if row.ackHeader != "" && !row.isAckHeader {
		out = append(out, "  "+row.ackHeader)
	}
	if row.fileHeader != "" && !row.isFileHeader {
		out = append(out, row.fileHeader)
	}
	return out
}

// countLines counts the cursor lines drawn in rows — code and stops, not notes —
// for the "more above/below" indicators.
func countLines(cur stepCursor, rows []paneRow) int {
	seen := map[int]bool{}
	for _, row := range rows {
		if row.line >= 0 && cur.lines[row.line].kind != kindNote {
			seen[row.line] = true
		}
	}
	return len(seen)
}

// paneRows draws every row of a Step's pane, in order: the Step's own Excerpts,
// then each Acknowledgement — its stop, and then either its manifest or, expanded,
// the code it stands in for, drawn exactly as narrated code is.
func paneRows(step *daemon.StepWire, cur stepCursor, commented map[string]bool, ackCommentCounts []int, width, wrapCap int, showRepo, wrapAll bool) []paneRow {
	var rows []paneRow
	decor := func(text, fileHeader, ackHeader string) {
		rows = append(rows, paneRow{text: text, line: -1, blank: text == "", fileHeader: fileHeader, ackHeader: ackHeader})
	}

	lastAck, lastExcerpt := -2, -2
	fileHeader, ackHeader := "", ""
	for i, line := range cur.lines {
		if line.kind == kindStop {
			ack := step.Acknowledgements[line.ack]
			commentCount := 0
			if line.ack < len(ackCommentCounts) {
				commentCount = ackCommentCounts[line.ack]
			}
			header, truncated := acknowledgementHeader(ack, commentCount, width)
			ackHeader, fileHeader = header, ""
			lastAck, lastExcerpt = line.ack, -1
			decor("", "", "")
			caret := "  "
			if i == cur.cursor {
				caret = caretSt.Render("▸ ")
			}
			rows = append(rows, paneRow{text: caret + header, line: i, ackHeader: header, isAckHeader: true})
			if truncated {
				// The stop had to clip the reason; the claim is what the Reviewer is
				// weighing, so it is shown in full just below.
				for _, row := range strings.Split(wrapIndented("    ", ack.Reason, width), "\n") {
					decor(dimSt.Render(row), "", header)
				}
			}
			if !cur.isExpanded(line.ack) {
				for _, row := range manifestRows(ack, showRepo) {
					decor(row, "", header)
				}
			}
			continue
		}

		if line.ack < 0 {
			ackHeader = ""
		}
		if line.ack != lastAck || line.excerpt != lastExcerpt {
			e := cur.excerptOf(step, line)
			// No side in the header: a new-side Excerpt renders as a unified diff, so
			// the -/+ signs carry before/after per line, not the file header.
			fileHeader = dimSt.Render("── " + fileLabel(e.Repository, e.File, showRepo))
			decor("", "", ackHeader)
			rows = append(rows, paneRow{text: fileHeader, line: -1, fileHeader: fileHeader, isFileHeader: true, ackHeader: ackHeader})
			lastAck, lastExcerpt = line.ack, line.excerpt
		}

		if line.kind == kindNote {
			rows = append(rows, paneRow{
				text: "    " + dimSt.Render(expansionNote(cur.excerptOf(step, line))), line: i,
				fileHeader: fileHeader, ackHeader: ackHeader,
			})
			continue
		}
		file := cur.excerptOf(step, line).File
		for _, text := range codeRows(cur, i, commented[commentKey(file, line.side, line.number)], width, wrapCap, wrapAll) {
			rows = append(rows, paneRow{text: text, line: i, fileHeader: fileHeader, ackHeader: ackHeader})
		}
	}
	return rows
}

// codeRows draws one line of code: a single truncated row, or — under the cursor,
// or on every line once wrapAll is on (#77) — the line soft-wrapped in place
// (#26) so its tail is readable without scrolling, capped at wrapCap rows.
func codeRows(cur stepCursor, i int, hasComment bool, width, wrapCap int, wrapAll bool) []string {
	line := cur.lines[i]
	sign := " "
	if line.changed {
		sign = "+"
		if line.side == "old" {
			sign = "-"
		}
	}
	note := " "
	if hasComment {
		note = "✎"
	}
	text := strings.ReplaceAll(line.text, "\t", "    ") // tabs display wider than one cell
	gutter := fmt.Sprintf("%s%s %5d │ ", note, sign, line.number)
	isCursor := i == cur.cursor

	rows := []string{truncateTo(gutter+text, width-2)} // leave room for the caret
	if isCursor || wrapAll {
		// Continuation rows carry a blank gutter — the missing line number is itself
		// the continuation signal — plus a hanging indent, so they sit under the
		// line's own first character and a wrapped statement reads as one indented
		// block. The hang is capped at half the code column: a deeply nested line
		// would otherwise wrap into a sliver narrower than the indent in front of it.
		textWidth := (width - 2) - lipgloss.Width(gutter)
		hang := leadingSpaces(text)
		if limit := textWidth / 2; hang > limit {
			hang = limit
		}
		restWidth := textWidth - hang
		if textWidth >= 1 && restWidth >= 1 {
			chunks := wrapCode(text, textWidth, restWidth, wrapCap)
			if len(chunks) > 1 {
				indent := strings.Repeat(" ", lipgloss.Width(gutter)+hang)
				rows = []string{gutter + chunks[0]}
				for _, chunk := range chunks[1:] {
					rows = append(rows, indent+chunk)
				}
			}
		}
	}

	// The caret marks only the first row; cursor, comment, and selection styling
	// span every row so the wrapped line reads as one unit.
	selected := cur.sel >= 0 && cur.inSelection(i)
	out := make([]string, 0, len(rows))
	for ri, row := range rows {
		caret := "  "
		if isCursor && ri == 0 {
			caret = caretSt.Render("▸ ")
		}
		if selected {
			out = append(out, caret+selSt.Render(row))
		} else {
			out = append(out, caret+rowStyle(hasComment, isCursor, !line.changed).Render(row))
		}
	}
	return out
}

// acknowledgementHeader is an Acknowledgement's one-row header — its stop, and
// the sticky header over its expanded code: the claim, how much it covers, and
// any Comment raised in it. The reason is clipped to fit the row, and
// truncated reports whether it was.
func acknowledgementHeader(ack daemon.AcknowledgementWire, commentCount, width int) (string, bool) {
	const label = "Acknowledged"
	size := " · " + pluralize(len(ack.Entries), "file")
	raised := ""
	if commentCount > 0 {
		raised = " · " + pluralize(commentCount, "Comment")
	}
	room := width - 2 - len([]rune(label+" — "+size+raised))
	if room < 8 {
		room = 8
	}
	reason := truncateTo(ack.Reason, room)
	header := labelSt.Render(label) + dimSt.Render(" — "+reason+size)
	if raised != "" {
		header += lipgloss.NewStyle().Foreground(commentColor).Render(raised)
	}
	return header, reason != ack.Reason
}

// wrapIndented wraps text to width with every row indented.
func wrapIndented(indent, text string, width int) string {
	if width <= len(indent)+1 {
		return indent + text
	}
	wrapped := lipgloss.NewStyle().Width(width - len(indent)).Render(text)
	return indent + strings.ReplaceAll(wrapped, "\n", "\n"+indent)
}

// manifestRows draws a collapsed Acknowledgement's manifest: the files it stands
// in for, with the size of what it skips — so a bulk claim is never invisible.
// Files are grouped by what git saw happen to them, so a mixed Acknowledgement
// reads as "these were regenerated, that one is binary" at a glance.
func manifestRows(ack daemon.AcknowledgementWire, showRepo bool) []string {
	groups := []struct{ change, label string }{
		{"modified", "modified"},
		{"added", "added"},
		{"removed", "removed"},
		{"rename", "renamed"},
		{"mode", "mode change"},
		{"binary", "binary"},
	}
	var rows []string
	for _, g := range groups {
		var entries []daemon.AcknowledgedFileWire
		for _, entry := range ack.Entries {
			if entry.Change == g.change {
				entries = append(entries, entry)
			}
		}
		if len(entries) == 0 {
			continue
		}
		rows = append(rows, "    "+dimSt.Render(g.label))
		for _, entry := range entries {
			rows = append(rows, "      "+dimSt.Render("• "+fileLabel(entry.Repository, entry.File, showRepo)+manifestSuffix(entry)))
		}
	}
	return rows
}

// manifestSuffix is the trailing detail for a manifest entry: a line count for a
// text file, or the extra detail of an Opaque Change beyond its group label.
func manifestSuffix(entry daemon.AcknowledgedFileWire) string {
	if entry.Opaque != "" {
		if entry.OpaqueDetail != "" && entry.OpaqueDetail != "binary file" {
			return "  ·  " + entry.OpaqueDetail
		}
		return ""
	}
	return "  ·  " + pluralize(entry.ChangedLines, "line")
}

// expansionNote says why an expanded file shows no code: an Opaque Change has no
// lines (it carries no side), and anything else could not be read.
func expansionNote(e daemon.ExcerptWire) string {
	if e.Side == "" {
		return parenthetical(e.Problem, "opaque change")
	}
	return "could not be read: " + e.Problem
}

// parenthetical returns the text between the first "(" and ")" in s, or fallback
// if there is none — used to pull "binary file" out of an Opaque Change's note.
func parenthetical(s, fallback string) string {
	open := strings.IndexByte(s, '(')
	if open < 0 {
		return fallback
	}
	close := strings.IndexByte(s[open:], ')')
	if close < 0 {
		return fallback
	}
	return s[open+1 : open+close]
}

// composeAnchor asks the daemon for the paste-ready Anchor text of a selection.
func (c client) composeAnchor(run selectedRun) (string, bool) {
	body, err := json.Marshal(anchorBody(run))
	if err != nil {
		return "", false
	}
	response, err := http.Post(c.base+"/anchor", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", false
	}
	text, err := io.ReadAll(response.Body)
	if err != nil {
		return "", false
	}
	return string(text), true
}

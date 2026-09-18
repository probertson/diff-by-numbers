package tui

import (
	"bytes"
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

// codeLine is one selectable line of a Step: which Excerpt it belongs to, its
// file line number, and its text. Selection works within a single Excerpt.
type codeLine struct {
	excerpt int
	number  int
	side    string
	text    string
	changed bool
}

// stepCursor holds the selection state over a Step's code lines. It is rebuilt
// whenever the Step in view changes.
type stepCursor struct {
	lines  []codeLine
	cursor int // index into lines
	sel    int // selection start index, or -1 for none
}

func newStepCursor(step *daemon.StepWire) stepCursor {
	var lines []codeLine
	for ei, excerpt := range step.Excerpts {
		for _, line := range excerpt.Lines {
			lines = append(lines, codeLine{
				excerpt: ei, number: line.Number, side: lineSide(line, excerpt), text: line.Text, changed: line.Changed,
			})
		}
	}
	return stepCursor{lines: lines, cursor: 0, sel: -1}
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

func (c *stepCursor) move(delta int) {
	if len(c.lines) == 0 {
		return
	}
	c.cursor += delta
	if c.cursor < 0 {
		c.cursor = 0
	}
	if c.cursor >= len(c.lines) {
		c.cursor = len(c.lines) - 1
	}
}

// extend grows the selection by moving the cursor, dropping an anchor at the
// current line first if none is set. It backs shift+arrow, the editor-conventional
// way to select a range without first pressing a select key.
func (c *stepCursor) extend(delta int) {
	if len(c.lines) == 0 {
		return
	}
	if c.sel < 0 {
		c.sel = c.cursor
	}
	c.move(delta)
}

func (c *stepCursor) toggleSelect() {
	if c.sel == c.cursor {
		c.sel = -1
		return
	}
	c.sel = c.cursor
}

// selection returns the excerpt index, line range, and side currently selected,
// and whether the selection is valid: non-empty, within one Excerpt, and on one
// side (a unified diff mixes before- and after-side rows, and an Anchor is to one
// side).
func (c stepCursor) selection() (excerpt, first, last int, side string, ok bool) {
	if len(c.lines) == 0 {
		return 0, 0, 0, "", false
	}
	start := c.cursor
	if c.sel >= 0 {
		start = c.sel
	}
	end := c.cursor
	if start > end {
		start, end = end, start
	}
	if c.lines[start].excerpt != c.lines[end].excerpt {
		return 0, 0, 0, "", false // a selection may not straddle two Excerpts
	}
	if c.lines[start].side != c.lines[end].side {
		return 0, 0, 0, "", false // nor two sides of a unified diff
	}
	return c.lines[start].excerpt, c.lines[start].number, c.lines[end].number, c.lines[start].side, true
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
	commentColor = lipgloss.AdaptiveColor{Light: "#8a6d00", Dark: "#ffd787"}                                 // a line carrying a Change Request
)

// rowStyle composes the signals of a code row: colour says the line carries a
// Change Request, weight says it is the cursor line, and a dim colour says it is
// unchanged reference context. Comment colour and cursor weight compose — a
// commented cursor line is yellow AND bold — so the cursor never hides that a
// line is commented. The dim is the lowest-priority layer: a reference row is
// dimmed only when it is neither commented nor the cursor (and the caller's
// selection background, drawn separately, overrides it too), so context recedes
// without ever hiding a Change Request, the cursor, or a selection.
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

// wrapRunes hard-wraps a plain (ANSI-free) string so the line under the cursor
// can be read in full where a single row would clip it. The first row gets first
// cells and every continuation gets rest, which is narrower by the hanging
// indent the caller will prepend. It caps the result at maxRows chunks; when the
// text overruns that cap the last chunk is truncated with an ellipsis exactly as
// a single clipped line is, so an enormous line cannot grow the pane without
// bound. It wraps on runes rather than words: code has no reliable word
// boundaries, and a full row is easier to read back against the original than a
// ragged one.
func wrapRunes(s string, first, rest, maxRows int) []string {
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
		n := w
		if n > len(r) {
			n = len(r)
		}
		chunks = append(chunks, string(r[:n]))
		r = r[n:]
		// A break landing inside a run of spaces — gofmt's end-of-line comment
		// alignment, say — would otherwise carry that padding onto the next row and
		// start it at an arbitrary column. Drop it: at a wrap point it is layout,
		// not content, and keeping it is what made the indent look random.
		for len(r) > 0 && r[0] == ' ' {
			r = r[1:]
		}
		w = rest
	}
	return chunks
}

// renderStep draws the Step with the cursor and selection, windowed to height
// rows so a long Step stays navigable, and every row clipped to width so nothing
// overflows the terminal.
func renderStep(step *daemon.StepWire, cur stepCursor, commented map[string]bool, width, height int, showRepo bool) string {
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

	if len(cur.lines) == 0 && len(step.Acknowledgements) == 0 {
		fmt.Fprint(&b, "\n"+dimSt.Render("no readable code in this Step")+"\n")
		for _, e := range step.Excerpts {
			if e.Problem != "" {
				fmt.Fprint(&b, warnSt.Render("  "+e.File+": ")+e.Problem+"\n")
			}
		}
		return b.String()
	}

	if len(cur.lines) == 0 {
		// An Acknowledgement-only Step: no code lines, just the manifest.
		fmt.Fprint(&b, renderManifest(step, width, showRepo))
		return b.String()
	}

	// Window the code lines so the cursor stays on screen — but budget the window
	// against everything else the body will hold: the fixed header already written
	// (Step name and explanation), one line per Excerpt file header, and the
	// manifest hint if there are Acknowledgements. Otherwise a long explanation
	// pushes itself off the top of the screen, and the explanation is the whole
	// reason the Reviewer is here. The explanation wins ties: the code window can
	// shrink to a single line, and the Reviewer scrolls or enlarges the terminal.
	// Each Excerpt shown costs two rows (a blank spacer and the file header); the
	// manifest hint below costs two more when there are Acknowledgements.
	reserve := lipgloss.Height(b.String()) + 2*len(step.Excerpts)
	if len(step.Acknowledgements) > 0 {
		reserve += 2 // the "press x to expand" hint
	}
	available := height - reserve
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
	cursorCap := available / 2
	if cursorCap < 3 {
		cursorCap = 3
	}
	if cursorCap > available {
		cursorCap = available
	}
	cursorLine := cur.lines[cur.cursor]
	cursorText := strings.ReplaceAll(cursorLine.text, "\t", "    ")
	cursorGutter := fmt.Sprintf("%s%s %5d │ ", " ", " ", cursorLine.number)
	cursorTextWidth := (width - 2) - lipgloss.Width(cursorGutter)
	// Continuations hang under the line's own first character rather than at the
	// gutter edge, so a wrapped statement reads as one indented block. The hang is
	// capped at half the code column: a deeply nested line would otherwise wrap
	// into a sliver narrower than the indent in front of it.
	cursorHang := leadingSpaces(cursorText)
	if limit := cursorTextWidth / 2; cursorHang > limit {
		cursorHang = limit
	}
	cursorRestWidth := cursorTextWidth - cursorHang
	cursorRows := 1
	if cursorTextWidth >= 1 && cursorRestWidth >= 1 {
		cursorRows = len(wrapRunes(cursorText, cursorTextWidth, cursorRestWidth, cursorCap))
	}

	// When the code does not all fit, reserve two rows for scroll indicators. They
	// are always present while scrolling (blank at an edge), so the body height
	// stays constant and the frame below does not shift as the cursor moves. The
	// cursor line's extra wrapped rows count against the budget, so a long line
	// pushes more context off the edges rather than overflowing the frame.
	overflow := len(cur.lines)-1+cursorRows > available
	// The indicators cost two rows; only reserve them when the cursor line's full
	// height plus both arrows fit, so a very short terminal shows code rather than
	// two arrows and nothing.
	showIndicators := overflow && available >= cursorRows+2
	rowBudget := available
	if showIndicators {
		rowBudget -= 2
	}
	// Lines to show: the cursor (cursorRows rows) plus as many one-row lines as the
	// remaining budget holds.
	window := rowBudget - (cursorRows - 1)
	if window < 1 {
		window = 1
	}

	start := cur.cursor - window/2
	if start < 0 {
		start = 0
	}
	end := start + window
	if end > len(cur.lines) {
		end = len(cur.lines)
		start = end - window
		if start < 0 {
			start = 0
		}
	}

	if showIndicators {
		// The blank goes above the indicator so it separates from the explanation
		// rather than blending into it; the first Excerpt header then follows the
		// indicator directly. The row count is the same either way.
		if start > 0 {
			fmt.Fprint(&b, "\n"+dimSt.Render(fmt.Sprintf("  ↑ %d more above", start))+"\n")
		} else {
			fmt.Fprint(&b, "\n\n")
		}
	}

	lastExcerpt := -1
	firstHeader := true
	for i := start; i < end; i++ {
		line := cur.lines[i]
		if line.excerpt != lastExcerpt {
			e := step.Excerpts[line.excerpt]
			// A blank line normally sets the header off from what is above it, but
			// when a scroll indicator was just drawn it already did that.
			sep := "\n"
			if firstHeader && showIndicators {
				sep = ""
			}
			// No side in the header: a new-side Excerpt renders as a unified diff, so
			// the -/+ signs carry before/after per line, not the file header.
			fmt.Fprint(&b, sep+dimSt.Render(fmt.Sprintf("── %s", fileLabel(e.Repository, e.File, showRepo)))+"\n")
			lastExcerpt = line.excerpt
			firstHeader = false
		}
		sign := " "
		if line.changed {
			sign = "+"
			if line.side == "old" {
				sign = "-"
			}
		}
		note := " "
		if commented[commentKey(step.Excerpts[line.excerpt].File, line.side, line.number)] {
			note = "✎"
		}
		text := strings.ReplaceAll(line.text, "\t", "    ") // tabs display wider than one cell
		hasComment := note == "✎"
		selected := cur.sel >= 0 && cur.inSelection(i)

		var rows []string
		if i == cur.cursor && cursorRows > 1 {
			// The cursor line soft-wraps in place so its tail is readable without
			// scrolling. Continuation rows carry a blank gutter — the missing line
			// number is itself the continuation signal — plus the hanging indent, so
			// they sit under the line's own first character.
			gutter := fmt.Sprintf("%s%s %5d │ ", note, sign, line.number)
			indent := strings.Repeat(" ", lipgloss.Width(gutter)+cursorHang)
			for ci, chunk := range wrapRunes(text, cursorTextWidth, cursorRestWidth, cursorCap) {
				if ci == 0 {
					rows = append(rows, gutter+chunk)
				} else {
					rows = append(rows, indent+chunk)
				}
			}
		} else {
			row := fmt.Sprintf("%s%s %5d │ %s", note, sign, line.number, text)
			rows = append(rows, truncateTo(row, width-2)) // leave room for the caret
		}

		// The caret marks only the first row; cursor, comment, and selection styling
		// span every row so the wrapped line reads as one unit.
		for ri, row := range rows {
			caret := "  "
			if i == cur.cursor && ri == 0 {
				caret = caretSt.Render("▸ ")
			}
			if selected {
				fmt.Fprint(&b, caret+selSt.Render(row)+"\n")
			} else {
				fmt.Fprint(&b, caret+rowStyle(hasComment, i == cur.cursor, !line.changed).Render(row)+"\n")
			}
		}
	}
	if showIndicators {
		if end < len(cur.lines) {
			fmt.Fprint(&b, dimSt.Render(fmt.Sprintf("  ↓ %d more below", len(cur.lines)-end))+"\n")
		} else {
			fmt.Fprint(&b, "\n")
		}
	}
	if len(step.Acknowledgements) > 0 {
		fmt.Fprint(&b, renderManifest(step, width, showRepo))
	}
	return b.String()
}

// renderManifest draws a Step's Acknowledgements as what they are: a claim the
// Reviewer can weigh and call. Each names its reason and the files it stands in
// for, with the size of what it skips — so a bulk claim is never invisible.
func renderManifest(step *daemon.StepWire, width int, showRepo bool) string {
	wrap := func(text string) string {
		if width > 1 {
			return lipgloss.NewStyle().Width(width).Render(text)
		}
		return text
	}
	// Files are grouped by what git saw happen to them, so a mixed Acknowledgement
	// reads as "these were regenerated, that one is binary" at a glance.
	groups := []struct{ change, label string }{
		{"modified", "modified"},
		{"added", "added"},
		{"removed", "removed"},
		{"rename", "renamed"},
		{"mode", "mode change"},
		{"binary", "binary"},
	}

	var b bytes.Buffer
	for _, ack := range step.Acknowledgements {
		fmt.Fprint(&b, "\n"+labelSt.Render("Acknowledged")+dimSt.Render(" — mechanical, not read line by line")+"\n")
		fmt.Fprint(&b, wrap(ack.Reason)+"\n")
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
			fmt.Fprint(&b, "  "+dimSt.Render(g.label)+"\n")
			for _, entry := range entries {
				fmt.Fprint(&b, "    "+dimSt.Render("• "+fileLabel(entry.Repository, entry.File, showRepo)+manifestSuffix(entry))+"\n")
			}
		}
	}
	fmt.Fprint(&b, "\n"+dimSt.Render("press x to expand into the actual code")+"\n")
	return b.String()
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

// renderExpanded draws the code behind an Acknowledgement once the Reviewer calls
// it — read-only, since the expansion is a look, not part of the plan. Excerpts
// are grouped by file under an orange header, then split into before/after; it is
// capped to height rows so expanding a huge lockfile does not blow the frame.
func renderExpanded(excerpts []daemon.ExcerptWire, width, height int, showRepo bool) string {
	var b bytes.Buffer
	fmt.Fprint(&b, labelSt.Render("Expanded")+dimSt.Render(" — the acknowledged code (x to collapse)")+"\n")
	if len(excerpts) == 0 {
		fmt.Fprint(&b, "\n"+dimSt.Render("nothing to show")+"\n")
		return b.String()
	}

	wrap := func(indent, text string) string {
		if width > len(indent)+1 {
			return lipgloss.NewStyle().Width(width).Render(indent + text)
		}
		return indent + text
	}

	// Group by repository+file, preserving first-seen order. The key includes the
	// repository so two repos with a same-named file are not merged into one.
	type fileKey struct{ repository, file string }
	var order []fileKey
	byFile := map[fileKey][]daemon.ExcerptWire{}
	for _, e := range excerpts {
		key := fileKey{e.Repository, e.File}
		if _, seen := byFile[key]; !seen {
			order = append(order, key)
		}
		byFile[key] = append(byFile[key], e)
	}

	shown, budget := 0, height
	if budget < 4 {
		budget = 4
	}

	for _, key := range order {
		group := byFile[key]
		fmt.Fprint(&b, "\n"+warnSt.Render("── "+fileLabel(key.repository, key.file, showRepo))+"\n")

		hasBefore, hasAfter, opaque, opaqueNote := false, false, false, "opaque change"
		for _, e := range group {
			switch e.Side {
			case "old":
				hasBefore = true
			case "new":
				hasAfter = true
			default: // an Opaque Change carries no side
				opaque = true
				opaqueNote = parenthetical(e.Problem, opaqueNote)
			}
		}

		if opaque {
			fmt.Fprint(&b, wrap("  ", dimSt.Render(opaqueNote))+"\n")
			continue
		}
		// drawSide draws one side's resolved lines, marked, respecting the height
		// budget. It reports false when the budget is spent and the whole expansion
		// should stop.
		drawSide := func(side, sign string) bool {
			for _, e := range group {
				if e.Side != side {
					continue
				}
				for _, line := range e.Lines {
					if shown >= budget {
						fmt.Fprint(&b, "  "+dimSt.Render("… more not shown; collapse and read it as an Excerpt in full")+"\n")
						return false
					}
					mark := " "
					if line.Changed {
						mark = sign
					}
					text := strings.ReplaceAll(line.Text, "\t", "    ")
					row := fmt.Sprintf("    %s %5d │ %s", mark, line.Number, text)
					// Read-only view: no cursor or comment here, so this dims plain
					// reference rows and leaves changed rows at normal brightness.
					fmt.Fprint(&b, rowStyle(false, false, !line.Changed).Render(truncateTo(row, width))+"\n")
					shown++
				}
			}
			return true
		}

		if hasBefore && !hasAfter {
			fmt.Fprint(&b, "  "+dimSt.Render("file removed")+"\n")
			if !drawSide("old", "-") {
				return b.String()
			}
			continue
		}

		// before
		fmt.Fprint(&b, "  "+dimSt.Render("before")+"\n")
		if hasBefore {
			if !drawSide("old", "-") {
				return b.String()
			}
		} else {
			fmt.Fprint(&b, "    "+dimSt.Render("file added")+"\n")
		}

		// after
		fmt.Fprint(&b, "  "+dimSt.Render("after")+"\n")
		if !drawSide("new", "+") {
			return b.String()
		}
	}
	return b.String()
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
func (c client) composeAnchor(excerpt, first, last int, side string) (string, bool) {
	body := fmt.Sprintf(`{"excerpt_index":%d,"first_line":%d,"last_line":%d,"side":%q}`, excerpt, first, last, side)
	response, err := http.Post(c.base+"/anchor", "application/json", bytes.NewReader([]byte(body)))
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

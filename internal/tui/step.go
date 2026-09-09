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
				excerpt: ei, number: line.Number, side: excerpt.Side, text: line.Text, changed: line.Changed,
			})
		}
	}
	return stepCursor{lines: lines, cursor: 0, sel: -1}
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

func (c *stepCursor) toggleSelect() {
	if c.sel == c.cursor {
		c.sel = -1
		return
	}
	c.sel = c.cursor
}

// selection returns the excerpt index and line range currently selected, and
// whether the selection is valid (non-empty and within one Excerpt).
func (c stepCursor) selection() (excerpt, first, last int, ok bool) {
	if len(c.lines) == 0 {
		return 0, 0, 0, false
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
		return 0, 0, 0, false // a selection may not straddle two Excerpts
	}
	return c.lines[start].excerpt, c.lines[start].number, c.lines[end].number, true
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
	cursorLineSt = lipgloss.NewStyle().Bold(true)                                                            // cursor line, no selection
	selSt        = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#cfe6ff", Dark: "#0a3550"}) // an active selection range
	commentSt    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#8a6d00", Dark: "#ffd787"}) // a line carrying a Change Request
)

// beforeAfter renders a Side for the Reviewer: "old"/"new" are git's words, but
// "before"/"after" read more plainly on screen.
func beforeAfter(side string) string {
	switch side {
	case "old":
		return "before"
	case "new":
		return "after"
	default:
		return side
	}
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
		fmt.Fprint(&b, "\n"+warnSt.Render("oversized: ")+wrap(step.OversizeJustification)+"\n")
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
	window := height - reserve
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

	lastExcerpt := -1
	for i := start; i < end; i++ {
		line := cur.lines[i]
		if line.excerpt != lastExcerpt {
			e := step.Excerpts[line.excerpt]
			fmt.Fprint(&b, "\n"+dimSt.Render(fmt.Sprintf("── %s (%s)", fileLabel(e.Repository, e.File, showRepo), beforeAfter(e.Side)))+"\n")
			lastExcerpt = line.excerpt
		}
		sign := " "
		if line.changed {
			sign = "+"
			if line.side == "old" {
				sign = "-"
			}
		}
		note := " "
		if commented[fmt.Sprintf("%s:%d", step.Excerpts[line.excerpt].File, line.number)] {
			note = "✎"
		}
		text := strings.ReplaceAll(line.text, "\t", "    ") // tabs display wider than one cell
		row := fmt.Sprintf("%s%s %5d │ %s", note, sign, line.number, text)
		row = truncateTo(row, width-2) // leave room for the caret
		caret := "  "
		if i == cur.cursor {
			caret = caretSt.Render("▸ ")
		}
		selActive := cur.sel >= 0
		switch {
		case selActive && cur.inSelection(i):
			fmt.Fprint(&b, caret+selSt.Render(row)+"\n")
		case i == cur.cursor:
			fmt.Fprint(&b, caret+cursorLineSt.Render(row)+"\n")
		case note == "✎":
			fmt.Fprint(&b, caret+commentSt.Render(row)+"\n")
		default:
			fmt.Fprint(&b, caret+row+"\n")
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
	return fmt.Sprintf("  ·  %d line(s)", entry.ChangedLines)
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
		if hasBefore && !hasAfter {
			removed := 0
			for _, e := range group {
				if e.Side == "old" {
					removed += e.LastLine - e.FirstLine + 1
				}
			}
			fmt.Fprint(&b, wrap("  ", dimSt.Render(fmt.Sprintf("file removed · %d line(s); the before-side lives in git history, not shown", removed)))+"\n")
			continue
		}

		// before
		fmt.Fprint(&b, "  "+dimSt.Render("before")+"\n")
		if hasBefore {
			fmt.Fprint(&b, wrap("    ", dimSt.Render("not shown — dbn reads only the working tree; the before-side lives in git history"))+"\n")
		} else {
			fmt.Fprint(&b, "    "+dimSt.Render("file added")+"\n")
		}

		// after
		fmt.Fprint(&b, "  "+dimSt.Render("after")+"\n")
		for _, e := range group {
			if e.Side != "new" {
				continue
			}
			for _, line := range e.Lines {
				if shown >= budget {
					fmt.Fprint(&b, "  "+dimSt.Render("… more not shown; collapse and read it as an Excerpt in full")+"\n")
					return b.String()
				}
				sign := " "
				if line.Changed {
					sign = "+"
				}
				text := strings.ReplaceAll(line.Text, "\t", "    ")
				row := fmt.Sprintf("    %s %5d │ %s", sign, line.Number, text)
				fmt.Fprint(&b, truncateTo(row, width)+"\n")
				shown++
			}
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
func (c client) composeAnchor(excerpt, first, last int) (string, bool) {
	body := fmt.Sprintf(`{"excerpt_index":%d,"first_line":%d,"last_line":%d}`, excerpt, first, last)
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

package tui

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

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
func renderStep(step *daemon.StepWire, cur stepCursor, commented map[string]bool, width, height int) string {
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
		fmt.Fprint(&b, renderManifest(step, width))
		return b.String()
	}

	// Window the code lines so the cursor stays on screen.
	window := height
	if window < 4 {
		window = 4
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
			fmt.Fprint(&b, "\n"+dimSt.Render(fmt.Sprintf("── %s (%s side)", e.File, e.Side))+"\n")
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
		fmt.Fprint(&b, renderManifest(step, width))
	}
	return b.String()
}

// renderManifest draws a Step's Acknowledgements as what they are: a claim the
// Reviewer can weigh and call. Each names its reason and the files it stands in
// for, with the size of what it skips — so a bulk claim is never invisible.
func renderManifest(step *daemon.StepWire, width int) string {
	wrap := func(text string) string {
		if width > 1 {
			return lipgloss.NewStyle().Width(width).Render(text)
		}
		return text
	}
	var b bytes.Buffer
	for _, ack := range step.Acknowledgements {
		fmt.Fprint(&b, "\n"+labelSt.Render("Acknowledged")+dimSt.Render(" — mechanical, not read line by line")+"\n")
		fmt.Fprint(&b, wrap(ack.Reason)+"\n")
		for _, entry := range ack.Entries {
			size := fmt.Sprintf("%d changed line(s)", entry.ChangedLines)
			if entry.Opaque != "" {
				size = entry.Opaque
				if entry.OpaqueDetail != "" {
					size = entry.OpaqueDetail
				}
			}
			fmt.Fprint(&b, dimSt.Render(fmt.Sprintf("  • %s  (%s)", entry.File, size))+"\n")
		}
	}
	fmt.Fprint(&b, "\n"+dimSt.Render("press x to expand into the actual code")+"\n")
	return b.String()
}

// renderExpanded draws the code behind an Acknowledgement once the Reviewer calls
// it — read-only, since the expansion is a look, not part of the plan. It is
// capped to height rows so expanding a huge lockfile does not blow the frame.
func renderExpanded(excerpts []daemon.ExcerptWire, width, height int) string {
	var b bytes.Buffer
	fmt.Fprint(&b, labelSt.Render("Expanded")+dimSt.Render(" — the acknowledged code (x to collapse)")+"\n")
	if len(excerpts) == 0 {
		fmt.Fprint(&b, "\n"+dimSt.Render("nothing to show")+"\n")
		return b.String()
	}
	shown, budget := 0, height
	if budget < 4 {
		budget = 4
	}
	for _, excerpt := range excerpts {
		fmt.Fprint(&b, "\n"+dimSt.Render(fmt.Sprintf("── %s (%s side)", excerpt.File, excerpt.Side))+"\n")
		if excerpt.Problem != "" {
			fmt.Fprint(&b, warnSt.Render("  "+excerpt.File+": ")+excerpt.Problem+"\n")
			continue
		}
		for _, line := range excerpt.Lines {
			if shown >= budget {
				fmt.Fprint(&b, dimSt.Render("  … more not shown; collapse and use the Excerpts to review in full")+"\n")
				return b.String()
			}
			sign := " "
			if line.Changed {
				sign = "+"
				if excerpt.Side == "old" {
					sign = "-"
				}
			}
			text := strings.ReplaceAll(line.Text, "\t", "    ")
			row := fmt.Sprintf(" %s %5d │ %s", sign, line.Number, text)
			fmt.Fprint(&b, truncateTo(row, width)+"\n")
			shown++
		}
	}
	return b.String()
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

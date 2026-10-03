// PROTOTYPE — throwaway, not dbn code. Answers "What does a document Step look
// like?" (probertson/diff-by-numbers#138).
//
// Four variants of one Step pane showing a Document, switchable with ←/→, over
// two real, public Steps: a new section of a requirements document (every line
// an addition) and a rewrite of existing glossary prose (removals and
// additions, partly rewrapped). Run: go run ./cmd/proto-docstep
package main

import (
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/sergi/go-diff/diffmatchpatch"
)

//go:embed data/requirements-background.md
var requirementsText string

//go:embed data/glossary-before.md
var glossaryBefore string

//go:embed data/glossary-after.md
var glossaryAfter string

// ---- palette, copied from internal/tui so the variants look like dbn ----

var (
	addTint     = lipgloss.AdaptiveColor{Light: "#e3f5e3", Dark: "#213a21"}
	addEmphasis = lipgloss.AdaptiveColor{Light: "#abe2ab", Dark: "#2f6b2f"}
	delTint     = lipgloss.AdaptiveColor{Light: "#fae3e3", Dark: "#3f2222"}
	delEmphasis = lipgloss.AdaptiveColor{Light: "#f0a8a8", Dark: "#7a2f2f"}
	addColour   = lipgloss.AdaptiveColor{Light: "#207520", Dark: "#87d787"}
	delColour   = lipgloss.AdaptiveColor{Light: "#a01010", Dark: "#ff8787"}
	subtle      = lipgloss.AdaptiveColor{Light: "#6b6b6b", Dark: "#9a9a9a"}
	accent      = lipgloss.AdaptiveColor{Light: "#005f87", Dark: "#5fd7ff"}
	codeColour  = lipgloss.AdaptiveColor{Light: "#875f00", Dark: "#d7af5f"}
	cursorBg    = lipgloss.AdaptiveColor{Light: "#d0d0d0", Dark: "#4e4e4e"}
	quietBg     = lipgloss.AdaptiveColor{Light: "#ececec", Dark: "#303030"}
	changeBar   = lipgloss.AdaptiveColor{Light: "#af8700", Dark: "#d7af00"}
	dimBody     = lipgloss.AdaptiveColor{Light: "#626262", Dark: "#a8a8a8"}
	markerHue   = lipgloss.AdaptiveColor{Light: "#8787af", Dark: "#7878a8"}
	newDim      = lipgloss.AdaptiveColor{Light: "#5f875f", Dark: "#9cb89c"}
	newBright   = lipgloss.AdaptiveColor{Light: "#2f5f2f", Dark: "#d4f0d4"}

	plainSt  = lipgloss.NewStyle()
	dimSt    = lipgloss.NewStyle().Foreground(subtle)
	headerSt = lipgloss.NewStyle().Bold(true).Foreground(accent)
	caretSt  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	barSt    = lipgloss.NewStyle().Reverse(true).Bold(true)

	// Variant 6 dims the text of every block but the cursor's, and draws the
	// Markdown markers in their own muted hue so they stay distinct from it.
	bodySt               = plainSt
	markerSt             = dimSt
	renderIdx, renderSel int
	renderNew, tintNew   bool // the block being drawn is purely new; tint such blocks grey-green
)

// ---- diffing ----

type op int

const (
	eq op = iota
	del
	ins
)

type tokenOp struct {
	op  op
	tok string
}

// diffTokens diffs two token sequences by mapping each distinct token to a rune.
func diffTokens(a, b []string) []tokenOp {
	index := map[string]rune{}
	var table []string
	encode := func(ts []string) []rune {
		out := make([]rune, len(ts))
		for i, t := range ts {
			r, ok := index[t]
			if !ok {
				r = rune(0xE000 + len(table))
				index[t] = r
				table = append(table, t)
			}
			out[i] = r
		}
		return out
	}
	ra, rb := encode(a), encode(b)
	dmp := diffmatchpatch.New()
	var out []tokenOp
	for _, d := range dmp.DiffMainRunes(ra, rb, false) {
		o := eq
		if d.Type == diffmatchpatch.DiffDelete {
			o = del
		} else if d.Type == diffmatchpatch.DiffInsert {
			o = ins
		}
		for _, r := range d.Text {
			out = append(out, tokenOp{o, table[r-0xE000]})
		}
	}
	return out
}

// ---- source rows (variants 1 and 2) ----

type srcRow struct {
	op           op
	text         string
	oldNo, newNo int
}

func lines(s string) []string { return strings.Split(strings.TrimRight(s, "\n"), "\n") }

func sourceRows(before, after []string, beforeStart, afterStart int) []srcRow {
	var rows []srcRow
	o, n := beforeStart, afterStart
	for _, t := range diffTokens(before, after) {
		switch t.op {
		case eq:
			rows = append(rows, srcRow{eq, t.tok, o, n})
			o++
			n++
		case del:
			rows = append(rows, srcRow{del, t.tok, o, 0})
			o++
		case ins:
			rows = append(rows, srcRow{ins, t.tok, 0, n})
			n++
		}
	}
	return rows
}

// ---- Markdown blocks (variants 3 and 4) ----

type block struct {
	kind        string // front, heading, para, list, quote
	src         []string
	first, last int
}

var listRe = regexp.MustCompile(`^\s*([-*+]|\d+\.)\s`)

func startsBlock(l string) bool {
	t := strings.TrimSpace(l)
	return strings.HasPrefix(t, "#") || strings.HasPrefix(t, ">") || strings.HasPrefix(t, "```") || listRe.MatchString(l)
}

func parseBlocks(ls []string, start int) []block {
	var bs []block
	i := 0
	if len(ls) > 0 && ls[0] == "---" {
		j := 1
		for j < len(ls) && ls[j] != "---" {
			j++
		}
		bs = append(bs, block{"front", ls[0 : j+1], start, start + j})
		i = j + 1
	}
	for i < len(ls) {
		l := ls[i]
		if strings.TrimSpace(l) == "" {
			i++
			continue
		}
		j := i + 1
		kind := "para"
		switch {
		case strings.HasPrefix(l, "#"):
			kind = "heading"
		case strings.HasPrefix(l, ">"):
			kind = "quote"
			for j < len(ls) && strings.HasPrefix(ls[j], ">") {
				j++
			}
		case listRe.MatchString(l):
			kind = "list"
			for j < len(ls) && strings.TrimSpace(ls[j]) != "" && !listRe.MatchString(ls[j]) && !startsBlock(ls[j]) {
				j++
			}
		default:
			for j < len(ls) && strings.TrimSpace(ls[j]) != "" && !startsBlock(ls[j]) {
				j++
			}
		}
		bs = append(bs, block{kind, ls[i:j], start + i, start + j - 1})
		i = j
	}
	return bs
}

// words is a block's text as reflowable words; list markers and heading hashes
// stay in the text so they diff like any other word.
func (b block) words() []string {
	if b.kind == "front" || b.kind == "quote" {
		return []string{strings.Join(b.src, "\n")}
	}
	return strings.Fields(strings.Join(b.src, " "))
}

// ---- inline Markdown styling ----

type word struct {
	text string
	op   op
}

type inline struct{ bold, em, code bool }

// renderWord styles one word, tracking emphasis and code spans across words.
// hide drops the Markdown markers; otherwise they are drawn dim.
func renderWord(w string, st *inline, hide bool, wop op, mode int) string {
	var b strings.Builder
	rs := []rune(w)
	piece := func(s string, marker bool) {
		if s == "" {
			return
		}
		style := bodySt
		if marker {
			if hide {
				return
			}
			style = markerSt
		} else {
			if st.bold {
				style = style.Bold(true)
			}
			if st.em {
				style = style.Italic(true)
			}
			if st.code {
				style = style.Foreground(codeColour)
			}
		}
		style = withOp(style, wop, mode)
		b.WriteString(style.Render(s))
	}
	var run []rune
	flush := func() { piece(string(run), false); run = nil }
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '`':
			flush()
			st.code = !st.code
			piece("`", true)
		case !st.code && r == '*' && i+1 < len(rs) && rs[i+1] == '*':
			flush()
			st.bold = !st.bold
			piece("**", true)
			i++
		case !st.code && (r == '*' || (r == '_' && (i == 0 || i == len(rs)-1 || !isLetter(rs[i+1])))):
			flush()
			st.em = !st.em
			piece(string(r), true)
		default:
			run = append(run, r)
		}
	}
	flush()
	return b.String()
}

func isLetter(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }

// Variant 3 marks changed words with dbn's emphasis tints; variant 4 marks
// them track-changes style: struck removals, underlined additions; variant 5
// colours the text itself, red and struck or green, with no background.
func withOp(s lipgloss.Style, o op, mode int) lipgloss.Style {
	switch {
	case o == del && mode == 3:
		return s.Background(delEmphasis)
	case o == ins && mode == 3:
		return s.Background(addEmphasis)
	case o == del && (mode == 4 || mode == 6):
		return s.Foreground(delColour).Strikethrough(true)
	case o == ins && (mode == 4 || mode == 6):
		return s.Foreground(addColour).Underline(true)
	case o == del && mode == 5:
		return s.Foreground(delColour).Strikethrough(true)
	case o == ins && (mode == 5 || mode == 7):
		return s.Foreground(addColour)
	case o == del && mode == 7:
		return s.Foreground(delColour)
	}
	return s
}

// fill lays styled words into rows no wider than room.
func fill(ws []word, room int, hide bool, mode int) []string {
	var rows []string
	st := &inline{}
	cur, curW := "", 0
	for i, w := range ws {
		s := renderWord(w.text, st, hide, w.op, mode)
		sw := lipgloss.Width(s)
		if sw == 0 {
			continue
		}
		if curW > 0 && curW+1+sw > room {
			rows = append(rows, cur)
			cur, curW = "", 0
		}
		if curW > 0 {
			space := " "
			if i > 0 && ws[i-1].op == w.op && w.op != eq {
				space = withOp(plainSt, w.op, mode).Render(" ")
			}
			cur += space
			curW++
		}
		cur += s
		curW += sw
	}
	if curW > 0 || len(rows) == 0 {
		rows = append(rows, cur)
	}
	return rows
}

// ---- items: what the cursor moves over ----

type item struct {
	rows  []string // content rows, without gutter
	label string   // gutter: source lines
	bar   lipgloss.Style
	barCh string
	tint  lipgloss.TerminalColor // whole-row background, or nil
	kind  string                 // variant 6's gutter: new, mixed, removed, or "" for context
}

// renderBlock draws one block of words in variant 3 (markers dim) or 4 (hidden).
func renderBlock(kind string, src []string, ws []word, room, mode int) []string {
	bodySt, markerSt = plainSt, dimSt
	if mode == 6 {
		markerSt = lipgloss.NewStyle().Foreground(markerHue)
		if renderIdx != renderSel {
			bodySt = lipgloss.NewStyle().Foreground(dimBody)
		}
		if renderNew && tintNew {
			bodySt = lipgloss.NewStyle().Foreground(newDim)
			if renderIdx == renderSel {
				bodySt = lipgloss.NewStyle().Foreground(newBright)
			}
		}
	}
	renderIdx++
	hide := mode == 4
	switch kind {
	case "front":
		var out []string
		for _, l := range src {
			if hide && l == "---" {
				continue
			}
			style := dimSt.Italic(hide)
			if mode == 6 {
				style = bodySt // variant 6: like any other text, bright under the cursor
				if l == "---" {
					style = markerSt
				}
			}
			out = append(out, style.Render(l))
		}
		return out
	case "quote":
		var out []string
		for _, l := range src {
			depth := 0
			for strings.HasPrefix(l, ">") {
				depth++
				l = strings.TrimPrefix(strings.TrimPrefix(l, ">"), " ")
			}
			prefix := markerSt.Render(strings.Repeat("> ", depth))
			if hide {
				prefix = dimSt.Render(strings.Repeat("│ ", depth))
			}
			rows := fill(asWords(strings.Fields(l), ws[0].op), room-2*depth, hide, mode)
			for _, r := range rows {
				out = append(out, prefix+r)
			}
		}
		return out
	case "heading":
		level := 0
		if len(ws) > 0 && strings.Trim(ws[0].text, "#") == "" {
			level = len(ws[0].text)
		}
		text := ws
		if level > 0 {
			text = ws[1:]
		}
		bodySt = headerSt // a heading keeps its colour, selected or not: it is the signpost
		rows := fill(text, room, hide, mode)
		for i := range rows {
			rows[i] = headerSt.Render(rows[i])
		}
		if !hide && level > 0 && len(rows) > 0 {
			rows[0] = markerSt.Render(strings.Repeat("#", level)+" ") + rows[0]
		}
		if hide {
			rows = append(rows, dimSt.Render(strings.Repeat("─", min(room, lipgloss.Width(rows[0])))))
		}
		return rows
	case "list":
		marker := ws[0]
		indent := strings.Repeat(" ", lipgloss.Width(marker.text)+1)
		rows := fill(ws[1:], room-len(indent), hide, mode)
		m := markerSt.Render(marker.text + " ")
		if hide {
			m = "• " + strings.Repeat(" ", max(0, len(indent)-2))
		}
		for i := range rows {
			if i == 0 {
				rows[i] = m + rows[i]
			} else {
				rows[i] = indent + rows[i]
			}
		}
		return rows
	}
	return fill(ws, room, hide, mode)
}

func asWords(ts []string, o op) []word {
	ws := make([]word, len(ts))
	for i, t := range ts {
		ws[i] = word{t, o}
	}
	return ws
}

func rangeLabel(first, last int) string {
	if first == last {
		return fmt.Sprint(first)
	}
	return fmt.Sprintf("%d–%d", first, last)
}

// ---- the Steps ----

type step struct {
	name, explain           string
	before, after           []string
	beforeStart, afterStart int
}

var steps = []step{
	{
		name:       "requirements.md: Background (new Document)",
		explain:    "A new requirements Document, opening with its Background. Every line is an addition.",
		after:      lines(requirementsText),
		afterStart: 1,
	},
	{
		name:        "CONTEXT.md: a Walkthrough becomes a Review and a Round",
		explain:     "A rewrite of existing glossary prose: entries replaced, one renamed, and lines rewrapped.",
		before:      lines(glossaryBefore),
		after:       lines(glossaryAfter),
		beforeStart: 36,
		afterStart:  36,
	},
}

// ---- variants ----

var variantNames = []string{
	"1/6 Today: source lines, tinted bands",
	"2/6 Quiet source: no tint on new text",
	"3/6 Reflowed: paragraphs, dim Markdown, before/after",
	"4/6 Rendered: formatted, track changes",
	"5/6 Reflowed, coloured text: before/after, struck red, green",
	"6/6 Combined: dim Markdown, track changes; source with +/- and coloured words",
}

const gutterW = 9 // "▸ " + label(5) + bar + space

func sourceItems(s step, room int, quiet bool) []item {
	var items []item
	for _, r := range sourceRows(s.before, s.after, s.beforeStart, s.afterStart) {
		no := r.newNo
		if r.op == del {
			no = r.oldNo
		}
		it := item{label: fmt.Sprint(no), barCh: " ", bar: plainSt}
		switch r.op {
		case ins:
			it.barCh = "+"
			it.bar = lipgloss.NewStyle().Foreground(addColour)
			if quiet {
				it.barCh = "▎"
			} else {
				it.tint = addTint
			}
		case del:
			it.barCh = "-"
			it.bar = lipgloss.NewStyle().Foreground(delColour)
			it.tint = delTint
		}
		wrapped := strings.Split(ansi.Wrap(r.text, room, ""), "\n")
		it.rows = wrapped
		items = append(items, it)
	}
	return items
}

func blockItems(s step, room, mode, sel int) []item {
	renderIdx, renderSel = 0, sel
	oldB := parseBlocks(s.before, s.beforeStart)
	if len(s.before) == 0 {
		oldB = nil
	}
	newB := parseBlocks(s.after, s.afterStart)
	key := func(b block) string { return b.kind + "\x00" + strings.Join(b.words(), " ") }
	var ka, kb []string
	for _, b := range oldB {
		ka = append(ka, key(b))
	}
	for _, b := range newB {
		kb = append(kb, key(b))
	}
	var items []item
	oi, ni := 0, 0
	ops := diffTokens(ka, kb)
	for i := 0; i < len(ops); {
		if ops[i].op == eq {
			b := newB[ni]
			items = append(items, item{
				rows:  renderBlock(b.kind, b.src, asWords(b.words(), eq), room, mode),
				label: rangeLabel(b.first, b.last), barCh: " ", bar: plainSt,
			})
			oi++
			ni++
			i++
			continue
		}
		// A run of changed blocks: diff its words as one stream, with ¶ between blocks.
		var ob, nb []block
		for i < len(ops) && ops[i].op != eq {
			if ops[i].op == del {
				ob = append(ob, oldB[oi])
				oi++
			} else {
				nb = append(nb, newB[ni])
				ni++
			}
			i++
		}
		items = append(items, changedRun(ob, nb, room, mode)...)
	}
	return items
}

func streamOf(bs []block) []string {
	var ts []string
	for i, b := range bs {
		if i > 0 {
			ts = append(ts, "¶")
		}
		ts = append(ts, b.words()...)
	}
	return ts
}

// split cuts a word stream at ¶ into one slice per block.
func split(ws []word) [][]word {
	out := [][]word{{}}
	for _, w := range ws {
		if w.text == "¶" {
			out = append(out, []word{})
			continue
		}
		out[len(out)-1] = append(out[len(out)-1], w)
	}
	return out
}

func changedRun(ob, nb []block, room, mode int) []item {
	ops := diffTokens(streamOf(ob), streamOf(nb))
	allNew := len(ob) == 0
	if mode == 3 || mode == 5 || allNew {
		var items []item
		var oldWs, newWs []word
		for _, t := range ops {
			if t.op != ins {
				oldWs = append(oldWs, word{t.tok, t.op})
			}
			if t.op != del {
				o := t.op
				if allNew {
					o = eq // a wholly new Document: nothing to emphasise
				}
				newWs = append(newWs, word{t.tok, o})
			}
		}
		for k, ws := range split(oldWs) {
			if k >= len(ob) {
				break
			}
			b := ob[k]
			items = append(items, item{
				rows:  renderBlock(b.kind, b.src, ws, room, min(mode, 5)),
				label: rangeLabel(b.first, b.last), barCh: "▎",
				bar: lipgloss.NewStyle().Foreground(delColour), kind: "removed",
			})
		}
		for k, ws := range split(newWs) {
			if k >= len(nb) {
				break
			}
			b := nb[k]
			renderNew = allNew
			rows := renderBlock(b.kind, b.src, ws, room, mode)
			renderNew = false
			items = append(items, item{
				rows: rows, label: rangeLabel(b.first, b.last), barCh: "▎",
				bar: lipgloss.NewStyle().Foreground(addColour), kind: "new",
			})
		}
		return items
	}
	// Variant 4: one merged stream in the new structure, removals struck inline.
	var merged []word
	for _, t := range ops {
		if t.tok == "¶" && t.op == del {
			continue
		}
		merged = append(merged, word{t.tok, t.op})
	}
	var items []item
	structure, kind, colour := nb, "mixed", changeBar
	if len(nb) == 0 { // a pure removal: draw it in the old structure
		structure, kind, colour = ob, "removed", delColour
	}
	for k, ws := range split(merged) {
		b := structure[min(k, len(structure)-1)]
		items = append(items, item{
			rows:  renderBlock(b.kind, b.src, ws, room, mode),
			label: rangeLabel(b.first, b.last), barCh: "▎",
			bar: lipgloss.NewStyle().Foreground(colour), kind: kind,
		})
	}
	return items
}

// ---- model ----

type model struct {
	w, h          int
	variant, step int
	source        bool
	signs         bool // variant 6: +, - and ± in the gutter instead of bars
	cursor        map[string]int
	offset        int
}

func (m model) key() string { return fmt.Sprint(m.variant, m.step, m.source) }

func (m model) items() []item {
	room := max(20, m.w-gutterW-1)
	s := steps[m.step]
	v := m.variant
	if m.source && v >= 2 && v <= 4 {
		v = 1
	}
	switch v {
	case 0:
		return sourceItems(s, room, false)
	case 1:
		return sourceItems(s, room, true)
	case 2:
		return blockItems(s, room, 3, -1)
	case 3:
		return blockItems(s, room, 4, -1)
	case 4:
		return blockItems(s, room, 5, -1)
	}
	if m.source {
		return colouredSourceItems(s, room)
	}
	return blockItems(s, room, 6, m.cursor[m.key()])
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyMsg:
		n := len(m.items())
		c := m.cursor[m.key()]
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "left":
			m.variant = (m.variant + len(variantNames) - 1) % len(variantNames)
			m.offset = 0
		case "right":
			m.variant = (m.variant + 1) % len(variantNames)
			m.offset = 0
		case "tab":
			m.step = (m.step + 1) % len(steps)
			m.offset = 0
		case "s":
			m.source = !m.source
			m.offset = 0
		case "g":
			m.signs = !m.signs
		case "up", "k":
			m.cursor[m.key()] = max(0, c-1)
		case "down", "j":
			m.cursor[m.key()] = min(n-1, c+1)
		case "pgdown", " ":
			m.cursor[m.key()] = min(n-1, c+10)
		case "pgup":
			m.cursor[m.key()] = max(0, c-10)
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.w == 0 {
		return ""
	}
	s := steps[m.step]
	header := []string{
		headerSt.Render(fmt.Sprintf("Step %d of %d: %s", m.step+1, len(steps), s.name)),
		dimSt.Render(ansi.Truncate(s.explain, m.w, "…")),
		"",
	}
	items := m.items()
	cur := min(m.cursor[m.key()], len(items)-1)
	byBlock := m.variant >= 2 && !m.source
	var body []string
	curStart, curEnd := 0, 0
	for i, it := range items {
		isCur := i == cur
		if isCur {
			curStart = len(body)
		}
		for ri, row := range it.rows {
			caret := "  "
			if isCur && ri == 0 {
				caret = caretSt.Render("▸ ")
			}
			label := "     "
			if ri == 0 {
				label = fmt.Sprintf("%5s", it.label)
			}
			bar := it.bar.Render(it.barCh)
			if isCur && m.variant == 3 && !m.source {
				bar = caretSt.Render("┃") // variant 4 marks the cursor's block in the gutter
			}
			if m.variant == 5 && !m.source {
				ch := map[string]string{"new": "+", "mixed": "±", "removed": "-", "": " "}[it.kind]
				if !m.signs && it.kind != "" {
					ch = "▎"
				}
				style := it.bar
				if isCur {
					style = caretSt
					if !m.signs || it.kind == "" {
						ch = "┃"
					}
				}
				bar = style.Render(ch)
			}
			content := row
			pad := max(0, m.w-gutterW-lipgloss.Width(row))
			line := caret + dimSt.Render(label) + " " + bar + " "
			switch {
			case isCur && !(m.variant == 3 && !m.source) && !(m.variant == 5 && !m.source):
				bg := cursorBg
				if byBlock {
					bg = quietBg
				}
				line += lipgloss.NewStyle().Background(bg).Render(content + strings.Repeat(" ", pad))
			case it.tint != nil:
				line += lipgloss.NewStyle().Background(it.tint).Render(content + strings.Repeat(" ", pad))
			default:
				line += content
			}
			body = append(body, line)
		}
		if byBlock {
			body = append(body, "") // a blank row between blocks, as the source has
		}
		if isCur {
			curEnd = len(body)
		}
	}
	view := m.h - len(header) - 1
	off := m.offset
	if curStart < off {
		off = curStart
	}
	if curEnd > off+view {
		off = min(curStart, curEnd-view)
	}
	end := min(len(body), off+view)
	shown := body[off:end]
	for len(shown) < view {
		shown = append(shown, "")
	}
	toggle := ""
	if m.variant >= 2 {
		toggle = "s: show source  ·  "
		if m.source {
			toggle = "s: show formatted  ·  "
		}
	}
	if m.variant == 5 && !m.source {
		g := "g: gutter signs"
		if m.signs {
			g = "g: gutter bars"
		}
		toggle += g + "  ·  "
	}
	bar := barSt.Render(fmt.Sprintf(" ◀ %s ▶ ", variantNames[m.variant])) +
		dimSt.Render("  "+toggle+"tab: other Step  ·  ↑↓ move  ·  q quit")
	return strings.Join(header, "\n") + "\n" + strings.Join(shown, "\n") + "\n" + ansi.Truncate(bar, m.w, "")
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-dump" {
		for v := range variantNames {
			for st := range steps {
				m := model{w: 100, h: 34, variant: v, step: st, cursor: map[string]int{}}
				fmt.Printf("===== variant %d, step %d =====\n%s\n", v+1, st+1, m.View())
				if v == 5 {
					m.source = true
					fmt.Printf("===== variant %d, step %d, source =====\n%s\n", v+1, st+1, m.View())
				}
			}
		}
		return
	}
	p := tea.NewProgram(model{variant: 5, signs: true, cursor: map[string]int{}}, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// colouredSourceItems is variant 6's source view: variant 1's rows and +/-
// signs, but no tinted bands: the changed text itself is coloured, red or
// green — within a rewrite only the words that changed, and a wholly new line
// all of it.
func colouredSourceItems(s step, room int) []item {
	rows := sourceRows(s.before, s.after, s.beforeStart, s.afterStart)
	wordsOf := make([][]word, len(rows))
	for i := 0; i < len(rows); {
		if rows[i].op == eq {
			wordsOf[i] = asWords(strings.Fields(rows[i].text), eq)
			i++
			continue
		}
		var dels, inss []int
		for i < len(rows) && rows[i].op != eq {
			if rows[i].op == del {
				dels = append(dels, i)
			} else {
				inss = append(inss, i)
			}
			i++
		}
		if len(dels) == 0 {
			for _, k := range inss {
				wordsOf[k] = asWords(strings.Fields(rows[k].text), ins)
			}
			continue
		}
		stream := func(idx []int) []string {
			var ts []string
			for _, k := range idx {
				ts = append(ts, strings.Fields(rows[k].text)...)
				ts = append(ts, "\n")
			}
			return ts
		}
		var oldWs, newWs []word
		for _, t := range diffTokens(stream(dels), stream(inss)) {
			if t.op != ins {
				oldWs = append(oldWs, word{t.tok, t.op})
			}
			if t.op != del {
				newWs = append(newWs, word{t.tok, t.op})
			}
		}
		assign := func(idx []int, ws []word) {
			k := 0
			for _, w := range ws {
				if w.text == "\n" {
					k++
					continue
				}
				if k < len(idx) {
					wordsOf[idx[k]] = append(wordsOf[idx[k]], w)
				}
			}
		}
		assign(dels, oldWs)
		assign(inss, newWs)
	}
	var items []item
	for i, r := range rows {
		no := r.newNo
		if r.op == del {
			no = r.oldNo
		}
		it := item{label: fmt.Sprint(no), barCh: " ", bar: plainSt}
		switch r.op {
		case ins:
			it.barCh, it.bar = "+", lipgloss.NewStyle().Foreground(addColour)
		case del:
			it.barCh, it.bar = "-", lipgloss.NewStyle().Foreground(delColour)
		}
		indent := r.text[:len(r.text)-len(strings.TrimLeft(r.text, " \t"))]
		var b []string
		cur, curW := indent, len(indent)
		for j, w := range wordsOf[i] {
			ws := withOp(plainSt, w.op, 7).Render(w.text)
			ww := lipgloss.Width(ws)
			if curW > len(indent) && curW+1+ww > room {
				b = append(b, cur)
				cur, curW = indent, len(indent)
			}
			if curW > len(indent) {
				sp := " "
				if j > 0 && wordsOf[i][j-1].op == w.op && w.op != eq {
					sp = withOp(plainSt, w.op, 7).Render(" ")
				}
				cur += sp
				curW++
			}
			cur += ws
			curW += ww
		}
		it.rows = append(b, cur)
		items = append(items, it)
	}
	return items
}

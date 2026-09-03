// Package tui draws the review. It is deliberately thin: keypresses become
// intents sent to the daemon, and what comes back is drawn. All state that
// matters lives in the daemon, so closing this window loses nothing.
package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// Run attaches to the daemon and blocks until the Reviewer quits. It refuses to
// start at all when no daemon answers: a review surface with nothing behind it
// should say so on stderr, not draw an empty screen.
func Run(port int) error {
	client := client{base: fmt.Sprintf("http://127.0.0.1:%d", port)}
	view, err := client.view()
	if err != nil {
		return fmt.Errorf("no dbn daemon on port %d — start one with `dbn serve`: %w", port, err)
	}

	program := tea.NewProgram(
		model{client: client, view: view},
		tea.WithAltScreen(),
	)
	_, err = program.Run()
	return err
}

type client struct{ base string }

func (c client) view() (*daemon.ViewWire, error) {
	response, err := http.Get(c.base + "/view")
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the server answered %s — is that dbn?", response.Status)
	}
	var view daemon.ViewWire
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		return nil, err
	}
	return &view, nil
}

func (c client) intent(path string) {
	response, err := http.Post(c.base+path, "text/plain", nil)
	if err != nil {
		return // the next poll will surface the daemon being gone
	}
	response.Body.Close()
}

type refreshMsg struct {
	view *daemon.ViewWire
	err  error
}

type tickMsg struct{}

type model struct {
	client      client
	view        *daemon.ViewWire
	lostErr     error
	viewport    viewport.Model
	cursor      stepCursor
	status      string
	mode        mode
	note        textarea.Model
	crCursor    int    // selected row in the change-request list
	pendingSel  [3]int // excerpt, first, last awaiting a note
	pendingCode string // the code being commented on, shown above the note input
	width       int
	height      int
	ready       bool
}

type mode int

const (
	modeReview mode = iota // walking Steps
	modeNote               // typing a Change Request note
	modeList               // the Change Request list
	modeDone               // the finish summary
)

func (m *model) inStep() bool {
	return m.view != nil && m.view.Posted && m.view.Position > 0 && m.view.Step != nil
}

// syncCursor rebuilds the selection cursor when the Step in view changes.
func (m *model) syncCursor() {
	if m.inStep() {
		m.cursor = newStepCursor(m.view.Step)
	}
}

func (m model) Init() tea.Cmd {
	return tick()
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func (c client) raiseChangeRequest(excerpt, first, last int, note string) bool {
	body, _ := json.Marshal(map[string]any{
		"excerpt_index": excerpt, "first_line": first, "last_line": last, "note": note,
	})
	response, err := http.Post(c.base+"/changerequest", "application/json", bytes.NewReader(body))
	if err != nil {
		return false
	}
	response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func (c client) withdraw(id int) {
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/changerequest/%d", c.base, id), nil)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
	}
}

func (m *model) copyAnchor() tea.Cmd {
	excerpt, first, last, ok := m.cursor.selection()
	if !ok {
		m.status = "selection spans two Excerpts — narrow it to one"
		return nil
	}
	text, ok := m.client.composeAnchor(excerpt, first, last)
	if !ok {
		m.status = "could not compose the Anchor"
		return nil
	}
	if copyToClipboard(text) {
		m.status = fmt.Sprintf("copied Anchor for lines %d-%d — paste it into your agent chat", first, last)
	} else {
		m.status = "no clipboard tool found; the Anchor could not be copied"
	}
	m.cursor.sel = -1
	return nil
}

func (m model) refresh() tea.Cmd {
	return func() tea.Msg {
		view, err := m.client.view()
		return refreshMsg{view: view, err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		headerHeight, footerHeight := 2, 2
		m.viewport = viewport.New(msg.Width, max(1, msg.Height-headerHeight-footerHeight))
		if m.note.Value() == "" && !m.note.Focused() {
			ta := textarea.New()
			ta.Placeholder = "what should change here?"
			ta.CharLimit = 1000
			ta.ShowLineNumbers = false
			m.note = ta
		}
		m.note.SetWidth(max(20, msg.Width-4))
		m.note.SetHeight(4)
		m.ready = true
		m.syncCursor()
		m.viewport.SetContent(m.content())
		return m, nil

	case tickMsg:
		return m, tea.Batch(m.refresh(), tick())

	case refreshMsg:
		positionChanged := false
		if msg.err != nil {
			m.lostErr = msg.err
		} else {
			if m.view != nil && msg.view != nil && m.view.Position != msg.view.Position {
				positionChanged = true
			}
			m.lostErr = nil
			m.view = msg.view
		}
		if positionChanged {
			m.syncCursor()
			m.viewport.GotoTop()
		}
		if m.ready {
			m.viewport.SetContent(m.content())
		}
		return m, nil

	case tea.KeyMsg:
		key := msg.String()

		switch m.mode {
		case modeNote:
			return m.updateNote(msg)
		case modeList:
			return m.updateList(key)
		case modeDone:
			if key == "q" || key == "ctrl+c" {
				return m, tea.Quit
			}
			return m, nil
		}

		switch key {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "enter", "n", "right":
			m.status = ""
			m.client.intent("/advance")
			return m, m.refresh()
		case "p", "left":
			m.status = ""
			m.client.intent("/back")
			return m, m.refresh()
		case "g":
			m.status = ""
			m.client.intent("/goto/0")
			return m, m.refresh()
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			m.status = "→ Step " + key
			m.client.intent("/goto/" + key)
			return m, m.refresh()
		case "L":
			m.mode = modeList
			m.crCursor = 0
			return m, nil
		case "f", "F":
			m.client.intent("/finish")
			m.mode = modeDone
			return m, m.refresh()
		}

		if m.inStep() {
			switch key {
			case "up", "k":
				m.cursor.move(-1)
				return m, nil
			case "down", "j":
				m.cursor.move(1)
				return m, nil
			case "v", " ":
				m.cursor.toggleSelect()
				if m.cursor.sel >= 0 {
					m.status = "selecting — ↑/↓ to extend, y copy, c comment"
				} else {
					m.status = ""
				}
				return m, nil
			case "esc":
				m.cursor.sel = -1
				return m, nil
			case "y":
				return m, m.copyAnchor()
			case "c":
				excerpt, first, last, ok := m.cursor.selection()
				if !ok {
					m.status = "selection spans two Excerpts — narrow it to one"
					return m, nil
				}
				m.mode = modeNote
				m.note.SetValue("")
				m.note.Focus()
				m.pendingSel = [3]int{excerpt, first, last}
				if code, ok := m.client.composeAnchor(excerpt, first, last); ok {
					m.pendingCode = code
				} else {
					m.pendingCode = ""
				}
				return m, textarea.Blink
			}
			return m, nil
		}

	}

	// Only the Brief scrolls through the viewport; a Step is cursor-driven.
	if m.view == nil || m.view.Position == 0 {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) updateNote(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, isKey := msg.(tea.KeyMsg)
	if isKey {
		switch key.String() {
		case "esc":
			m.mode = modeReview
			m.note.Blur()
			return m, nil
		case "ctrl+d", "ctrl+s":
			note := m.note.Value()
			if note != "" {
				if m.client.raiseChangeRequest(m.pendingSel[0], m.pendingSel[1], m.pendingSel[2], note) {
					m.status = "Change Request raised"
					m.cursor.sel = -1
				} else {
					m.status = "could not raise the Change Request"
				}
			}
			m.mode = modeReview
			m.note.Blur()
			return m, m.refresh()
		}
	}
	var cmd tea.Cmd
	m.note, cmd = m.note.Update(msg)
	return m, cmd
}

func (m model) updateList(key string) (tea.Model, tea.Cmd) {
	list := m.view.ChangeRequests
	switch key {
	case "esc", "L", "q":
		m.mode = modeReview
		return m, nil
	case "up", "k":
		if m.crCursor > 0 {
			m.crCursor--
		}
		return m, nil
	case "down", "j":
		if m.crCursor < len(list)-1 {
			m.crCursor++
		}
		return m, nil
	case "d", "x":
		if m.crCursor < len(list) {
			m.client.withdraw(list[m.crCursor].ID)
			if m.crCursor > 0 {
				m.crCursor--
			}
		}
		return m, m.refresh()
	}
	return m, nil
}

var (
	subtle   = lipgloss.AdaptiveColor{Light: "#6b6b6b", Dark: "#9a9a9a"}
	accent   = lipgloss.AdaptiveColor{Light: "#005f87", Dark: "#5fd7ff"}
	warn     = lipgloss.AdaptiveColor{Light: "#af5f00", Dark: "#ffaf5f"}
	headerSt = lipgloss.NewStyle().Bold(true).Foreground(accent)
	dimSt    = lipgloss.NewStyle().Foreground(subtle)
	warnSt   = lipgloss.NewStyle().Bold(true).Foreground(warn)
	labelSt  = lipgloss.NewStyle().Bold(true)
	accentSt = lipgloss.NewStyle().Bold(true).Foreground(accent)
	gutterSt = lipgloss.NewStyle().Foreground(subtle)
	addSt    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#207520", Dark: "#87d787"})
	delSt    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#a01010", Dark: "#ff8787"})
)

func (m model) View() string {
	if !m.ready {
		return "attaching…"
	}
	header := m.header()
	footerText := m.footer()
	if m.width > 1 {
		footerText = lipgloss.NewStyle().Width(m.width).Render(footerText)
	}
	footer := dimSt.Render(footerText)
	if m.status != "" {
		footer = accentSt.Render(m.status) + "\n" + footer
	}

	switch m.mode {
	case modeNote:
		return header + "\n\n" + m.noteView() + "\n" + footer
	case modeList:
		return header + "\n\n" + m.listView() + "\n" + dimSt.Render("↑/↓ move  ·  d withdraw  ·  esc back")
	case modeDone:
		return header + "\n\n" + m.doneView() + "\n" + dimSt.Render("q quit")
	}

	var body string
	if m.inStep() {
		body = renderStep(m.view.Step, m.cursor, m.commentedLines(), m.width, m.bodyHeight())
	} else {
		body = m.viewport.View()
	}
	return header + "\n\n" + body + "\n" + footer
}

func (m model) noteView() string {
	code := m.pendingCode
	if code == "" {
		code = dimSt.Render(fmt.Sprintf("lines %d-%d", m.pendingSel[1], m.pendingSel[2]))
	}
	return labelSt.Render("New Change Request") + "\n\n" +
		code + "\n" +
		m.note.View() + "\n\n" + dimSt.Render("ctrl+d to raise  ·  esc to cancel")
}

func (m model) listView() string {
	list := m.view.ChangeRequests
	if len(list) == 0 {
		return dimSt.Render("No Change Requests raised yet.")
	}
	var b strings.Builder
	b.WriteString(labelSt.Render(fmt.Sprintf("%d Change Request(s)", len(list))) + "\n\n")
	for i, cr := range list {
		cursor := "  "
		if i == m.crCursor {
			cursor = accentSt.Render("▸ ")
		}
		b.WriteString(fmt.Sprintf("%sStep %d  %s\n     %s\n", cursor, cr.Step, dimSt.Render(cr.Location), cr.Note))
	}
	return b.String()
}

func (m model) doneView() string {
	var b strings.Builder
	b.WriteString(labelSt.Render("Review finished") + "\n\n")
	if m.view != nil {
		seen, flagged := 0, 0
		for _, st := range m.view.StepStatuses {
			switch st {
			case "seen":
				seen++
			case "flagged":
				flagged++
			}
		}
		b.WriteString(fmt.Sprintf("%d Steps seen, %d flagged, %d Change Request(s) raised.\n\n",
			seen, flagged, len(m.view.ChangeRequests)))
		b.WriteString(dimSt.Render("Tell your agent you are done; it will collect the Change Requests and open a Revision Round.") + "\n")
	}
	return b.String()
}

// commentedLines is the set of "file:line" in the current Step that carry a
// Change Request, so the diff can mark them.
func (m model) commentedLines() map[string]bool {
	out := map[string]bool{}
	if m.view == nil {
		return out
	}
	for _, cr := range m.view.ChangeRequests {
		if cr.Step != m.view.Position {
			continue
		}
		for n := cr.FirstLine; n <= cr.LastLine; n++ {
			out[fmt.Sprintf("%s:%d", cr.File, n)] = true
		}
	}
	return out
}

func (m model) bodyHeight() int {
	h := m.height - 5
	if h < 4 {
		return 4
	}
	return h
}

func (m model) header() string {
	switch {
	case m.lostErr != nil:
		return warnSt.Render("dbn — lost the daemon: ") + m.lostErr.Error()
	case m.view == nil || !m.view.Posted:
		return headerSt.Render("dbn") + dimSt.Render(" — no Walkthrough posted")
	case m.view.Position == 0:
		return headerSt.Render("dbn — Brief") + dimSt.Render("  ·  "+pluralize(m.view.StepCount, "Step")+" ahead"+m.coverageSuffix())
	default:
		return headerSt.Render(fmt.Sprintf("dbn — Step %d of %d", m.view.Position, m.view.StepCount)) + dimSt.Render(m.coverageSuffix())
	}
}

func (m model) coverageSuffix() string {
	c := m.view.Coverage
	return fmt.Sprintf("  ·  %d/%d changed lines seen", c.Seen, c.Total)
}

func (m model) footer() string {
	switch {
	case m.view == nil || !m.view.Posted:
		return "waiting for an agent to post a Walkthrough  ·  q quit"
	case m.view.Position == 0:
		return "enter/→ begin  ·  " + m.jumpHint() + "  ·  ↑/↓ scroll  ·  L list  ·  F finish  ·  q quit"
	default:
		return "↑/↓ move  ·  space/v select  ·  y copy  ·  c comment  ·  →/enter next  ·  ←/p back  ·  " + m.jumpHint() + "  ·  g Brief  ·  L list  ·  F finish  ·  q quit"
	}
}

// jumpHint labels the number-jump with the real Step count, and says how to
// reach Steps past 9.
func (m model) jumpHint() string {
	n := m.view.StepCount
	if n <= 1 {
		return "1 Step"
	}
	if n <= 9 {
		return fmt.Sprintf("1-%d go to Step", n)
	}
	return "1-9 go to Step (→ for later)"
}

func (m model) content() string {
	if m.view == nil || !m.view.Posted {
		return dimSt.Render("An Authoring Agent posts a Walkthrough over MCP; it will appear here.")
	}
	if m.view.Position == 0 {
		return m.brief()
	}
	return m.step()
}

func (m model) brief() string {
	var b strings.Builder
	brief := m.view.Brief

	wrap := func(text string) string {
		if m.viewport.Width > 1 {
			return lipgloss.NewStyle().Width(m.viewport.Width).Render(text)
		}
		return text
	}

	b.WriteString(labelSt.Render("Goal") + "\n" + wrap(brief.Ask) + "\n\n")
	b.WriteString(labelSt.Render("Approach") + "\n" + wrap(brief.Approach) + "\n\n")

	b.WriteString(labelSt.Render("Provenance") + "\n")
	if brief.ProvenanceKind == "stated" {
		b.WriteString("stated — " + brief.ProvenanceCitation + "\n\n")
	} else {
		b.WriteString(warnSt.Render("inferred") + " — reverse-engineered from the changes; trust the narrative accordingly\n\n")
	}

	b.WriteString(labelSt.Render("Under review") + "\n")
	for _, repository := range m.view.Repositories {
		b.WriteString(fmt.Sprintf("  %s  (%s)\n", repository.Root, repository.Range))
	}
	b.WriteString("\n")

	b.WriteString(labelSt.Render("Steps") + "\n")
	for i, name := range m.view.StepNames {
		mark := " "
		if i < len(m.view.Seen) && m.view.Seen[i] {
			mark = "✓"
		}
		b.WriteString(fmt.Sprintf("  %s %2d. %s\n", dimSt.Render(mark), i+1, name))
	}
	return b.String()
}

func (m model) step() string {
	var b strings.Builder
	step := m.view.Step

	b.WriteString(labelSt.Render(step.Name) + "\n\n")
	b.WriteString(step.Explanation + "\n")
	if step.OversizeJustification != "" {
		b.WriteString("\n" + warnSt.Render("oversized: ") + step.OversizeJustification + "\n")
	}

	for _, excerpt := range step.Excerpts {
		b.WriteString("\n" + dimSt.Render(fmt.Sprintf("── %s:%d–%d (%s side)", excerpt.File, excerpt.FirstLine, excerpt.LastLine, excerpt.Side)) + "\n")
		if excerpt.Problem != "" {
			b.WriteString(warnSt.Render("  cannot show this Excerpt: ") + excerpt.Problem + "\n")
			continue
		}
		for _, line := range excerpt.Lines {
			gutter := gutterSt.Render(fmt.Sprintf("%5d │ ", line.Number))
			if line.Changed {
				marker := excerpt.Side // "new" -> +, "old" -> -
				sign, style := "+", addSt
				if marker == "old" {
					sign, style = "-", delSt
				}
				b.WriteString(gutter + style.Render(sign+" "+line.Text) + "\n")
			} else {
				b.WriteString(gutter + dimSt.Render("  ") + line.Text + "\n")
			}
		}
	}
	return b.String()
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

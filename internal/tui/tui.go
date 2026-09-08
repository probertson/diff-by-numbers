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

func (c client) reopen() {
	c.intent("/reopen")
}

func (c client) reraise(id int) bool {
	response, err := http.Post(fmt.Sprintf("%s/reraise/%d", c.base, id), "text/plain", nil)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}

type refreshMsg struct {
	view *daemon.ViewWire
	err  error
}

type tickMsg struct{}

type model struct {
	client        client
	view          *daemon.ViewWire
	lostErr       error
	viewport      viewport.Model
	cursor        stepCursor
	status        string
	mode          mode
	note          textarea.Model
	crCursor      int      // selected row in the change-request list
	pendingSel    [3]int   // excerpt, first, last awaiting a note
	pendingCode   string   // the code being commented on, shown above the note input
	editingID     int      // >0 when editing an existing Change Request rather than adding
	crFilter      crFilter // when active, the List shows only comments on one line
	reraiseCursor int      // selected row among declined dispositions
	expandedAck   bool     // showing the code behind this Step's Acknowledgements
	expanded      []daemon.ExcerptWire
	width         int
	height        int
	ready         bool
}

type crFilter struct {
	active bool
	file   string
	line   int
}

type mode int

const (
	modeReview  mode = iota // walking Steps
	modeNote                // typing a Change Request note
	modeList                // the Change Request list
	modeDone                // the finish summary
	modeReraise             // choosing a declined request to re-raise
)

func (m *model) inStep() bool {
	return m.view != nil && m.view.Posted && m.view.Position > 0 && m.view.Step != nil
}

// multiRepo reports whether the Walkthrough spans more than one repository, so
// the UI can label files with their repository only when it is ambiguous.
func (m *model) multiRepo() bool {
	return m.view != nil && len(m.view.Repositories) > 1
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

func (c client) editChangeRequest(id int, note string) bool {
	body, _ := json.Marshal(map[string]any{"note": note})
	req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/changerequest/%d", c.base, id), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
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

// expand asks the daemon for the code an Acknowledgement stands in for.
func (c client) expand(step, ack int) ([]daemon.ExcerptWire, bool) {
	response, err := http.Get(fmt.Sprintf("%s/expand/%d/%d", c.base, step, ack))
	if err != nil {
		return nil, false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, false
	}
	var views []daemon.ExcerptWire
	if err := json.NewDecoder(response.Body).Decode(&views); err != nil {
		return nil, false
	}
	return views, true
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
			m.expandedAck = false // the expansion belonged to the Step we just left
			m.expanded = nil
		}
		if m.view != nil && m.view.Finished && m.mode == modeReview {
			m.mode = modeDone
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
		case modeReraise:
			return m.updateReraise(key)
		case modeDone:
			switch key {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "r":
				m.client.reopen()
				m.mode = modeReview
				m.status = "review reopened — add or change anything, then f to finish again"
				return m, m.refresh()
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
		case "r":
			if m.view != nil && m.view.Finished {
				m.client.reopen()
				m.status = "review reopened — add or change anything, then f to finish again"
				return m, m.refresh()
			}
		case "l", "L":
			m.crFilter = crFilter{}
			m.mode = modeList
			m.crCursor = 0
			return m, nil
		case "R":
			if len(m.declinedDispositions()) == 0 {
				m.status = "no declined requests to re-raise"
				return m, nil
			}
			m.reraiseCursor = 0
			m.mode = modeReraise
			return m, nil
		case "f", "F":
			m.client.intent("/finish")
			m.mode = modeDone
			return m, m.refresh()
		}

		if m.inStep() {
			// A Step with no code lines (Acknowledgement-only, or one whose Excerpts
			// all failed to resolve) has nothing to select, comment on or anchor —
			// and neither does an expanded view, which is read-only. Only x/esc and
			// navigation apply there.
			interactive := len(m.cursor.lines) > 0 && !m.expandedAck
			switch key {
			case "up", "k":
				if interactive {
					m.cursor.move(-1)
				}
				return m, nil
			case "down", "j":
				if interactive {
					m.cursor.move(1)
				}
				return m, nil
			case "v", " ":
				if !interactive {
					m.status = m.noInteractionHint()
					return m, nil
				}
				m.cursor.toggleSelect()
				m.status = ""
				return m, nil
			case "esc":
				if m.expandedAck {
					m.expandedAck = false
					m.expanded = nil
					m.cursor.sel = -1
					m.status = ""
					return m, nil
				}
				m.cursor.sel = -1
				m.status = ""
				return m, nil
			case "x":
				if len(m.view.Step.Acknowledgements) == 0 {
					m.status = "nothing to expand on this Step"
					return m, nil
				}
				if m.expandedAck {
					m.expandedAck = false
					m.expanded = nil
					m.cursor.sel = -1
					m.status = ""
					return m, nil
				}
				var all []daemon.ExcerptWire
				failed := 0
				for i := range m.view.Step.Acknowledgements {
					if views, ok := m.client.expand(m.view.Position, i); ok {
						all = append(all, views...)
					} else {
						failed++
					}
				}
				m.expanded = all
				m.expandedAck = true
				m.cursor.sel = -1
				if failed > 0 {
					m.status = fmt.Sprintf("expanded — but %d acknowledgement(s) could not be fetched (x or <esc> to collapse)", failed)
				} else {
					m.status = "expanded — showing the acknowledged code (x or <esc> to collapse)"
				}
				return m, nil
			case "y":
				if !interactive {
					m.status = m.noInteractionHint()
					return m, nil
				}
				return m, m.copyAnchor()
			case "e":
				if !interactive {
					m.status = m.noInteractionHint()
					return m, nil
				}
				if m.view.Finished {
					m.status = "review is finished — press r to reopen before editing"
					return m, nil
				}
				here := m.commentsAtCursor()
				switch len(here) {
				case 0:
					m.status = "no comment on this line to edit"
				case 1:
					m.editingID = here[0].ID
					m.pendingCode = here[0].Anchor
					m.note.SetValue(here[0].Note)
					m.note.Focus()
					m.mode = modeNote
					return m, textarea.Blink
				default:
					line := m.cursor.lines[m.cursor.cursor]
					m.crFilter = crFilter{active: true, file: m.view.Step.Excerpts[line.excerpt].File, line: line.number}
					m.crCursor = 0
					m.mode = modeList
				}
				return m, nil
			case "c":
				if !interactive {
					m.status = m.noInteractionHint()
					return m, nil
				}
				if m.view.Finished {
					m.status = "review is finished — press r to reopen before commenting"
					return m, nil
				}
				excerpt, first, last, ok := m.cursor.selection()
				if !ok {
					m.status = "selection spans two Excerpts — narrow it to one"
					return m, nil
				}
				m.editingID = 0 // c always adds a fresh comment, never edits
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
			m.editingID = 0
			m.mode = modeReview
			m.note.Blur()
			return m, nil
		case "enter":
			note := m.note.Value()
			if note != "" {
				if m.editingID > 0 {
					if m.client.editChangeRequest(m.editingID, note) {
						m.status = "Change Request updated"
					} else {
						m.status = "could not update the Change Request"
					}
				} else if m.client.raiseChangeRequest(m.pendingSel[0], m.pendingSel[1], m.pendingSel[2], note) {
					m.status = "comment added"
				} else {
					m.status = "could not add the comment"
				}
			}
			m.editingID = 0
			m.cursor.sel = -1
			m.mode = modeReview
			m.note.Blur()
			return m, m.refresh()
		case "ctrl+j", "alt+enter", "shift+enter":
			m.note.InsertString("\n")
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.note, cmd = m.note.Update(msg)
	return m, cmd
}

func (m model) updateList(key string) (tea.Model, tea.Cmd) {
	list := m.filteredCRs()
	switch key {
	case "esc", "L", "q":
		m.crFilter = crFilter{}
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
	case "e", "enter":
		if m.crCursor < len(list) {
			cr := list[m.crCursor]
			m.editingID = cr.ID
			m.pendingCode = cr.Anchor
			m.note.SetValue(cr.Note)
			m.note.Focus()
			m.mode = modeNote
			return m, textarea.Blink
		}
		return m, nil
	}
	return m, nil
}

func (m model) updateReraise(key string) (tea.Model, tea.Cmd) {
	declined := m.declinedDispositions()
	switch key {
	case "esc", "q", "R":
		m.mode = modeReview
		return m, nil
	case "up", "k":
		if m.reraiseCursor > 0 {
			m.reraiseCursor--
		}
		return m, nil
	case "down", "j":
		if m.reraiseCursor < len(declined)-1 {
			m.reraiseCursor++
		}
		return m, nil
	case "enter":
		if m.reraiseCursor < len(declined) {
			disposition := declined[m.reraiseCursor]
			if m.client.reraise(disposition.ChangeRequestID) {
				m.status = fmt.Sprintf("re-raised request #%d — it stands again this round", disposition.ChangeRequestID)
			} else {
				m.status = "could not re-raise the request"
			}
			m.mode = modeReview
			return m, m.refresh()
		}
		return m, nil
	}
	return m, nil
}

// declinedDispositions is the subset of the previous round's Change Requests the
// agent declined — the ones the Reviewer may re-raise.
func (m model) declinedDispositions() []daemon.DispositionWire {
	if m.view == nil {
		return nil
	}
	var out []daemon.DispositionWire
	for _, disposition := range m.view.Dispositions {
		if disposition.Status == "declined" {
			out = append(out, disposition)
		}
	}
	return out
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

	var body, persistent, stateful string
	switch m.mode {
	case modeNote:
		body = m.noteView()
		persistent = keybar("enter add", "shift+enter newline", "<esc> cancel")
	case modeList:
		body = m.listView()
		persistent = keybar("↑/↓ move", "e edit", "d withdraw", "<esc> back")
	case modeReraise:
		body = m.reraiseView()
		persistent = keybar("↑/↓ move", "enter re-raise", "<esc> back")
	case modeDone:
		body = m.doneView()
		persistent = keybar("r reopen", "q quit")
	default:
		if m.inStep() && m.expandedAck {
			body = renderExpanded(m.expanded, m.width, m.bodyHeight(), m.multiRepo())
		} else if m.inStep() {
			body = renderStep(m.view.Step, m.cursor, m.commentedLines(), m.width, m.bodyHeight(), m.multiRepo())
		} else {
			body = m.viewport.View()
		}
		// Two rows: the global actions always in the gray row below, and the
		// current page's own actions in the blue row above — replaced by a
		// transient status message while there is one to show.
		persistent = m.globalKeys()
		stateful = m.status
		if stateful == "" {
			stateful = m.modeKeys()
		}
	}

	return m.frame(m.header(), body, stateful, persistent)
}

// frame assembles a screen with the persistent shortcut line pinned to the very
// bottom, a stateful (coloured) line just above it, and the body filling the gap
// so the shortcuts sit in the same place on every page.
func (m model) frame(header, body, stateful, persistent string) string {
	wrap := func(text string) string {
		if m.width > 1 {
			return lipgloss.NewStyle().Width(m.width).Render(text)
		}
		return text
	}
	top := header + "\n\n" + body

	var bottom string
	if stateful != "" {
		bottom = accentSt.Render(wrap(stateful)) + "\n"
	}
	bottom += dimSt.Render(wrap(persistent))

	gap := m.height - lipgloss.Height(top) - lipgloss.Height(bottom)
	if gap < 1 {
		gap = 1
	}
	return top + strings.Repeat("\n", gap) + bottom
}

// globalKeys is the gray row: the actions available on every page, so their
// position never changes as the Reviewer moves.
func (m model) globalKeys() string {
	if m.view == nil || !m.view.Posted {
		return keybar("q quit")
	}
	return keybar(m.navHint(), "g Overview", "l list", "f finish", "q quit")
}

// modeKeys is the blue row: the actions available on the current page only. It
// changes with the page — code selection on a Step, expansion on an
// Acknowledgement — while the global row underneath stays put.
func (m model) modeKeys() string {
	if m.view == nil || !m.view.Posted {
		return ""
	}
	if m.view.Position == 0 {
		tokens := []string{"enter begin", "↑/↓ scroll"}
		if len(m.declinedDispositions()) > 0 {
			tokens = append(tokens, "R re-raise a decline")
		}
		return keybar(tokens...)
	}
	if m.expandedAck {
		return keybar("x/<esc> collapse")
	}
	if !m.inStep() {
		return ""
	}
	if m.cursor.sel >= 0 {
		return keybar("↑/↓ extend", "y copy", "c comment", "<esc> stop selecting")
	}
	var tokens []string
	if len(m.cursor.lines) > 0 {
		tokens = append(tokens, "↑/↓ move", "<space>/v select", "y copy", "c comment")
		if _, ok := m.commentAtCursor(); ok {
			tokens = append(tokens, "e edit")
		}
	}
	if len(m.view.Step.Acknowledgements) > 0 {
		tokens = append(tokens, "x expand")
	}
	return keybar(tokens...)
}

// noInteractionHint explains why selecting, commenting or anchoring is
// unavailable on the current Step — expanded, acknowledged, or code-less.
func (m model) noInteractionHint() string {
	if m.expandedAck {
		return "collapse with x first to select code"
	}
	if m.inStep() && len(m.view.Step.Acknowledgements) > 0 {
		return "no code to select here — press x to expand the acknowledged files"
	}
	return "no readable code on this Step"
}

func (m model) noteView() string {
	code := m.pendingCode
	if code == "" {
		code = dimSt.Render(fmt.Sprintf("lines %d-%d", m.pendingSel[1], m.pendingSel[2]))
	}
	title := "New Change Request"
	if m.editingID > 0 {
		title = "Edit Change Request"
	}
	return labelSt.Render(title) + "\n\n" + code + "\n" + m.note.View()
}

func (m model) listView() string {
	list := m.filteredCRs()
	if len(list) == 0 {
		return dimSt.Render("No comments to show.")
	}
	var b strings.Builder
	title := fmt.Sprintf("%d Change Request(s)", len(list))
	if m.crFilter.active {
		title = fmt.Sprintf("%d comment(s) on %s:%d", len(list), m.crFilter.file, m.crFilter.line)
	}
	b.WriteString(labelSt.Render(title) + "\n\n")
	for i, cr := range list {
		cursor := "  "
		if i == m.crCursor {
			cursor = accentSt.Render("▸ ")
		}
		where := fmt.Sprintf("Step %d", cr.Step)
		if cr.Step == 0 {
			where = "re-raised" // carried over from a previous round, not tied to a current Step
		}
		b.WriteString(fmt.Sprintf("%s%s  %s\n", cursor, where, dimSt.Render(cr.Location)))
		for _, line := range strings.Split(strings.TrimRight(cr.Anchor, "\n"), "\n") {
			b.WriteString("     " + dimSt.Render(line) + "\n")
		}
		b.WriteString("     " + cr.Note + "\n\n")
	}
	return b.String()
}

func (m model) reraiseView() string {
	declined := m.declinedDispositions()
	if len(declined) == 0 {
		return dimSt.Render("No declined requests to re-raise.")
	}
	var b strings.Builder
	b.WriteString(labelSt.Render("Re-raise a declined request") + "\n\n")
	for i, disposition := range declined {
		cursor := "  "
		if i == m.reraiseCursor {
			cursor = accentSt.Render("▸ ")
		}
		b.WriteString(fmt.Sprintf("%s#%d  %s\n", cursor, disposition.ChangeRequestID, dimSt.Render(disposition.Location)))
		b.WriteString("     " + dimSt.Render("you asked: ") + disposition.Note + "\n")
		b.WriteString("     " + dimSt.Render("agent declined: ") + disposition.Reasoning + "\n\n")
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
		b.WriteString(dimSt.Render("Or press r to reopen and keep editing.") + "\n")
	}
	return b.String()
}

// commentAtCursor returns the Change Request anchored over the cursor's line, if
// there is one, so it can be edited in place.
func (m model) commentAtCursor() (daemon.ChangeRequestWire, bool) {
	if !m.inStep() || len(m.cursor.lines) == 0 {
		return daemon.ChangeRequestWire{}, false
	}
	line := m.cursor.lines[m.cursor.cursor]
	file := m.view.Step.Excerpts[line.excerpt].File
	for _, cr := range m.view.ChangeRequests {
		if cr.Step == m.view.Position && cr.File == file && line.number >= cr.FirstLine && line.number <= cr.LastLine {
			return cr, true
		}
	}
	return daemon.ChangeRequestWire{}, false
}

// filteredCRs is the List's current contents: every Change Request, or just
// those on one line when the List was opened by pressing e on a line that has
// more than one.
func (m model) filteredCRs() []daemon.ChangeRequestWire {
	all := m.view.ChangeRequests
	if !m.crFilter.active {
		return all
	}
	var out []daemon.ChangeRequestWire
	for _, cr := range all {
		if cr.File == m.crFilter.file && m.crFilter.line >= cr.FirstLine && m.crFilter.line <= cr.LastLine {
			out = append(out, cr)
		}
	}
	return out
}

// commentsAtCursor returns every Change Request anchored over the cursor's line.
func (m model) commentsAtCursor() []daemon.ChangeRequestWire {
	if !m.inStep() || len(m.cursor.lines) == 0 {
		return nil
	}
	line := m.cursor.lines[m.cursor.cursor]
	file := m.view.Step.Excerpts[line.excerpt].File
	var out []daemon.ChangeRequestWire
	for _, cr := range m.view.ChangeRequests {
		if cr.Step == m.view.Position && cr.File == file && line.number >= cr.FirstLine && line.number <= cr.LastLine {
			out = append(out, cr)
		}
	}
	return out
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
		return headerSt.Render("dbn — Overview") + dimSt.Render("  ·  "+pluralize(m.view.StepCount, "Step")+" ahead"+m.coverageSuffix())
	default:
		return headerSt.Render(fmt.Sprintf("dbn — Step %d of %d", m.view.Position, m.view.StepCount)) + dimSt.Render(m.coverageSuffix())
	}
}

func (m model) coverageSuffix() string {
	c := m.view.Coverage
	return fmt.Sprintf("  ·  %d/%d changed lines seen", c.Seen, c.Total)
}

const nbsp = "\u00a0"

// keybar joins shortcut labels with a breakable separator, while the spaces
// inside each label are made non-breaking so a label like "g Brief" never
// splits across a wrap.
func keybar(tokens ...string) string {
	for i, t := range tokens {
		tokens[i] = strings.ReplaceAll(t, " ", nbsp)
	}
	return strings.Join(tokens, "  ·  ")
}

// jumpHint labels the number-jump with the real Step count, and says how to
// reach Steps past 9.
func (m model) navHint() string {
	n := m.view.StepCount
	switch {
	case n <= 1:
		return "→ go to Step"
	case n <= 9:
		return fmt.Sprintf("←/→/1-%d go to Step", n)
	default:
		return "←/→/1-9 go to Step (→ for later)"
	}
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

	b.WriteString(labelSt.Render("Source") + "\n")
	if brief.ProvenanceKind == "stated" {
		b.WriteString("stated — " + brief.ProvenanceCitation + "\n\n")
	} else {
		b.WriteString(warnSt.Render("inferred") + " — reverse-engineered from the changes; trust the narrative accordingly\n\n")
	}

	if len(m.view.Dispositions) > 0 {
		b.WriteString(labelSt.Render("Since the last round") + "\n")
		for _, disposition := range m.view.Dispositions {
			if disposition.Status == "declined" {
				b.WriteString(warnSt.Render("  ✗ declined") + dimSt.Render(fmt.Sprintf("  #%d  %s", disposition.ChangeRequestID, disposition.Location)) + "\n")
				b.WriteString("      " + dimSt.Render("you asked: ") + wrap(disposition.Note) + "\n")
				b.WriteString("      " + dimSt.Render("agent: ") + wrap(disposition.Reasoning) + "\n")
			} else {
				b.WriteString(addSt.Render("  ✓ addressed") + dimSt.Render(fmt.Sprintf("  #%d  %s", disposition.ChangeRequestID, disposition.Location)) + "\n")
				b.WriteString("      " + dimSt.Render("you asked: ") + wrap(disposition.Note) + "\n")
			}
		}
		if len(m.declinedDispositions()) > 0 {
			b.WriteString("\n" + dimSt.Render("  press R to re-raise a declined request") + "\n")
		}
		b.WriteString("\n")
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
		b.WriteString("\n" + dimSt.Render(fmt.Sprintf("── %s:%d–%d (%s)", excerpt.File, excerpt.FirstLine, excerpt.LastLine, beforeAfter(excerpt.Side))) + "\n")
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

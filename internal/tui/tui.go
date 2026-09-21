// Package tui draws the review. It is deliberately thin: keypresses become
// intents sent to the daemon, and what comes back is drawn. All state that
// matters lives in the daemon, so closing this window loses nothing.
package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/probertson/diff-by-numbers/internal/buildinfo"
	"github.com/probertson/diff-by-numbers/internal/daemon"
	"github.com/probertson/diff-by-numbers/internal/selfupdate"
	"github.com/probertson/diff-by-numbers/internal/updatecheck"
)

// Run attaches to the daemon and blocks until the Reviewer quits.
func Run(port int) error {
	starting, err := attach(port)
	if err != nil {
		return err
	}

	program := tea.NewProgram(starting, tea.WithAltScreen())
	final, err := program.Run()
	if err != nil {
		return err
	}
	// A wait that ended because something answered that was not dbn leaves its
	// reason here: the screen is gone by now, so it belongs on stderr, exactly as
	// it would have at startup.
	if ended, ok := final.(model); ok {
		return ended.fatalErr
	}

	return nil
}

// attach builds the model the program starts from. No daemon answering is not a
// failure: the Reviewer often opens the TUI while their agent is still preparing
// the Walkthrough, so the model starts waiting for one and the poll loop picks it
// up when it appears.
func attach(port int) (model, error) {
	client := client{base: fmt.Sprintf("http://127.0.0.1:%d", port)}
	view, err := client.view()
	if err != nil {
		if !worthWaitingThrough(err) {
			return model{}, onPort(port, err)
		}
		return model{client: client, port: port, waiting: true, waitingSince: time.Now()}, nil
	}

	return model{client: client, port: port, view: view}, nil
}

type client struct{ base string }

// errNoDaemon marks nothing answering on the port at all, as against a server
// that answers but is not dbn. Only the first is worth waiting through: a daemon
// may yet be started there, while something else already holding the port will
// never turn into one.
type errNoDaemon struct{ err error }

func (e errNoDaemon) Error() string { return e.err.Error() }
func (e errNoDaemon) Unwrap() error { return e.err }

// worthWaitingThrough reports whether err is the kind a daemon turning up would
// answer. Only silence is: anything that answered has already told us what holds
// the port, and it is not going to change its mind.
func worthWaitingThrough(err error) bool {
	var noDaemon errNoDaemon

	return errors.As(err, &noDaemon)
}

// onPort says which port the trouble was on, which is the one thing the Reviewer
// needs to act on it.
func onPort(port int, err error) error { return fmt.Errorf("port %d: %w", port, err) }

func (c client) view() (*daemon.ViewWire, error) {
	response, err := http.Get(c.base + "/view")
	if err != nil {
		return nil, errNoDaemon{err}
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

// reraise pushes back on a resolution, carrying whatever the Reviewer wrote in
// the note editor — the original wording where they left it alone, a follow-up
// or a counter-argument where they did not.
func (c client) reraise(id int, note string) bool {
	body, _ := json.Marshal(map[string]any{"note": note})
	response, err := http.Post(fmt.Sprintf("%s/reraise/%d", c.base, id), "application/json", bytes.NewReader(body))
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

// statusMsg carries what the daemon says about itself — including its refusing
// to say, which is itself an answer. Only a daemon that cannot be reached at all
// changes nothing here: the header says that far better than any notice could.
type statusMsg struct {
	status *daemon.StatusWire
	err    error
}

// updateMsg carries the update check's answer: the notice to show, or nothing at
// all — which is equally what a failed, disabled or up-to-date check returns.
type updateMsg struct{ notice string }

type model struct {
	client client
	view   *daemon.ViewWire
	// daemonVersion is the build the daemon reported, kept in its own field
	// because the notice it drives is persistent — m.status is a transient line
	// that many keys clear.
	daemonVersion string
	// updateNotice is set once the background update check finds a newer release.
	// Persistent for the same reason daemonVersion is: it is a standing fact about
	// this install, not a response to a keypress.
	updateNotice string
	// replacedNotice tells the Reviewer the agent replaced the Walkthrough under
	// them. Unlike the other notices it is about a moment rather than a standing
	// fact, so the next navigation clears it.
	replacedNotice string
	// waiting is set while no daemon has ever answered. Distinct from lostErr,
	// which is a daemon that answered and then went away: the Reviewer waiting
	// for their agent to start one needs different words from the Reviewer whose
	// review just vanished.
	waiting bool
	// waitingSince is when this wait began, so a wait that drags on can say more
	// than a wait a few seconds old needs to.
	waitingSince time.Time
	// fatalErr ends the program: set when the wait meets something it cannot wait
	// out, so Run can report it on stderr once the screen is gone.
	fatalErr error
	// hintAfter is how long the wait runs before that longer message appears.
	// Zero means waitHintAfter; it is a field so a test need not wait out the real
	// threshold.
	hintAfter time.Duration
	// port is the port this TUI attached to, which the waiting hint names: the
	// Reviewer checking whether their agent is wired up needs to know which one
	// is being watched.
	port             int
	lostErr          error
	viewport         viewport.Model
	cursor           stepCursor
	status           string
	mode             mode
	note             textarea.Model
	commentCursor    int           // selected row in the Comment list
	pendingSel       selectedRun   // the selection awaiting a note
	pendingCode      string        // the code being commented on, shown above the note input
	editingID        int           // >0 when editing an existing Comment rather than adding
	reraisingID      int           // >0 when the editor is composing a push-back on that resolution (#80)
	confirmingDelete bool          // an inline y/n delete confirm is armed (edit screen or List)
	confirmingQuit   bool          // a second-q quit heads-up is armed on an unfinished review
	commentFilter    commentFilter // when active, the List shows only the Comments on one line
	// noteReturn is where leaving the edit screen goes: modeReview when it was
	// opened on a Step, modeList when it was opened from the List. Its zero value
	// is modeReview, which is where every exit went before the List could be an
	// origin.
	noteReturn mode
	// listReturn is where leaving the Comment list goes: modeReview when it was
	// opened from a Step, modeConclusion when it was opened from the conclusion
	// screen. Zero value modeReview, as above.
	listReturn mode
	// reraiseReturn is where leaving the re-raise picker goes: modeReview when R
	// was pressed on the Overview, modeConclusion when it was the last chance
	// before the hand-off. Zero value modeReview, as above.
	reraiseReturn mode
	reraiseCursor int // selected row among the resolutions still open to push-back
	// expanded holds the code of each of this Step's Acknowledgements the Reviewer
	// has expanded, by index. Expansion is viewing, not review state, so it lives
	// here rather than in the daemon.
	expanded map[int][]daemon.ExcerptWire
	// leftSteps is how the Reviewer left each Step of the Walkthrough on screen,
	// by position, so returning to one finds it as it was. It is forgotten when a
	// different Walkthrough arrives.
	leftSteps map[int]leftStep
	// wrapAll turns the Step pane's soft-wrap on for every line rather than the
	// cursor's alone (#77), so a long removal and the addition replacing it can be
	// read side by side. Wrapping is purely how the code is drawn, so it belongs
	// to the TUI, holds for the whole review rather than one Step, and starts off
	// again on the next launch.
	wrapAll bool
	width   int
	height  int
	ready   bool
}

// leftStep is how the Reviewer left a Step: the row the cursor was on and the
// Acknowledgements they had expanded. The pane is windowed around the cursor, so
// restoring the cursor restores the scroll position too. A selection is not
// kept: it is a gesture in progress, and a restored one would make the next
// arrow extend it rather than move.
type leftStep struct {
	spot     codeLine
	expanded map[int][]daemon.ExcerptWire
}

type commentFilter struct {
	active bool
	file   string
	side   string
	line   int
}

type mode int

const (
	modeReview     mode = iota // walking Steps
	modeNote                   // typing a Comment note
	modeList                   // the Comment list
	modeDone                   // the hand-off summary
	modeReraise                // choosing a declined Comment to re-raise
	modeConclusion             // reached by advancing past the last Step: the pre-hand-off on-ramp
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
		m.cursor = newStepCursor(m.view.Step, m.expanded)
	}
}

// leaveStep records how the Reviewer is leaving the Step in view.
func (m *model) leaveStep() {
	if !m.inStep() || len(m.cursor.lines) == 0 {
		return
	}
	if m.leftSteps == nil {
		m.leftSteps = map[int]leftStep{}
	}
	m.leftSteps[m.view.Position] = leftStep{spot: m.cursor.lines[m.cursor.cursor], expanded: m.expanded}
}

// enterStep lays out the Step now in view as the Reviewer last left it, or fresh
// from the top on a first visit.
func (m *model) enterStep() {
	var left leftStep
	visited := false
	if m.view != nil {
		left, visited = m.leftSteps[m.view.Position]
	}
	m.expanded = left.expanded
	m.syncCursor()
	if visited {
		m.cursor.restore(left.spot)
	}
}

// relayout rebuilds the pane after an Acknowledgement expands or collapses, and
// puts the cursor on spot — found by what it is, since the indices have moved.
func (m *model) relayout(spot codeLine) {
	m.cursor = newStepCursor(m.view.Step, m.expanded)
	m.cursor.restore(spot)
}

// toggleAcknowledgement expands or collapses the Acknowledgement the cursor is in:
// on its stop, or anywhere in its expanded code. Expanding lands on the first line
// of its code, if it has any; collapsing returns to its stop.
func (m *model) toggleAcknowledgement() {
	if len(m.view.Step.Acknowledgements) == 0 {
		m.status = "nothing to expand on this Step"
		return
	}
	line := m.cursor.lines[m.cursor.cursor]
	if line.ack < 0 {
		m.status = "move down to an Acknowledgement to expand it"
		return
	}
	k := line.ack
	stop := codeLine{kind: kindStop, ack: k, excerpt: -1}
	m.status = ""
	if m.cursor.isExpanded(k) {
		delete(m.expanded, k)
		m.relayout(stop)
		return
	}
	views, ok := m.client.expand(m.view.Position, k)
	if !ok {
		m.status = "could not fetch the acknowledged code"
		return
	}
	if m.expanded == nil {
		m.expanded = map[int][]daemon.ExcerptWire{}
	}
	m.expanded[k] = views
	m.relayout(stop)
	for i, candidate := range m.cursor.lines {
		if candidate.ack == k && candidate.kind == kindCode {
			m.cursor.cursor = i
			break
		}
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tick(), m.readStatus(), checkForUpdate())
}

// checkForUpdate asks — in the background, so it costs the Reviewer no startup
// delay — whether a newer dbn has been released. A check that fails is silent
// here: the Reviewer came to read code, and `dbn version` is where someone who
// wants to know why gets told.
func checkForUpdate() tea.Cmd {
	return func() tea.Msg {
		result, err := updatecheck.Check(context.Background(), updatecheck.DefaultConfig())
		if err != nil || !result.Available {
			return updateMsg{}
		}
		return updateMsg{notice: selfupdate.Notice(result.Latest)}
	}
}

// readStatus asks the daemon which build it is running, in the background.
func (m model) readStatus() tea.Cmd {
	return func() tea.Msg {
		status, err := daemon.FetchStatus(context.Background(), m.client.base)
		return statusMsg{status: status, err: err}
	}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// anchorBody is a selected run as the daemon's /anchor and /comment
// endpoints take it: the Excerpt and the row at each end, never a range.
func anchorBody(run selectedRun) map[string]any {
	body := map[string]any{
		"excerpt_index": run.excerpt,
		"start":         map[string]any{"side": run.start.side, "line": run.start.line},
		"end":           map[string]any{"side": run.end.side, "line": run.end.line},
	}
	// A run in acknowledged code counts its Excerpt within that Acknowledgement's
	// expansion, so the daemon must be told which one.
	if run.ack >= 0 {
		body["acknowledgement_index"] = run.ack
	}
	return body
}

func (c client) raiseComment(run selectedRun, note string) bool {
	payload := anchorBody(run)
	payload["note"] = note
	body, _ := json.Marshal(payload)
	response, err := http.Post(c.base+"/comment", "application/json", bytes.NewReader(body))
	if err != nil {
		return false
	}
	response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func (c client) editComment(id int, note string) bool {
	body, _ := json.Marshal(map[string]any{"note": note})
	req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/comment/%d", c.base, id), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c client) withdraw(id int) {
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/comment/%d", c.base, id), nil)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
	}
}

// oneExcerptStatus is what the Reviewer is told when a movement is refused
// because it would grow the selection out of the Excerpt it anchored in.
const oneExcerptStatus = "Selection can only apply to lines in one Excerpt"

// noteBoundary reports a movement refused at an Excerpt boundary, and takes the
// message back once a movement succeeds: it describes the keypress that was
// refused, not a standing condition, so leaving it up would have it explain a
// cursor that is plainly moving. Only its own message is cleared — an unrelated
// status is nothing to do with moving the cursor.
func (m *model) noteBoundary(blocked bool) {
	if blocked {
		m.status = oneExcerptStatus
		return
	}
	if m.status == oneExcerptStatus {
		m.status = ""
	}
}

func (m *model) copyAnchor() tea.Cmd {
	run, ok := m.cursor.selection()
	if !ok {
		m.status = oneExcerptStatus
		return nil
	}
	text, ok := m.client.composeAnchor(run)
	if !ok {
		m.status = "could not compose the Anchor"
		return nil
	}
	if copyToClipboard(text) {
		m.status = fmt.Sprintf("copied Anchor for %s — paste it into your agent chat", pluralize(run.rows, "line"))
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
		m.viewport = viewport.New(msg.Width, m.viewportHeight())
		if m.note.Value() == "" && !m.note.Focused() {
			ta := textarea.New()
			ta.Placeholder = "what should change here?"
			ta.CharLimit = 1000
			ta.ShowLineNumbers = false
			m.note = ta
		}
		m.note.SetWidth(max(20, msg.Width-4))
		m.setNoteHeight()
		m.ready = true
		// The pane's rows do not depend on the terminal's size, so a resize keeps
		// the cursor where it is; the cursor is only laid out if it never was.
		if len(m.cursor.lines) == 0 {
			m.syncCursor()
		}
		m.viewport.SetContent(m.content())
		return m, nil

	case tickMsg:
		return m, tea.Batch(m.refresh(), tick())

	case statusMsg:
		switch {
		case msg.err == nil && msg.status != nil:
			m.daemonVersion = msg.status.Version
		case errors.Is(msg.err, daemon.ErrNoStatus):
			// It answered, just not there — a daemon from before the status
			// endpoint existed, which makes it older than this build by
			// definition. That is worth the same notice as any other mismatch.
			m.daemonVersion = olderDaemon
		default:
			return m, nil // unreachable: the header already says the daemon is gone
		}
		m.resizeViewport()
		return m, nil

	case updateMsg:
		m.updateNotice = msg.notice
		m.resizeViewport()
		return m, nil

	case refreshMsg:
		positionChanged := false
		newWalkthrough := false
		newConnection := false
		if msg.err != nil {
			switch {
			case !m.waiting:
				m.lostErr = msg.err
			case !worthWaitingThrough(msg.err):
				// Something took the port and it is not dbn. Waiting will not fix
				// that here any more than it would have at startup.
				m.fatalErr = onPort(m.port, msg.err)

				return m, tea.Quit
			}
			// Otherwise the poll failing is the wait itself: nothing has been lost
			// until something has answered.
		} else {
			if m.view != nil && msg.view != nil && m.view.Position != msg.view.Position {
				positionChanged = true
			}
			// How the Reviewer left each Step belongs to one Walkthrough: a new
			// review, a Revision Round, a Replacement or a restarted daemon starts
			// every Step fresh.
			// A restarted daemon counts its postings from the start again, so the
			// review id it mints is what tells its Walkthrough from the last one.
			if msg.view != nil && (m.view == nil || !msg.view.Posted ||
				msg.view.Posting != m.view.Posting || msg.view.ReviewID != m.view.ReviewID) {
				newWalkthrough = true
			}
			if positionChanged && !newWalkthrough {
				m.leaveStep()
			}
			// Coming back after losing the daemon, the daemon on the other end may
			// not be the one we left — a restart is exactly how it gets replaced by
			// a different build — so ask again who it is. The daemon that ends a
			// wait has never been asked at all, which wants the same question.
			newConnection = m.lostErr != nil || m.waiting
			m.lostErr = nil
			m.waiting = false
			m.view = msg.view
		}
		if newWalkthrough {
			m.leftSteps = nil
		}
		switch {
		case newWalkthrough && m.view.Replaced:
			// The Walkthrough the Reviewer was in has gone, so whatever screen they
			// were on belongs to it: they start again from the Overview.
			m.replacedNotice = replacementNotice(m.view.Comments)
			m.mode = modeReview
			m.resizeViewport()
		case newWalkthrough || positionChanged:
			if m.replacedNotice != "" {
				m.replacedNotice = ""
				m.resizeViewport()
			}
		}
		if positionChanged || newWalkthrough {
			m.enterStep()
			m.viewport.GotoTop()
		}
		if m.view != nil && (m.view.Finished || m.view.Concluded) {
			// The review is done — finished, or concluded outright (an explicit
			// conclude sets Concluded without Finished). Possibly elsewhere (another
			// attached TUI, or the agent). A heads-up about an unfinished review is now
			// moot, and both the walking and pre-finish screens fall to the finished
			// screen — which, when Concluded, shows the complete face.
			m.confirmingQuit = false
			if m.mode == modeReview || m.mode == modeConclusion {
				m.mode = modeDone
			}
		}
		if m.ready {
			m.viewport.SetContent(m.content())
		}
		if newConnection {
			return m, m.readStatus()
		}
		return m, nil

	case tea.KeyMsg:
		key := msg.String()

		if m.confirmingQuit {
			// The heads-up is informational, not an are-you-sure: a repeat q exits,
			// h takes the better path, and esc — the advertised way back — dismisses
			// it, as does any other key, so a stray press cannot strand the reviewer.
			// Only ever armed in modeReview or modeConclusion, so this is safe here.
			switch key {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "h", "H":
				m.confirmingQuit = false
				m.client.intent("/finish")
				m.mode = modeDone
				return m, m.refresh()
			default:
				m.confirmingQuit = false
			}
			return m, nil
		}

		switch m.mode {
		case modeNote:
			return m.updateNote(msg)
		case modeList:
			return m.updateList(key)
		case modeReraise:
			return m.updateReraise(key)
		case modeConclusion:
			return m.updateConclusion(key)
		case modeDone:
			return m.updateDone(key)
		}

		switch key {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			return m.quit()
		case "enter", "n", "right":
			m.status = ""
			// Advancing past the last Step lands on the conclusion screen — the
			// pre-finish bookend to the Overview — rather than silently no-opping.
			// It is TUI-only: the daemon stays at the last Step.
			if m.inStep() && m.view.Position == m.view.StepCount {
				m.mode = modeConclusion
				return m, nil
			}
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
				m.status = "review resumed — add or change anything, then h to hand off again"
				return m, m.refresh()
			}
		case "l", "L":
			m.openList(modeReview)
			return m, nil
		case "R":
			if len(m.reRaisableDispositions()) == 0 {
				// Two different nothings: the agent turned nothing down, or the
				// Reviewer has already pushed back on everything it did.
				if len(m.disputable()) == 0 {
					m.status = "no declined or answered Comments to re-raise"
				} else {
					m.status = "every declined or answered Comment is already re-raised"
				}
				return m, nil
			}
			m.reraiseCursor = 0
			m.reraiseReturn = modeReview
			m.mode = modeReraise
			return m, nil
		case "h", "H":
			m.client.intent("/finish")
			m.mode = modeDone
			return m, m.refresh()
		}

		if m.inStep() {
			// Only a line of code can be selected, commented on or anchored — not an
			// Acknowledgement's stop, and nothing on a Step with no readable code.
			interactive := m.cursor.onCode()
			switch key {
			case "up", "k":
				m.noteBoundary(m.cursor.move(-1))
				return m, nil
			case "down", "j":
				m.noteBoundary(m.cursor.move(1))
				return m, nil
			case "w":
				// Wrapping is only how the code is drawn, so the toggle holds for the
				// whole review rather than this Step, and the cursor does not move.
				m.wrapAll = !m.wrapAll
				m.status = ""
				return m, nil
			case "shift+up":
				if !interactive {
					m.status = m.noInteractionHint()
					return m, nil
				}
				m.status = ""
				m.noteBoundary(m.cursor.extend(-1))
				return m, nil
			case "shift+down":
				if !interactive {
					m.status = m.noInteractionHint()
					return m, nil
				}
				m.status = ""
				m.noteBoundary(m.cursor.extend(1))
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
				m.cursor.sel = -1
				m.status = ""
				return m, nil
			case "x":
				if len(m.cursor.lines) == 0 {
					m.status = "nothing to expand on this Step"
					return m, nil
				}
				m.cursor.sel = -1
				m.toggleAcknowledgement()
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
					m.status = "review is handed off — press r to resume before editing"
					return m, nil
				}
				here := m.commentsAtCursor()
				switch len(here) {
				case 0:
					m.status = "no Comment on this line to edit"
				case 1:
					m.noteReturn = modeReview
					m.editingID = here[0].ID
					m.pendingCode = here[0].Anchor
					m.note.SetValue(here[0].Note)
					m.note.Focus()
					m.setNoteHeight()
					m.mode = modeNote
					return m, textarea.Blink
				default:
					line := m.cursor.lines[m.cursor.cursor]
					m.openList(modeReview)
					m.commentFilter = commentFilter{active: true, file: m.cursor.excerptOf(m.view.Step, line).File, side: line.side, line: line.number}
				}
				return m, nil
			case "c":
				if !interactive {
					m.status = m.noInteractionHint()
					return m, nil
				}
				if m.view.Finished {
					m.status = "review is handed off — press r to resume before commenting"
					return m, nil
				}
				run, ok := m.cursor.selection()
				if !ok {
					m.status = oneExcerptStatus
					return m, nil
				}
				m.editingID = 0 // c always adds a fresh comment, never edits
				m.noteReturn = modeReview
				m.mode = modeNote
				m.note.SetValue("")
				m.note.Focus()
				m.pendingSel = run
				if code, ok := m.client.composeAnchor(run); ok {
					m.pendingCode = code
				} else {
					m.pendingCode = ""
				}
				m.setNoteHeight()
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
		if m.confirmingDelete {
			switch readConfirm(key.String()) {
			case confirmProceed:
				id := m.editingID
				m.confirmingDelete = false
				m.editingID = 0
				m.mode = m.noteReturn // matches esc: back to wherever the edit began
				m.note.Blur()
				m.client.withdraw(id)
				if m.noteReturn == modeList {
					// The List has no status row, so a message set here would go
					// unseen and then surface on the next Step. The List is also one
					// entry shorter now, which can leave the cursor past its end.
					m.status = ""
					m.clampCommentCursor(len(m.filteredComments()) - 1)
				} else {
					m.status = "Comment deleted"
				}
				return m, m.refresh()
			case confirmCancel:
				m.confirmingDelete = false // cancel back into editing, note intact
			}
			return m, nil // confirmIgnore lands here — swallowed, still armed
		}
		switch key.String() {
		case "esc":
			m.editingID = 0
			m.reraisingID = 0
			m.mode = m.noteReturn
			m.note.Blur()
			return m, nil
		case "ctrl+d":
			// Delete only makes sense against an existing Comment; while
			// composing a new one there is nothing yet to delete.
			if m.editingID > 0 {
				m.confirmingDelete = true
			}
			return m, nil
		case "enter":
			note := m.note.Value()
			status := ""
			switch {
			// A re-raise sends whatever is in the box, empty included: the Reviewer
			// picked a resolution to push back on, and clearing the pre-filled text
			// means "as it stood", not "never mind" — esc is how you take it back.
			case m.reraisingID > 0:
				if m.client.reraise(m.reraisingID, note) {
					status = fmt.Sprintf("re-raised Comment #%d — it stands again this round", m.reraisingID)
				} else {
					status = "could not re-raise the Comment"
				}
			case note != "":
				if m.editingID > 0 {
					if m.client.editComment(m.editingID, note) {
						status = "Comment updated"
					} else {
						status = "could not update the Comment"
					}
				} else if m.client.raiseComment(m.pendingSel, note) {
					status = "Comment added"
				} else {
					status = "could not add the Comment"
				}
			}
			if m.noteReturn == modeList {
				// The List has no status row, so the message would go unseen and
				// then surface on the next Step. Clearing rather than skipping the
				// assignment also takes down anything left over from before.
				status = ""
			}
			m.status = status
			m.editingID = 0
			m.reraisingID = 0
			m.cursor.sel = -1
			m.mode = m.noteReturn
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
	list := m.filteredComments()
	if m.confirmingDelete {
		// The armed confirm intercepts esc too, so it cancels the delete rather
		// than falling through to the List's esc-exits-to-review.
		switch readConfirm(key) {
		case confirmProceed:
			if m.commentCursor < len(list) {
				m.client.withdraw(list[m.commentCursor].ID)
				if m.commentCursor > 0 {
					m.commentCursor--
				}
			}
			m.confirmingDelete = false
			return m, m.refresh()
		case confirmCancel:
			m.confirmingDelete = false
		}
		return m, nil // confirmIgnore lands here — swallowed, still armed
	}
	switch key {
	case "esc", "L", "q":
		m.commentFilter = commentFilter{}
		m.mode = m.listReturn
		return m, nil
	case "up", "k":
		if m.commentCursor > 0 {
			m.commentCursor--
		}
		return m, nil
	case "down", "j":
		if m.commentCursor < len(list)-1 {
			m.commentCursor++
		}
		return m, nil
	case "d", "x":
		if m.commentCursor < len(list) {
			m.confirmingDelete = true
		}
		return m, nil
	case "e", "enter":
		if m.commentCursor < len(list) {
			comment := list[m.commentCursor]
			m.noteReturn = modeList
			m.editingID = comment.ID
			m.pendingCode = comment.Anchor
			m.note.SetValue(comment.Note)
			m.note.Focus()
			m.setNoteHeight()
			m.mode = modeNote
			return m, textarea.Blink
		}
		return m, nil
	}
	return m, nil
}

func (m model) updateReraise(key string) (tea.Model, tea.Cmd) {
	offered := m.reRaisableDispositions()
	switch key {
	case "esc", "q", "R":
		m.mode = m.reraiseReturn
		return m, nil
	case "up", "k":
		if m.reraiseCursor > 0 {
			m.reraiseCursor--
		}
		return m, nil
	case "down", "j":
		if m.reraiseCursor < len(offered)-1 {
			m.reraiseCursor++
		}
		return m, nil
	case "enter":
		if m.reraiseCursor < len(offered) {
			// The note editor opens over the original wording rather than sending it
			// straight back: the Reviewer read the agent's reasoning, and a push-back
			// that answers it lands better than the same sentence repeated (#80).
			disposition := offered[m.reraiseCursor]
			m.reraisingID = disposition.CommentID
			// Back to wherever R was pressed, not to a Step: the Reviewer who pushed
			// back from the conclusion screen was on their way out, not in.
			m.noteReturn = m.reraiseReturn
			m.editingID = 0
			m.pendingCode = disposition.Anchor
			m.note.SetValue(disposition.Note)
			m.note.Focus()
			m.setNoteHeight()
			m.mode = modeNote
			return m, textarea.Blink
		}
		return m, nil
	}
	return m, nil
}

// updateConclusion drives the pre-hand-off conclusion screen. It is purely
// navigational and reversible: back returns to the last Step, g jumps to the
// Overview, h is the deliberate hand-off, and q is guarded like everywhere else
// on a review not yet handed off.
func (m model) updateConclusion(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "left", "p", "esc":
		m.mode = modeReview // the daemon never left the last Step
		return m, nil
	case "g":
		m.client.intent("/goto/0")
		m.mode = modeReview
		return m, m.refresh()
	case "l", "L":
		// The full list, whatever the Reviewer was last filtered to on a Step: from
		// here they are looking over everything they raised, not one line of it.
		m.openList(modeConclusion)
		return m, nil
	case "R":
		// This screen carries the count of what is still standing, so R has to work
		// from here: it is the last chance to push back before the hand-off (#80).
		if len(m.reRaisableDispositions()) == 0 {
			m.status = "every declined or answered Comment is already re-raised"
			return m, nil
		}
		m.reraiseCursor = 0
		m.reraiseReturn = modeConclusion
		m.mode = modeReraise
		return m, nil
	case "h", "H":
		m.client.intent("/finish")
		m.mode = modeDone
		return m, m.refresh()
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		return m.quit()
	}
	return m, nil
}

// openList shows the Comment list unfiltered and from the top, remembering the
// screen to return to when it closes.
func (m *model) openList(from mode) {
	// The list has no status row, so a message still standing from a Step would
	// go unread here and be waiting again on the way back.
	m.status = ""
	m.commentFilter = commentFilter{}
	m.commentCursor = 0
	m.listReturn = from
	m.mode = modeList
}

// quit is q's shared behaviour in both modeReview and modeConclusion: arm the
// heads-up on an unfinished review, otherwise quit outright.
func (m model) quit() (tea.Model, tea.Cmd) {
	if m.shouldGuardQuit() {
		m.confirmingQuit = true
		return m, nil
	}
	return m, tea.Quit
}

// shouldGuardQuit reports whether q should raise the unfinished-review heads-up
// rather than quit outright: only when a Walkthrough is posted and neither
// finished nor concluded, since quitting then leaves the agent unable to post the
// next round. A concluded review is over, so quitting it needs no heads-up.
func (m model) shouldGuardQuit() bool {
	return m.view != nil && m.view.Posted && !m.view.Finished && !m.view.Concluded
}

// doneState is which face the finished screen shows.
type doneState int

const (
	doneWaiting  doneState = iota // finished, a Revision Round is coming
	doneRevision                  // a Revision Round has arrived
	doneComplete                  // the review is over — finished having raised nothing
)

// doneStateOf reports the finished screen's face. Precedence matters: a concluded
// review is complete even though it is also finished; a finished flag turned back
// off (while still on the finished screen) means a Revision Round arrived.
func (m model) doneState() doneState {
	switch {
	case m.view != nil && m.view.Concluded:
		return doneComplete
	case m.view != nil && !m.view.Finished:
		return doneRevision
	default:
		return doneWaiting
	}
}

// updateDone drives the handed-off screen. In the revision face enter (or the
// unadvertised r, for muscle memory) proceeds into the new round; otherwise r
// resumes the round for more editing. q always exits — the round is handed off,
// so there is nothing to guard.
func (m model) updateDone(key string) (tea.Model, tea.Cmd) {
	if key == "q" || key == "ctrl+c" {
		return m, tea.Quit // the round is finished — nothing to guard in any state
	}
	if m.doneState() == doneRevision {
		if key == "enter" || key == "r" {
			m.mode = modeReview
			m.status = ""
			return m, m.refresh()
		}
		return m, nil
	}
	if key == "r" {
		m.client.reopen()
		m.mode = modeReview
		m.status = "review resumed — add or change anything, then h to hand off again"
		return m, m.refresh()
	}
	return m, nil
}

// quitGuardMessage reassures that nothing is lost, names the real consequence,
// then offers all three ways out. It counts the pending Comments when
// there are some, so the reassurance is about the reviewer's actual work.
func (m model) quitGuardMessage() string {
	k := 0
	if m.view != nil {
		k = len(m.view.Comments)
	}
	safe := "Nothing will be lost"
	if k > 0 {
		safe = "Your " + pluralize(k, "Comment") + " will not be lost"
	}
	return fmt.Sprintf("Confirm exit? %s, but your agent will not be able to continue the review. Press <esc> to go back, h to hand off the review, or q again to exit anyway.", safe)
}

// declinedDispositions is the subset of the previous round's Comments the
// agent declined — the ones the Reviewer may re-raise.
func (m model) declinedDispositions() []daemon.DispositionWire {
	if m.view == nil {
		return nil
	}
	var out []daemon.DispositionWire
	for _, disposition := range m.view.Dispositions {
		if dispositionStatus(disposition) == "declined" {
			out = append(out, disposition)
		}
	}
	return out
}

// disputable is the previous round's resolutions the Reviewer can still push
// back on: the ones the agent declined or only answered. An addressed Comment is
// left out — the code moved, and there is fresh code to comment on instead.
func (m model) disputable() []daemon.DispositionWire {
	if m.view == nil {
		return nil
	}
	var out []daemon.DispositionWire
	for _, disposition := range m.view.Dispositions {
		if status := dispositionStatus(disposition); status == "declined" || status == "answered" {
			out = append(out, disposition)
		}
	}
	return out
}

// reRaisableDispositions is what the picker offers: the disputable resolutions
// that do not already have a Comment standing against them this round (#80).
// Withdrawing that Comment puts its resolution back on the list.
func (m model) reRaisableDispositions() []daemon.DispositionWire {
	var out []daemon.DispositionWire
	for _, disposition := range m.disputable() {
		if _, ok := m.reRaiseOf(disposition.CommentID); !ok {
			out = append(out, disposition)
		}
	}
	return out
}

// reRaiseOf is the Comment standing against a previous round's resolution, if
// the Reviewer has raised one this round.
func (m model) reRaiseOf(commentID int) (daemon.CommentWire, bool) {
	if m.view == nil {
		return daemon.CommentWire{}, false
	}
	for _, comment := range m.view.Comments {
		if comment.ReRaisedFrom == commentID {
			return comment, true
		}
	}
	return daemon.CommentWire{}, false
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
	// revisionBoxStyle sets the "Revision Round ready" announcement off in a bordered
	// accent box, so a round arriving on the finished screen is announced rather than
	// silently swapping the copy.
	revisionBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(0, 1)
)

func (m model) View() string {
	if !m.ready {
		return "attaching…"
	}

	var body, persistent, stateful string
	switch m.mode {
	case modeNote:
		body = m.noteView()
		persistent = m.noteKeys()
		if m.confirmingDelete {
			stateful = deleteConfirmPrompt
		}
	case modeList:
		body = m.listView()
		persistent = m.listKeys()
		if m.confirmingDelete {
			stateful = deleteConfirmPrompt
		}
	case modeReraise:
		body = m.reraiseView()
		persistent = keybar("↑/↓ move", "enter re-raise", "<esc> back")
	case modeConclusion:
		body = m.conclusionView()
		persistent = keybar("← back", "g Overview", "l list", "h hand off", "q exit")
		if m.confirmingQuit {
			stateful = m.quitGuardMessage()
		}
	case modeDone:
		body = m.doneView()
		if m.doneState() == doneRevision {
			persistent = keybar("enter review revision", "q exit")
		} else {
			persistent = keybar("r resume", "q exit")
		}
	default:
		if m.inStep() {
			body = renderStep(m.view.Step, m.cursor, m.commentedLines(), m.ackComments(), m.width, m.bodyHeight(), m.multiRepo(), m.wrapAll)
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
		if m.confirmingQuit {
			stateful = m.quitGuardMessage()
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
		return keybar("q exit")
	}
	return keybar(m.navHint(), "g Overview", "l list", "h hand off", "q exit")
}

// modeKeys is the blue row: the actions available on the current page only. It
// changes with the cursor — code selection on a line of code, expansion on an
// Acknowledgement — while the global row underneath stays put.
func (m model) modeKeys() string {
	if m.view == nil || !m.view.Posted {
		return ""
	}
	if m.view.Position == 0 {
		tokens := []string{"enter begin", "↑/↓ scroll"}
		// Offered only while something is still open: once every decline and
		// answer has been pushed back on, R has nothing left to do (#80).
		if len(m.reRaisableDispositions()) > 0 {
			tokens = append(tokens, "R re-raise a decline or answer")
		}
		return keybar(tokens...)
	}
	if !m.inStep() || len(m.cursor.lines) == 0 {
		return ""
	}
	if m.cursor.sel >= 0 {
		return keybar("↑/↓ extend", "y copy", "c comment", m.wrapToggleKey(), "<esc> stop selecting")
	}
	tokens := []string{"↑/↓ move"}
	line := m.cursor.lines[m.cursor.cursor]
	if line.kind == kindCode {
		tokens = append(tokens, "<space>/v select", "y copy", "c comment")
		if _, ok := m.commentAtCursor(); ok {
			tokens = append(tokens, "e edit")
		}
	}
	if line.ack >= 0 {
		if m.cursor.isExpanded(line.ack) {
			tokens = append(tokens, "x collapse")
		} else {
			tokens = append(tokens, "x expand")
		}
	}
	tokens = append(tokens, m.wrapToggleKey())
	return keybar(tokens...)
}

// wrapToggleKey names what w will do next, so the label is the outcome rather
// than the state. It is offered while selecting too, since comparing a long
// removal with its replacement is exactly when a selection is being made.
func (m model) wrapToggleKey() string {
	if m.wrapAll {
		return "w wrap cursor line only"
	}
	return "w wrap all lines"
}

// deleteConfirmPrompt is the inline y/n guard the edit screen and the List both
// show as an accent toast above the keybar while a delete is armed.
const deleteConfirmPrompt = "Delete this Comment? (y/n)"

// confirmChoice is how a keystroke lands while an inline delete confirm is armed.
type confirmChoice int

const (
	confirmIgnore  confirmChoice = iota // an unrelated key — swallow it, stay armed
	confirmCancel                       // n/esc — disarm without deleting
	confirmProceed                      // y — disarm and delete
)

// readConfirm interprets a key while a delete confirm is armed. Only y/n/esc are
// live; every other key is ignored (swallowed) so a stray press neither deletes
// nor leaks through to the note or the List cursor.
func readConfirm(key string) confirmChoice {
	switch key {
	case "y":
		return confirmProceed
	case "n", "esc":
		return confirmCancel
	default:
		return confirmIgnore
	}
}

// noteKeys is the edit screen's keybar. It is context-aware: editing an existing
// Comment offers delete, while composing a new one has nothing to delete
// yet. The armed confirm shows as a toast above this row, not in place of it.
func (m model) noteKeys() string {
	if m.editingID > 0 {
		return keybar("enter save", "ctrl+d delete", "<esc> cancel")
	}
	return keybar("enter add", "<esc> cancel")
}

// listKeys is the List's keybar.
func (m model) listKeys() string {
	return keybar("↑/↓ move", "e edit", "d withdraw", "<esc> back")
}

// noInteractionHint explains why selecting, commenting or anchoring is
// unavailable where the cursor is — on an Acknowledgement's stop, or on a Step
// with no readable code.
func (m model) noInteractionHint() string {
	if m.inStep() && len(m.cursor.lines) > 0 && m.cursor.lines[m.cursor.cursor].kind == kindStop {
		if m.cursor.isExpanded(m.cursor.lines[m.cursor.cursor].ack) {
			return "move down into the code to select it"
		}
		return "expand with x to select code"
	}
	return "no readable code on this Step"
}

// noteMaxHeight caps how tall the Comment input grows: it fills the space
// the modal has up to this, and shrinks below it on a short terminal.
const noteMaxHeight = 10

// setNoteHeight sizes the note input to fill the modal, up to noteMaxHeight. The
// title, the code being commented on, and the counter each take rows the input
// cannot, so a short terminal shrinks the input rather than overflowing.
func (m *model) setNoteHeight() {
	codeLines := 1 // the "lines %d-%d" placeholder shown when there is no anchor
	if m.pendingCode != "" {
		codeLines = len(renderAnchorRows(m.pendingCode, m.width))
	}
	// The fixed single-line rows around the input: the header and its blank line,
	// the title and its blank line, the counter, and the keybar — six in all — plus
	// one row for frame()'s minimum gap. The code block takes codeLines on top.
	const fixedRows = 7
	available := m.height - fixedRows - codeLines
	height := min(noteMaxHeight, available)
	if height < 1 {
		height = 1
	}
	m.note.SetHeight(height)
}

func (m model) noteView() string {
	code := m.pendingCode
	if code == "" {
		code = dimSt.Render(pluralize(m.pendingSel.rows, "line"))
	} else {
		code = strings.Join(renderAnchorRows(code, m.width), "\n")
	}
	title := "New Comment"
	if m.editingID > 0 {
		title = "Edit Comment"
	}
	// The counter sits just below the input, right-aligned under its edge, so the
	// invisible 1000-char cap is visible before it is hit.
	counter := lipgloss.NewStyle().Width(m.note.Width()).Align(lipgloss.Right).
		Render(dimSt.Render(fmt.Sprintf("%d/%d", m.note.Length(), m.note.CharLimit)))
	return labelSt.Render(title) + "\n\n" + code + "\n" + m.note.View() + "\n" + counter
}

// listItemIndent is how far the Comment list and the re-raise picker indent an
// item's body under its heading. It is both the padding drawn and the width the
// body loses, so the two cannot drift apart.
const listItemIndent = 5

func (m model) listView() string {
	list := m.filteredComments()
	if len(list) == 0 {
		return dimSt.Render("No Comments to show.")
	}
	title := pluralize(len(list), "Comment")
	if m.commentFilter.active {
		title = fmt.Sprintf("%s on %s:%d", pluralize(len(list), "Comment"), m.commentFilter.file, m.commentFilter.line)
	}
	items := make([][]string, 0, len(list))
	for i, comment := range list {
		items = append(items, m.commentItem(i, comment))
	}
	return m.windowedList(title, items, m.commentCursor)
}

// commentItem draws one Comment of the list: its heading, as much of its Anchor
// as the cap allows, its own text, and the blank row that sets it off from the
// next — the whitespace that was missing when every item ran together (#75).
func (m model) commentItem(i int, comment daemon.CommentWire) []string {
	cursor := "  "
	if i == m.commentCursor {
		cursor = accentSt.Render("▸ ")
	}
	where := comment.Stepless()
	if where == "" {
		where = fmt.Sprintf("Step %d", comment.Step)
	}
	rows := []string{fmt.Sprintf("%s%s  %s", cursor, where, dimSt.Render(comment.Location))}

	// An item's body is indented under its heading, so it has that much less
	// width to wrap in.
	indent := strings.Repeat(" ", listItemIndent)
	body := m.width - listItemIndent
	quote, omitted := capAnchorRows(comment.Anchor, listAnchorCap)
	for _, line := range renderAnchorRows(quote, body) {
		rows = append(rows, indent+dimSt.Render(line))
	}
	if omitted > 0 {
		rows = append(rows, indent+dimSt.Render(fmt.Sprintf("… %d more lines", omitted)))
	}
	for _, line := range strings.Split(wrapTo(comment.Note, body), "\n") {
		rows = append(rows, indent+line)
	}
	return append(rows, "")
}

func (m model) reraiseView() string {
	offered := m.reRaisableDispositions()
	if len(offered) == 0 {
		return dimSt.Render("No declined or answered Comments to re-raise.")
	}
	items := make([][]string, 0, len(offered))
	for i, disposition := range offered {
		items = append(items, m.declinedItem(i, disposition))
	}
	return m.windowedList("Re-raise a declined or answered Comment", items, m.reraiseCursor)
}

// declinedItem draws one resolution of the re-raise picker: what the Reviewer
// asked and what the agent said back, which is what they weigh before pushing.
func (m model) declinedItem(i int, disposition daemon.DispositionWire) []string {
	cursor := "  "
	if i == m.reraiseCursor {
		cursor = accentSt.Render("▸ ")
	}
	indent := strings.Repeat(" ", listItemIndent)
	body := m.width - listItemIndent
	rows := []string{fmt.Sprintf("%s#%d  %s", cursor, disposition.CommentID, dimSt.Render(disposition.Location))}
	rows = append(rows, indentedField(indent, "you asked: ", disposition.Note, body)...)
	said := "agent declined: "
	if dispositionStatus(disposition) == "answered" {
		said = "agent answered: "
	}
	rows = append(rows, indentedField(indent, said, disposition.Response, body)...)
	return append(rows, "")
}

// indentedField draws a labelled line of an item's body, wrapped so a long note
// takes the rows the window thinks it does rather than reflowing in the terminal.
// On a terminal too narrow to hold the label the dim styling is dropped rather
// than misapplied: the label has wrapped, so it is no longer a prefix of the row.
func indentedField(indent, label, text string, width int) []string {
	var rows []string
	for i, line := range strings.Split(wrapTo(label+text, width), "\n") {
		if i == 0 && strings.HasPrefix(line, label) {
			line = dimSt.Render(label) + line[len(label):]
		}
		rows = append(rows, indent+line)
	}
	return rows
}

// windowedList is the frame both the Comment list and the re-raise picker sit
// in: a title that stays put, and the items windowed on the cursor beneath it
// (#78). The title and the blank row under it are the two the items do not get.
func (m model) windowedList(title string, items [][]string, cursor int) string {
	const titleRows = 2
	return labelSt.Render(title) + "\n\n" + windowItems(items, cursor, m.bodyHeight()-titleRows)
}

// doneView is the handed-off screen, with three faces the reviewer can be on
// after handing off: waiting for a Revision Round, a Revision Round has arrived,
// or the review is complete. The copy here is the manual-relay baseline; a later
// change swaps it to push wording once dbn can notify the agent directly.
func (m model) doneView() string {
	if m.view == nil {
		return labelSt.Render("Review handed off")
	}
	switch m.doneState() {
	case doneComplete:
		var b strings.Builder
		b.WriteString(labelSt.Render("Review complete") + "\n\n")
		b.WriteString("You handed off having raised nothing, so the review is over.\n\n")
		b.WriteString(dimSt.Render("Press q to exit — or r to resume, if you changed your mind.") + "\n")
		return b.String()
	case doneRevision:
		var inner strings.Builder
		inner.WriteString(labelSt.Render("Revision Round ready") + "\n")
		if summary := m.dispositionSummary(); summary != "" {
			inner.WriteString(summary + "\n")
		}
		// A decline, or an answer that did not settle it, is what the Reviewer may
		// want to argue with — and nothing on the way in said the argument was
		// available (#75, widened to answers by #80).
		if len(m.disputable()) > 0 {
			inner.WriteString("You can re-raise a decline or an answer with R.\n")
		}
		inner.WriteString(accentSt.Render("Press enter to review it."))
		return revisionBoxStyle.Render(inner.String()) + "\n"
	default: // doneWaiting
		var b strings.Builder
		b.WriteString(labelSt.Render("Review handed off") + "\n\n")
		b.WriteString(m.stepsSeenLine() + "\n\n")
		// A bordered call-out rather than the dim line it replaces: the count is
		// the one thing on this screen the Reviewer must not forget before telling
		// the agent they are done. The text is wrapped to what the border and
		// padding leave, so the box stays inside the terminal.
		inner := m.width - revisionBoxStyle.GetHorizontalFrameSize()
		b.WriteString(revisionBoxStyle.Render(wrapTo(m.commentsWaitingCallOut(), inner)) + "\n\n")
		b.WriteString(dimSt.Render("Or press r to resume your review.") + "\n")
		return b.String()
	}
}

// stepsSeenLine reports how much of the Walkthrough the Reviewer got through.
// Step statuses are exclusive — a flagged Step is one they saw and raised a
// Comment on — so the two are added rather than printed side by side, which read
// as a counting bug (#74). With nothing left unseen the total stands alone.
func (m model) stepsSeenLine() string {
	seen, flagged := m.stepCounts()
	viewed := seen + flagged
	total := m.view.StepCount
	if viewed >= total {
		return fmt.Sprintf("%s seen.", pluralize(total, "Step"))
	}
	return fmt.Sprintf("%d of %s seen (%d unseen).", viewed, pluralize(total, "Step"), total-viewed)
}

// commentsWaitingCallOut is the sentence in the handed-off screen's box: how
// much is waiting for the agent, and what to do about it. "Across N Steps"
// counts flagged Steps, so re-raised and carried-over Comments — which belong to
// no current Step — are named separately; with only those there is no Step span
// to give.
func (m model) commentsWaitingCallOut() string {
	_, flagged := m.stepCounts()
	raised := len(m.view.Comments)
	byKind := steplessCounts(m.view.Comments)
	count := pluralize(raised, "Comment")
	var stepless []string
	for _, kind := range []string{daemon.ReRaised, daemon.CarriedOver} {
		if n := byKind[kind]; n > 0 {
			stepless = append(stepless, fmt.Sprintf("%d %s", n, kind))
		}
	}
	if len(stepless) > 0 {
		count += " (" + strings.Join(stepless, ", ") + ")"
	}
	if flagged > 0 {
		count += " across " + pluralize(flagged, "Step")
	}
	verb := "are"
	if raised == 1 {
		verb = "is"
	}
	return fmt.Sprintf("%s %s waiting for your agent — tell it you're done and it will collect them.", count, verb)
}

// stepCounts tallies how many Steps the reviewer saw and flagged, for the
// finished-screen summary.
func (m model) stepCounts() (seen, flagged int) {
	for _, st := range m.view.StepStatuses {
		switch st {
		case "seen":
			seen++
		case "flagged":
			flagged++
		}
	}
	return seen, flagged
}

// dispositionSummary is the sentence saying how the agent handled the previous
// round's Comments, for the Revision-Round-ready box. A status no Comment received is
// left out, so a round with no questions reads as it always has.
func (m model) dispositionSummary() string {
	counts := map[string]int{}
	for _, disposition := range m.view.Dispositions {
		counts[dispositionStatus(disposition)]++
	}
	var parts []string
	for _, status := range []string{"addressed", "answered", "declined"} {
		if counts[status] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", status, counts[status]))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	list := parts[len(parts)-1]
	if len(parts) > 1 {
		list = strings.Join(parts[:len(parts)-1], ", ") + " and " + list
	}
	return "The agent " + list + " of your Comments."
}

// conclusionView is the pre-hand-off on-ramp reached by advancing past the last
// Step: the status first, then the actions, with "End of review" carried by the
// header the way "Overview" is at the other end.
func (m model) conclusionView() string {
	var b strings.Builder
	if m.view != nil {
		// The count carries the accent on a line of its own rather than sitting
		// mid-sentence: it is the one thing that must register before the hand-off
		// (#74). With nothing raised the hand-off is simply the end, and the
		// invitation to look over what you raised goes with the count.
		raised := len(m.view.Comments)
		if raised > 0 {
			b.WriteString(accentSt.Render(pluralize(raised, "Comment")+" for your agent") + "\n\n")
		} else {
			b.WriteString(dimSt.Render("No Comments — handing off completes the review.") + "\n\n")
		}
		// The status first, then the actions, in the order the conclusion screen's
		// layout fixes for #65, #74 and #75.
		if hint := m.declinesHint(); hint != "" {
			b.WriteString(warnSt.Render(hint) + "\n\n")
		}
		if raised > 0 {
			noun := "Comments"
			if raised == 1 {
				noun = "Comment"
			}
			// Plain text, not the accent the hand-off line carries: looking over what
			// you raised is an invitation, handing off is the deliberate act.
			b.WriteString("Press l to see your " + noun + ".\n\n")
		}
	}
	b.WriteString(accentSt.Render("Press h to hand off to your agent.") + "\n")
	return b.String()
}

// commentAtCursor returns the Comment anchored over the cursor's line, if
// there is one, so it can be edited in place.
func (m model) commentAtCursor() (daemon.CommentWire, bool) {
	if !m.inStep() || !m.cursor.onCode() {
		return daemon.CommentWire{}, false
	}
	line := m.cursor.lines[m.cursor.cursor]
	file := m.cursor.excerptOf(m.view.Step, line).File
	for _, comment := range m.view.Comments {
		if comment.Step == m.view.Position && comment.Covers(file, line.side, line.number) {
			return comment, true
		}
	}
	return daemon.CommentWire{}, false
}

// filteredComments is the List's current contents: every Comment, or just
// those on one line when the List was opened by pressing e on a line that has
// more than one.
func (m model) filteredComments() []daemon.CommentWire {
	all := m.view.Comments
	if !m.commentFilter.active {
		return all
	}
	var out []daemon.CommentWire
	for _, comment := range all {
		if comment.Covers(m.commentFilter.file, m.commentFilter.side, m.commentFilter.line) {
			out = append(out, comment)
		}
	}
	return out
}

// clampCommentCursor pulls the List's cursor back onto a real entry after the
// List has lost one, given the number of entries it will have once the refresh
// lands. An emptied List leaves the cursor at 0, where its empty state shows.
func (m *model) clampCommentCursor(length int) {
	if m.commentCursor >= length {
		m.commentCursor = length - 1
	}
	if m.commentCursor < 0 {
		m.commentCursor = 0
	}
}

// commentsAtCursor returns every Comment anchored over the cursor's line.
func (m model) commentsAtCursor() []daemon.CommentWire {
	if !m.inStep() || !m.cursor.onCode() {
		return nil
	}
	line := m.cursor.lines[m.cursor.cursor]
	file := m.cursor.excerptOf(m.view.Step, line).File
	var out []daemon.CommentWire
	for _, comment := range m.view.Comments {
		if comment.Step == m.view.Position && comment.Covers(file, line.side, line.number) {
			out = append(out, comment)
		}
	}
	return out
}

// commentedLines is the set of "file:side:line" in the current Step that carry a
// Comment, so the diff can mark them — keyed by side so a comment on a
// before-side row does not mark the after-side row that shares its number. A
// Comment spanning a removal and its replacement marks rows on both sides,
// which is why this walks the Anchor's segments rather than one range.
func (m model) commentedLines() map[string]bool {
	out := map[string]bool{}
	if m.view == nil {
		return out
	}
	for _, comment := range m.view.Comments {
		if comment.Step != m.view.Position {
			continue
		}
		for _, segment := range comment.Segments {
			for n := segment.FirstLine; n <= segment.LastLine; n++ {
				out[commentKey(comment.File, segment.Side, n)] = true
			}
		}
	}
	return out
}

// ackComments counts, for each of this Step's Acknowledgements, the Comments
// raised in its expanded code — so a collapsed Acknowledgement still
// shows that a point was made inside it.
func (m model) ackComments() []int {
	if !m.inStep() {
		return nil
	}
	counts := make([]int, len(m.view.Step.Acknowledgements))
	for _, comment := range m.view.Comments {
		if comment.Step != m.view.Position || comment.Acknowledgement == nil {
			continue
		}
		if k := *comment.Acknowledgement; k >= 0 && k < len(counts) {
			counts[k]++
		}
	}
	return counts
}

// commentKey identifies a commented row by file, side, and line — the granularity
// at which a Comment is anchored in a unified diff.
func commentKey(file, side string, line int) string {
	return fmt.Sprintf("%s:%s:%d", file, side, line)
}

// headerHeight is how many rows the header occupies, including the blank line
// under it: two normally, three while a notice is showing. Everything that sizes
// the body measures from here, so a notice appearing takes a row from the body
// rather than pushing the keybar off the bottom.
func (m model) headerHeight() int {
	notice := m.notice()
	if notice == "" {
		return 2
	}
	return 1 + lipgloss.Height(m.wrappedNotice(notice)) + 1
}

// wrappedNotice keeps a notice inside the frame. The reinstall wording carries a
// whole curl command, which is longer than most terminals are wide.
func (m model) wrappedNotice(notice string) string { return wrapTo(notice, m.width) }

// footerHeight is the two rows frame() pins to the bottom: the stateful line and
// the keybar under it.
const footerHeight = 2

func (m model) viewportHeight() int {
	return max(1, m.height-m.headerHeight()-footerHeight)
}

// resizeViewport keeps the scrolling body in step with a notice appearing or
// going away, which happens long after the terminal was sized.
func (m *model) resizeViewport() {
	if m.ready {
		m.viewport.Height = m.viewportHeight()
	}
}

// bodyHeight is what a cursor-driven Step gets to draw in: the same rows the
// viewport gets, less one for the gap frame() always keeps above the footer.
func (m model) bodyHeight() int {
	h := m.viewportHeight() - 1
	if h < 4 {
		return 4
	}
	return h
}

// notice is the persistent line under the header: a condition the Reviewer
// should know about for as long as it holds, unlike m.status, which is a
// transient response to a keypress. There is one slot, and a daemon running a
// different build outranks an available release — it is about the review in
// front of them, and is usually the consequence of acting on the other one.
func (m model) notice() string {
	if mismatch := m.daemonMismatchNotice(); mismatch != "" {
		return mismatch
	}
	if m.replacedNotice != "" {
		return m.replacedNotice
	}
	return m.updateNotice
}

// steplessCounts counts the Comments that belong to no Step of this round, by
// why they belong to none.
func steplessCounts(comments []daemon.CommentWire) map[string]int {
	counts := map[string]int{}
	for _, comment := range comments {
		if kind := comment.Stepless(); kind != "" {
			counts[kind]++
		}
	}
	return counts
}

// replacementNotice says the agent replaced the Walkthrough, and how many of the
// Reviewer's Comments carried over to it — each is still theirs to withdraw if
// the replacement dealt with it.
func replacementNotice(comments []daemon.CommentWire) string {
	carried := steplessCounts(comments)[daemon.CarriedOver]
	if carried == 0 {
		return "The agent replaced this Walkthrough"
	}
	return fmt.Sprintf("The agent replaced this Walkthrough — %s carried over (l to review them)", pluralize(carried, "Comment"))
}

// olderDaemon stands in for the version of a daemon too old to have a status
// endpoint. It reads as the notice's subject, because that is all it is for.
const olderDaemon = "an older build"

// daemonMismatchNotice warns when the daemon is a different build from this TUI,
// which is what `dbn update` leaves behind when it cannot restart a daemon
// mid-review. There is no compatibility contract between the two, so the fix is
// to restart the daemon — but only once this review is done, which is why this
// warns and never refuses: the review in progress lives in that daemon.
func (m model) daemonMismatchNotice() string {
	if m.daemonVersion == "" || m.daemonVersion == buildinfo.Version() {
		return ""
	}
	return fmt.Sprintf("daemon is running %s (this is %s) — restart it after this review",
		m.daemonVersion, buildinfo.Version())
}

func (m model) header() string {
	if notice := m.notice(); notice != "" {
		return m.headerLine() + "\n" + warnSt.Render(m.wrappedNotice(notice))
	}
	return m.headerLine()
}

func (m model) headerLine() string {
	switch {
	case m.waiting:
		return headerSt.Render("dbn") + dimSt.Render(" — waiting for the dbn daemon")
	case m.lostErr != nil:
		return warnSt.Render("dbn — lost the daemon: ") + m.lostErr.Error()
	case m.view == nil || !m.view.Posted:
		return headerSt.Render("dbn") + dimSt.Render(" — no Walkthrough posted")
	case m.mode == modeDone:
		return headerSt.Render("dbn — "+m.doneHeading()) + dimSt.Render(m.coverageSuffix())
	case m.pastTheLastStep():
		return headerSt.Render("dbn — End of review") + dimSt.Render(m.coverageSuffix())
	case m.view.Position == 0:
		return headerSt.Render("dbn — Overview") + dimSt.Render("  ·  "+pluralize(m.view.StepCount, "Step")+" ahead"+m.coverageSuffix())
	default:
		return headerSt.Render(fmt.Sprintf("dbn — Step %d of %d", m.view.Position, m.view.StepCount)) + dimSt.Render(m.coverageSuffix())
	}
}

// pastTheLastStep reports whether the Reviewer is on the conclusion screen or on
// a screen they reached from it. The daemon never leaves the last Step while the
// conclusion screen shows, so the Step position alone would have the header
// announce a Step the Reviewer is not on — the Comment list opened from here
// would read "Step 7 of 7" while showing the Comments of the whole review.
func (m model) pastTheLastStep() bool {
	switch m.mode {
	case modeConclusion:
		return true
	case modeList:
		return m.listReturn == modeConclusion
	case modeNote:
		return m.noteReturn == modeList && m.listReturn == modeConclusion
	}
	return false
}

// doneHeading names the handed-off screen in the header, which otherwise falls
// through to the Step the daemon is still parked on. The round is over either
// way, but a Revision Round waiting is the start of the next one, so calling
// that the end of anything would be wrong.
func (m model) doneHeading() string {
	if m.doneState() == doneRevision {
		return "Revision Round"
	}
	return "End of review"
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
	// Substituted into a slice of its own: the parameter aliases the caller's
	// slice whenever a token list is spread into it, and a caller that reads its
	// list back must not find non-breaking spaces in it.
	labels := make([]string, len(tokens))
	for i, t := range tokens {
		labels[i] = strings.ReplaceAll(t, " ", nbsp)
	}
	return strings.Join(labels, "  ·  ")
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
	if m.waiting {
		return m.waitingView()
	}
	if m.view == nil || !m.view.Posted {
		return dimSt.Render("An Authoring Agent posts a Walkthrough over MCP; it will appear here.")
	}
	if m.view.Position == 0 {
		return m.brief()
	}
	return m.step()
}

// waitingView is what the Reviewer reads while no daemon has answered yet. It
// says whose job starting one is, because the answer — the Authoring Agent's,
// when it posts — is the difference between waiting and being stuck.
func (m model) waitingView() string {
	body := dimSt.Render(wrapTo(
		"The dbn daemon starts when your Authoring Agent posts a Walkthrough. This screen fills in as soon as it does.",
		m.viewport.Width))
	if !m.waitHintDue() {
		return body
	}
	hint := fmt.Sprintf("still waiting — is your agent set up with dbn? (port %d; use -port or $DBN_PORT for another)", m.port)

	return body + "\n\n" + warnSt.Render(wrapTo(hint, m.viewport.Width))
}

// waitHintAfter is how long a wait runs before it stops being unremarkable. Short
// enough to catch a misconfigured agent, long enough that a Reviewer who opened
// the TUI a beat early never sees it.
const waitHintAfter = 30 * time.Second

// waitHintDue reports whether this wait has run long enough to be worth
// explaining rather than merely announcing.
func (m model) waitHintDue() bool {
	after := m.hintAfter
	if after == 0 {
		after = waitHintAfter
	}

	return time.Since(m.waitingSince) >= after
}

func (m model) brief() string {
	var b strings.Builder
	brief := m.view.Brief

	wrap := func(text string) string { return wrapTo(text, m.viewport.Width) }

	b.WriteString(labelSt.Render("Goal") + "\n" + wrap(brief.Ask) + "\n\n")
	b.WriteString(labelSt.Render("Approach") + "\n" + wrap(brief.Approach) + "\n\n")

	b.WriteString(labelSt.Render("Source") + "\n")
	if brief.ProvenanceKind == "stated" {
		b.WriteString(wrap("stated — "+brief.ProvenanceCitation) + "\n\n")
	} else {
		b.WriteString(warnSt.Render("inferred") + " — reverse-engineered from the changes; trust the narrative accordingly\n\n")
	}

	if len(m.view.Dispositions) > 0 {
		b.WriteString(m.sinceTheLastRound(m.viewport.Width))
	}

	b.WriteString(labelSt.Render("Under review") + "\n")
	for _, repository := range m.view.Repositories {
		// Named, not just parenthesised: a bare ref beside a path reads as a branch
		// the work is on, when it is the ref the Change Set is measured from.
		b.WriteString(fmt.Sprintf("  %s  (base: %s)\n", repository.Root, repository.Base))
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
		b.WriteString("\n" + dimSt.Render(fmt.Sprintf("── %s:%d–%d", excerpt.File, excerpt.FirstLine, excerpt.LastLine)) + "\n")
		if excerpt.Problem != "" {
			b.WriteString(warnSt.Render("  cannot show this Excerpt: ") + excerpt.Problem + "\n")
			continue
		}
		for _, line := range excerpt.Lines {
			gutter := gutterSt.Render(fmt.Sprintf("%5d │ ", line.Number))
			if line.Changed {
				sign, style := "+", addSt
				if lineSide(line, excerpt) == "old" {
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

// wrapTo renders text into a block width cells wide, wrapping long lines at word
// boundaries. A width of 1 or less is treated as no limit, so a not-yet-sized
// model does not collapse the text to a sliver. It is the one place the several
// views reach for when they need text to stay inside the frame.
func wrapTo(text string, width int) string {
	if width > 1 {
		return lipgloss.NewStyle().Width(width).Render(text)
	}
	return text
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

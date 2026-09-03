// Package tui draws the review. It is deliberately thin: keypresses become
// intents sent to the daemon, and what comes back is drawn. All state that
// matters lives in the daemon, so closing this window loses nothing.
package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

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

func (c client) advance() {
	response, err := http.Post(c.base+"/advance", "text/plain", nil)
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
	client   client
	view     *daemon.ViewWire
	lostErr  error
	viewport viewport.Model
	width    int
	height   int
	ready    bool
}

func (m model) Init() tea.Cmd {
	return tick()
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
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
		m.ready = true
		m.viewport.SetContent(m.content())
		return m, nil

	case tickMsg:
		return m, tea.Batch(m.refresh(), tick())

	case refreshMsg:
		if msg.err != nil {
			m.lostErr = msg.err
		} else {
			m.lostErr = nil
			m.view = msg.view
		}
		if m.ready {
			m.viewport.SetContent(m.content())
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "enter", " ", "n":
			m.client.advance()
			return m, m.refresh()
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

var (
	subtle   = lipgloss.AdaptiveColor{Light: "#6b6b6b", Dark: "#9a9a9a"}
	accent   = lipgloss.AdaptiveColor{Light: "#005f87", Dark: "#5fd7ff"}
	warn     = lipgloss.AdaptiveColor{Light: "#af5f00", Dark: "#ffaf5f"}
	headerSt = lipgloss.NewStyle().Bold(true).Foreground(accent)
	dimSt    = lipgloss.NewStyle().Foreground(subtle)
	warnSt   = lipgloss.NewStyle().Bold(true).Foreground(warn)
	labelSt  = lipgloss.NewStyle().Bold(true)
	gutterSt = lipgloss.NewStyle().Foreground(subtle)
)

func (m model) View() string {
	if !m.ready {
		return "attaching…"
	}
	header := m.header()
	footer := dimSt.Render(m.footer())
	return header + "\n\n" + m.viewport.View() + "\n" + footer
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
		return "enter begin  ·  ↑/↓ scroll  ·  q quit"
	case m.view.Position < m.view.StepCount:
		return "enter next Step  ·  ↑/↓ scroll  ·  q quit"
	default:
		return "last Step  ·  ↑/↓ scroll  ·  q quit"
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

	b.WriteString(labelSt.Render("Ask") + "\n" + brief.Ask + "\n\n")
	b.WriteString(labelSt.Render("Approach") + "\n" + brief.Approach + "\n\n")

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
		b.WriteString(fmt.Sprintf("  %2d. %s\n", i+1, name))
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
			b.WriteString(gutterSt.Render(fmt.Sprintf("%5d │ ", line.Number)) + line.Text + "\n")
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

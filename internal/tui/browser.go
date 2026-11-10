package tui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os/exec"
	"runtime"

	"github.com/charmbracelet/x/ansi"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// The Browser Renderer is a page the daemon serves for each review, drawing
// the Step the Reviewer is on here. The TUI only opens it and says where it is;
// the page follows the review by itself.

// pageKeyLabel is the keybar entry for opening the page. It names an action,
// so it cannot be read as a third view beside "show source" and "show
// formatted".
const pageKeyLabel = "p open in browser"

// openInBrowser opens a URL in the Reviewer's default browser with the
// system's own opener. It returns once the opener has started, without waiting
// on the browser.
func openInBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	// Left unset, the opener's output goes nowhere rather than over the screen.
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// pageURL is where the page for the review this window has open is served, or
// false at the Inbox, where no review is open. Like every report to dbn about
// the page, it names the review the window's client is on.
func (m model) pageURL() (string, bool) {
	if m.client.review == "" {
		return "", false
	}
	return m.client.base + daemon.PagePath(m.client.review), true
}

// hyperlink makes text a terminal hyperlink to url (OSC 8). A terminal that
// does not know the sequence shows the text alone.
func hyperlink(url, text string) string {
	return ansi.SetHyperlink(url) + text + ansi.ResetHyperlink()
}

// openPage opens the review's page and says where it is, which is all a
// Reviewer whose terminal cannot open a browser needs. Without an opener, or
// when it fails, it only says where.
func (m model) openPage() model {
	url, ok := m.pageURL()
	if !ok {
		return m
	}
	link := hyperlink(url, url)
	if m.openURL == nil || m.openURL(url) != nil {
		m.pageNotice = "open " + link + " in your browser"
		return m
	}
	m.pageNotice = "opened " + link + " in your browser"
	return m
}

// screen is which of the screens the page tells apart this window shows for
// the review it has open: the conclusion screen, or anything reached from it,
// which dbn holds no stop for since it stays on the last Step; or any other,
// which the page draws from where dbn holds the Reviewer.
func (m model) screen() daemon.Screen {
	if m.pastTheLastStep() {
		return daemon.ScreenConclusion
	}
	return daemon.ScreenReview
}

// reportScreenChange tells dbn what changed between the window as it was and
// as it is: a review set aside for the Inbox, a review opened, or a move to or
// from the conclusion screen. Nothing else is reported, since the page follows
// every other move from dbn itself.
func (m model) reportScreenChange(was model) {
	if m.screenReport == nil {
		return
	}
	before, now := was.client.review, m.client.review
	if before == now {
		if now != "" && was.screen() != m.screen() {
			m.screenReport(now, m.screen())
		}
		return
	}
	if before != "" {
		m.screenReport(before, daemon.ScreenInbox)
	}
	if now != "" {
		m.screenReport(now, m.screen())
	}
}

// reportScreen tells dbn which screen the window shows for a review. Like an
// intent, a failure is left for the next poll to surface.
func (c client) reportScreen(reviewID string, screen daemon.Screen) {
	c.on(reviewID).report("/screen", daemon.ScreenWire{Screen: screen})
}

// reportCursor tells dbn where the cursor is on a review, for the review's page
// to follow. Like an intent, a failure is left for the next poll to surface.
func (c client) reportCursor(reviewID string, at daemon.CursorWire) {
	c.on(reviewID).report("/cursor", at)
}

// report posts a report about the page to dbn, fire and forget.
func (c client) report(path string, report any) {
	body, _ := json.Marshal(report)
	response, err := http.Post(c.url(path), "application/json", bytes.NewReader(body))
	if err != nil {
		return
	}
	response.Body.Close()
}

// pageKey is the keybar entry for opening the page, itself a link to it, or
// nothing at the Inbox.
func (m model) pageKey() []string {
	url, ok := m.pageURL()
	if !ok {
		return nil
	}
	return []string{hyperlink(url, pageKeyLabel)}
}

// pageLine is the Overview's line naming the page: its URL as text a terminal
// can pick up or the Reviewer can copy, and a link where the terminal allows.
func (m model) pageLine() string {
	url, ok := m.pageURL()
	if !ok {
		return ""
	}
	return labelSt.Render("In your browser") + "\n  " + hyperlink(url, url) + dimSt.Render("  (p opens it)") + "\n\n"
}

// cursorPlace is where the cursor is, as the page finds it, in the review it
// is on.
type cursorPlace struct {
	review string
	cursor daemon.CursorWire
}

// cursorAt is where the cursor is, or false off a Step. The page draws each
// Document rendered, so in a Document the cursor is its block, even where the
// pane shows the Document as source; in code, and on a line of a Document's
// code block, it is the line. A code block's fence, which the page does not
// draw, is the block. Anywhere the page does not draw, an Acknowledgement, is
// in no Excerpt.
func (m model) cursorAt() (cursorPlace, bool) {
	if !m.inStep() || len(m.cursor.lines) == 0 {
		return cursorPlace{}, false
	}
	line := m.cursor.lines[m.cursor.cursor]
	at := daemon.CursorWire{Step: m.view.Position, Excerpt: -1}
	switch {
	case line.ack >= 0 || line.excerpt < 0:
	case line.codeLine && !fence.MatchString(line.text):
		at.Excerpt, at.Side, at.Line = line.excerpt, line.side, line.number
	case line.block > 0:
		at.Excerpt, at.Block = line.excerpt, line.block
	case m.cursor.source && line.inBlock > 0:
		at.Excerpt, at.Block = line.excerpt, line.inBlock
	default:
		at.Excerpt, at.Side, at.Line = line.excerpt, line.side, line.number
	}
	return cursorPlace{review: m.client.review, cursor: at}, true
}

// reportCursorMove tells dbn where the cursor is, for the review's page to
// follow, when it is somewhere other than before.
func (m model) reportCursorMove(before cursorPlace) {
	if m.cursorReport == nil {
		return
	}
	at, ok := m.cursorAt()
	if !ok || at == before {
		return
	}
	m.cursorReport(at.review, at.cursor)
}

// reportCursor tells dbn where the cursor is, for the review's page to follow,
// wherever it was before.
func (m model) reportCursor() {
	if m.cursorReport == nil {
		return
	}
	if at, ok := m.cursorAt(); ok {
		m.cursorReport(at.review, at.cursor)
	}
}

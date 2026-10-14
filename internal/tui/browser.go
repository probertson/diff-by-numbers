package tui

import (
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
// false at the Inbox, where no review is open.
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

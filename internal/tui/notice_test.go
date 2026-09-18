package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/probertson/diff-by-numbers/internal/buildinfo"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// noticeModel is a walking-a-Step model sized like a real terminal, so a test can
// ask what the header says and how much room the body was left.
func noticeModel(daemonVersion string) model {
	const width, height = 80, 24
	return model{
		width:         width,
		height:        height,
		ready:         true,
		daemonVersion: daemonVersion,
		viewport:      viewport.New(width, height-4),
		view: &daemon.ViewWire{
			Posted:    true,
			StepCount: 2,
			Position:  1,
			StepNames: []string{"one", "two"},
			Step:      &daemon.StepWire{Name: "one", Explanation: "why"},
		},
	}
}

func TestADaemonOnThisBuildRaisesNoNotice(t *testing.T) {
	m := noticeModel(buildinfo.Version())

	if notice := m.notice(); notice != "" {
		t.Errorf("a matching daemon version raised the notice %q", notice)
	}
}

func TestAnUnreadDaemonVersionRaisesNoNotice(t *testing.T) {
	// Before the status read comes back — and whenever it fails — there is nothing
	// to compare, and silence is right: the header covers a daemon that is gone.
	m := noticeModel("")

	if notice := m.notice(); notice != "" {
		t.Errorf("an unknown daemon version raised the notice %q", notice)
	}
}

func TestAMismatchedDaemonVersionNamesBothBuilds(t *testing.T) {
	m := noticeModel("0.1.0")

	notice := m.notice()

	if !strings.Contains(notice, "0.1.0") {
		t.Errorf("the notice does not name the daemon's version: %q", notice)
	}
	if !strings.Contains(notice, buildinfo.Version()) {
		t.Errorf("the notice does not name this build's version: %q", notice)
	}
	if !strings.Contains(notice, "restart") {
		t.Errorf("the notice does not say what to do about it: %q", notice)
	}
	if !strings.Contains(m.View(), notice) {
		t.Error("the notice is not on the screen")
	}
}

// The notice is persistent: it is not m.status, which any number of keys clear.
func TestTheMismatchNoticeSurvivesAKeypressThatClearsTheStatus(t *testing.T) {
	m := noticeModel("0.1.0")
	m.status = "transient"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	after := updated.(model)

	if after.status != "" {
		t.Fatalf("precondition: g did not clear the transient status (%q)", after.status)
	}
	if after.notice() == "" {
		t.Error("the daemon-mismatch notice was cleared by a keypress")
	}
}

func TestANoticeCostsTheBodyARowRatherThanTheKeybar(t *testing.T) {
	quiet := noticeModel(buildinfo.Version())
	warned := noticeModel("0.1.0")

	// The notice must not make the screen taller: the keybar is pinned to the
	// bottom row, and a screen that outgrows the terminal pushes it out of sight.
	if got, want := lipgloss.Height(warned.View()), lipgloss.Height(quiet.View()); got != want {
		t.Errorf("the screen is %d rows tall with a notice showing and %d without", got, want)
	}
	if warned.bodyHeight() != quiet.bodyHeight()-1 {
		t.Errorf("the body gets %d rows with a notice and %d without; want one fewer",
			warned.bodyHeight(), quiet.bodyHeight())
	}
	if warned.viewportHeight() != quiet.viewportHeight()-1 {
		t.Errorf("the viewport gets %d rows with a notice and %d without; want one fewer",
			warned.viewportHeight(), quiet.viewportHeight())
	}
}

func TestTheStatusReadRecordsTheDaemonsVersion(t *testing.T) {
	m := noticeModel("")

	updated, _ := m.Update(statusMsg{status: &daemon.StatusWire{Version: "0.1.0"}})

	if got := updated.(model).daemonVersion; got != "0.1.0" {
		t.Errorf("the model recorded daemon version %q, want 0.1.0", got)
	}
}

func TestAFailedStatusReadLeavesTheKnownVersionAlone(t *testing.T) {
	m := noticeModel("0.1.0")

	updated, _ := m.Update(statusMsg{err: errUnreachable{}})

	if got := updated.(model).daemonVersion; got != "0.1.0" {
		t.Errorf("a failed status read changed the daemon version to %q", got)
	}
}

type errUnreachable struct{}

func (errUnreachable) Error() string { return "connection refused" }

package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// narration is a paragraph of the agent's prose long enough to wrap even across
// a wide terminal. Every word is the same, so any row holding one is narration.
func narration() string {
	return strings.TrimSpace(strings.Repeat("narration ", 60))
}

// codeTail marks the end of a code line longer than any prose width under test.
const codeTail = "CODETAIL"

func narratedStep() *daemon.StepWire {
	return &daemon.StepWire{
		Name: "Narrated", Explanation: narration(),
		Excerpts: []daemon.ExcerptWire{{File: "a.go", Side: "new", FirstLine: 1, LastLine: 1, Lines: []daemon.LineWire{
			{Number: 1, Text: "x := " + strings.Repeat("c", 120) + " " + codeTail, Changed: true, Side: "new"},
		}}},
	}
}

// widestRowWith is the widest row of out that holds word, measured without the
// padding a wrapped block is filled out with, so it is the width the text itself
// reaches. 0 when no row holds it.
func widestRowWith(out, word string) int {
	widest := 0
	for _, row := range strings.Split(out, "\n") {
		if !strings.Contains(row, word) {
			continue
		}
		if w := lipgloss.Width(strings.TrimRight(row, " ")); w > widest {
			widest = w
		}
	}
	return widest
}

func TestAStepExplanationWrapsAtTheProseWidthWhileCodeKeepsThePane(t *testing.T) {
	m := stepModel(t, narratedStep())
	m.width, m.height, m.proseWidth = 200, 40, 72

	out := m.View()

	if w := widestRowWith(out, "narration"); w > 72 || w < 60 {
		t.Errorf("the explanation's widest row is %d cells, want it wrapped at 72:\n%s", w, out)
	}
	if w := widestRowWith(out, codeTail); w <= 72 {
		t.Errorf("the code row is %d cells, want it to keep the pane's width:\n%s", w, out)
	}
	if !strings.Contains(out, keybar(m.navHint(), "g Overview", "l list", "h hand off", "i inbox", "q exit")) {
		t.Errorf("the keybar should keep the pane's width, unwrapped:\n%s", out)
	}
}

func TestAPaneNarrowerThanTheProseWidthWrapsNarrationAtThePane(t *testing.T) {
	m := stepModel(t, narratedStep())
	m.width, m.height, m.proseWidth = 50, 40, 72

	out := m.View()

	if w := widestRowWith(out, "narration"); w > 50 {
		t.Errorf("the explanation's widest row is %d cells, over the 50-cell pane:\n%s", w, out)
	}
}

func TestAProseWidthOfZeroWrapsNarrationAtThePane(t *testing.T) {
	m := stepModel(t, narratedStep())
	m.width, m.height, m.proseWidth = 200, 40, 0

	out := m.View()

	if w := widestRowWith(out, "narration"); w > 200 || w < 180 {
		t.Errorf("the explanation's widest row is %d cells, want it wrapped at the 200-cell pane:\n%s", w, out)
	}
}

func TestTheOverviewWrapsTheBriefAtTheProseWidth(t *testing.T) {
	m := roundModel(200, declined(1))
	m.view.Brief = daemon.BriefWire{Goal: "goal " + narration(), Background: "background " + narration()}
	m.view.Dispositions[0].Response = narration()
	m.proseWidth = 72

	out := m.brief()

	if w := widestRowWith(out, "narration"); w > 72 || w < 60 {
		t.Errorf("the Overview's widest row of narration is %d cells, want it wrapped at 72:\n%s", w, out)
	}
}

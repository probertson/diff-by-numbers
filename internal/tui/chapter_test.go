package tui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// chapteredView is a Round of five Steps in three Chapters, two, one and two,
// whose Step 3 asks the Reviewer a question.
func chapteredView() *daemon.ViewWire {
	return &daemon.ViewWire{
		Posted:    true,
		Posting:   1,
		Brief:     daemon.BriefWire{Goal: "GOAL-the-round", Background: "background"},
		StepNames: []string{"STEP-a", "STEP-b", "STEP-c", "STEP-d", "STEP-e"},
		StepCount: 5,
		Seen:      make([]bool, 5),
		Chapters: []daemon.ChapterWire{
			{Number: 1, Name: "Outline", Intro: "what the Round is about", FirstStep: 1, LastStep: 2},
			{Number: 2, Name: "EL-7 Pack holds", Intro: "the holds come first", FirstStep: 3, LastStep: 3, QuestionCount: 1},
			{Number: 3, Name: "Tidying", Intro: "what is left", FirstStep: 4, LastStep: 5},
		},
		Questions: []daemon.QuestionWire{{ID: 1, Step: 3, Text: "Should the hold be 3 days?"}},
	}
}

// atIntro is the model on a Chapter's intro, as the daemon reports one: the
// position is the Step before the Chapter's first, and no Step is in view.
func atIntro(chapter int) model {
	view := chapteredView()
	view.ChapterIntro = chapter
	view.Position = view.Chapters[chapter-1].FirstStep - 1
	return model{width: 80, height: 30, viewport: viewport.New(80, 30), mode: modeReview, view: view}
}

func TestTheIntroScreenShowsTheChaptersNumberNameIntroAndSteps(t *testing.T) {
	m := atIntro(2)

	out := m.content()

	for _, want := range []string{"2 · EL-7 Pack holds", "the holds come first", "3. STEP-c", "asks you a question"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the intro screen to show %q, got:\n%s", want, out)
		}
	}
	for _, other := range []string{"STEP-b", "STEP-d", "GOAL-the-round"} {
		if strings.Contains(out, other) {
			t.Errorf("expected only the Chapter's own Steps and no Overview, but found %q in:\n%s", other, out)
		}
	}
}

func TestTheIntroScreenMarksOnlyStepsThatAskAQuestion(t *testing.T) {
	m := atIntro(1)

	out := m.content()

	if strings.Contains(out, "asks you") {
		t.Errorf("expected no question mark on a Chapter whose Steps ask nothing, got:\n%s", out)
	}
}

func TestTheIntroWrapsAtTheProseWidth(t *testing.T) {
	m := atIntro(2)
	m.width, m.viewport.Width, m.proseWidth = 120, 120, 30
	m.view.Chapters[1].Intro = strings.Repeat("the intro says what the Chapter covers ", 6)
	m.view.Questions = nil // the Step list is never wrapped, so keep its row short

	out := m.content()

	if widest := widestLine(out); widest > 30 {
		t.Errorf("expected the intro wrapped at the prose width of 30, got a line %d wide:\n%s", widest, out)
	}
}

// The first Chapter's intro sits where the Overview does, before any Step, but
// it is not the Overview.
func TestTheFirstIntroIsNotTheOverview(t *testing.T) {
	m := atIntro(1)
	m.view.RoundQuestions = []daemon.QuestionWire{{ID: 2, Text: "about the whole Round"}}

	header, keys := m.headerLine(), m.modeKeys()

	if strings.Contains(header, "Overview") {
		t.Errorf("expected the header not to call an intro the Overview, got %q", header)
	}
	if !strings.Contains(header, "Steps 1–2 of 5") {
		t.Errorf("expected the header to place the Chapter among the Steps, got %q", header)
	}
	if strings.Contains(keys, "begin") || strings.Contains(keys, "answer") {
		t.Errorf("expected the intro's own keys rather than the Overview's, got %q", keys)
	}
}

func TestAStepHeaderNamesItsChapter(t *testing.T) {
	m := atIntro(2)
	m.view.ChapterIntro = 0
	m.view.Position = 3
	m.view.Step = oneLineStep("held")
	m.view.Step.Number, m.view.Step.Chapter = 3, 2
	m.syncCursor()

	header := m.paneInputs().header()

	if !strings.Contains(header, "2 · EL-7 Pack holds") {
		t.Errorf("expected the Step header to name its Chapter, got:\n%s", header)
	}
}

// recordingIntro is the model on a Chapter's intro, talking to a server that
// records what it is sent.
func recordingIntro(t *testing.T, chapter int) (model, *pathRecorder) {
	t.Helper()
	rec := &pathRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.paths = append(rec.paths, r.URL.Path)
		rec.mu.Unlock()
	}))
	t.Cleanup(server.Close)
	m := atIntro(chapter)
	m.client = client{base: server.URL, review: "a1"}
	return m, rec
}

// The intro is a stop like any other: the Reviewer moves on from it, and back,
// with the keys they always use, and dbn decides where those land.
func TestTheKeysOnAnIntroAskTheDaemonToMove(t *testing.T) {
	for _, tc := range []struct {
		key  tea.KeyMsg
		path string
	}{
		{tea.KeyMsg{Type: tea.KeyEnter}, "/advance"},
		{tea.KeyMsg{Type: tea.KeyRight}, "/advance"},
		{tea.KeyMsg{Type: tea.KeyLeft}, "/back"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")}, "/goto/0"},
	} {
		m, rec := recordingIntro(t, 3)

		after, _ := m.Update(tc.key)

		if !rec.sent(tc.path) {
			t.Errorf("expected %s on an intro to send %s, sent %v", tc.key, tc.path, rec.paths)
		}
		if after.(model).mode != modeReview {
			t.Errorf("expected %s on an intro to stay in the review, got mode %d", tc.key, after.(model).mode)
		}
	}
}

// Advancing from a Chapter's last Step lands on the next Chapter's intro, at
// the same position: the Step is left as it would be for any other stop.
func TestReturningFromAnIntroPutsTheCursorWhereItWasLeft(t *testing.T) {
	step := tallStep()
	m := model{client: client{base: expandServer(t).URL, review: "a1"}, mode: modeReview}
	arriveAt := func(m model, position, intro int, step *daemon.StepWire) model {
		view := chapteredView()
		view.Position, view.ChapterIntro, view.Step = position, intro, step
		after, _ := m.Update(refreshMsg{view: view})
		return after.(model)
	}
	m = arriveAt(m, 2, 0, step)
	for i := 0; i < 25; i++ {
		m = press(m, "down")
	}

	m = arriveAt(m, 2, 2, nil) // on to Chapter 2's intro
	inStepOnIntro := m.inStep()
	m = arriveAt(m, 3, 0, otherStep()) // on to Step 3
	m = arriveAt(m, 2, 2, nil)         // back to the intro
	m = arriveAt(m, 2, 0, step)        // back to Step 2

	if inStepOnIntro {
		t.Error("expected no Step in view on the intro")
	}
	if got := m.cursor.lines[m.cursor.cursor].number; got != 26 {
		t.Errorf("expected the cursor back on line 26, got line %d", got)
	}
}

func TestAStepHeaderNamesNoChapterInARoundWithout(t *testing.T) {
	m := model{width: 80, height: 30, mode: modeReview, view: &daemon.ViewWire{
		Posted: true, Position: 1, StepCount: 1, Step: oneLineStep("held"),
	}}
	m.view.Step.Number = 1
	m.syncCursor()

	header := m.paneInputs().header()

	if strings.Contains(header, "·") {
		t.Errorf("expected no Chapter named in a Round without Chapters, got:\n%s", header)
	}
}

package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// answerPlaceholder is what an empty Answer input says. "Your call" is a real
// answer, and saying so keeps a Reviewer from inventing a decision they do not
// have (ADR-0017).
const answerPlaceholder = "your answer — a decision, or \"your call\""

// questionBoxStyle sets Agent Questions apart from the Explanation around them:
// the agent is asking for a decision, and one run in with the narration is what
// the Reviewer missed (#114). It takes the warning colour, since what it asks
// for is something to stop for.
var questionBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(warn).Padding(0, 1)

// questionBlock draws Agent Questions in their own bordered block, each with
// the Answer so far or marked unanswered, fitted to width.
func questionBlock(questions []daemon.QuestionWire, width int) string {
	inner := width - questionBoxStyle.GetHorizontalFrameSize()
	heading := "Agent Question"
	if len(questions) > 1 {
		heading = fmt.Sprintf("Agent Questions (%d)", len(questions))
	}
	var b strings.Builder
	b.WriteString(warnSt.Render(heading))
	for i, question := range questions {
		text := question.Text
		if len(questions) > 1 {
			text = fmt.Sprintf("%d. %s", i+1, text)
		}
		b.WriteString("\n" + wrapTo(text, inner))
		if question.Answered() {
			b.WriteString("\n" + dimSt.Render(wrapTo("Your answer: "+question.Answer, inner)))
		} else {
			b.WriteString("\n" + accentSt.Render("unanswered — a to answer"))
		}
	}
	return questionBoxStyle.Render(b.String())
}

// answerCounts tallies the Round's Agent Questions the Reviewer has answered
// and has not.
func (m model) answerCounts() (answered, unanswered int) {
	if m.view == nil {
		return 0, 0
	}
	for _, question := range m.view.Questions {
		if question.Answered() {
			answered++
		} else {
			unanswered++
		}
	}
	return answered, unanswered
}

// answerPhrases names what the Round's Agent Questions send the agent — so many
// Answers, so many unanswered — each only when there are some.
func (m model) answerPhrases() []string {
	answered, unanswered := m.answerCounts()
	var phrases []string
	if answered > 0 {
		phrases = append(phrases, pluralize(answered, "Answer"))
	}
	if unanswered > 0 {
		phrases = append(phrases, pluralize(unanswered, "unanswered question"))
	}
	return phrases
}

// andList reads a list as a phrase: "a", "a and b", "a, b and c".
func andList(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// asksMark is what the Overview's list of Steps says of a Step that asks the
// Reviewer something: that it does, never what. A Step's question needs the
// Steps before it for its context, and read cold here it only distracts; the
// mark is so the Reviewer slows down when they reach it (ADR-0017).
func (m model) asksMark(step int) string {
	asked := 0
	for _, question := range m.view.Questions {
		if question.Step == step {
			asked++
		}
	}
	switch asked {
	case 0:
		return ""
	case 1:
		return warnSt.Render("  ◆ asks you a question")
	}
	return warnSt.Render(fmt.Sprintf("  ◆ asks you %d questions", asked))
}

// unansweredSuffix is the Step status line's count of its questions still
// unanswered, gone once every one is.
func (m model) unansweredSuffix() string {
	if !m.inStep() {
		return ""
	}
	unanswered := 0
	for _, question := range m.view.Step.Questions {
		if !question.Answered() {
			unanswered++
		}
	}
	if unanswered == 0 {
		return ""
	}
	return warnSt.Render("  ·  " + pluralize(unanswered, "unanswered question"))
}

// questionsHere are the Agent Questions the Reviewer can answer where they are:
// a Step's own, or on the Overview the Round's.
func (m model) questionsHere() []daemon.QuestionWire {
	switch {
	case m.inStep():
		return m.view.Step.Questions
	case m.view != nil && m.view.Posted && m.view.Position == 0:
		return m.view.RoundQuestions
	}
	return nil
}

// answer is a: it answers the one question here, or offers the several to
// choose from.
func (m model) answer() (tea.Model, tea.Cmd) {
	here := m.questionsHere()
	switch {
	case len(here) == 0:
		m.status = "no Agent Question here to answer"
		return m, nil
	case m.view.Finished:
		m.status = "review is handed off — press r to resume before answering"
		return m, nil
	case len(here) == 1:
		return m, m.openEditor(answerEditor(here[0]), here[0].Answer, modeReview)
	}
	m.status = ""
	m.questionCursor = 0
	m.mode = modeQuestions
	return m, nil
}

// updateQuestions drives the choice between several Agent Questions on one
// Step, or on the Round.
func (m model) updateQuestions(key string) (tea.Model, tea.Cmd) {
	here := m.questionsHere()
	switch key {
	case "esc", "q", "a":
		m.mode = modeReview
		return m, nil
	case "up", "k":
		if m.questionCursor > 0 {
			m.questionCursor--
		}
		return m, nil
	case "down", "j":
		if m.questionCursor < len(here)-1 {
			m.questionCursor++
		}
		return m, nil
	case "enter":
		if m.questionCursor < len(here) {
			chosen := here[m.questionCursor]
			return m, m.openEditor(answerEditor(chosen), chosen.Answer, modeReview)
		}
	}
	return m, nil
}

// questionsView lists the Agent Questions here to choose one to answer.
func (m model) questionsView() string {
	here := m.questionsHere()
	items := make([][]string, 0, len(here))
	for i, question := range here {
		items = append(items, m.questionItem(i, question))
	}
	return m.windowedList("Answer an Agent Question", items, m.questionCursor, "this question continues")
}

// questionItem draws one Agent Question of a list: the question, then the
// Answer so far or that there is none.
func (m model) questionItem(i int, question daemon.QuestionWire) []string {
	cursor := "  "
	if i == m.questionCursor {
		cursor = accentSt.Render("▸ ")
	}
	indent := strings.Repeat(" ", listItemIndent)
	body := m.width - listItemIndent
	var rows []string
	for j, line := range strings.Split(wrapTo(question.Text, m.width-2), "\n") {
		if j == 0 {
			rows = append(rows, cursor+line)
			continue
		}
		rows = append(rows, "  "+line)
	}
	if question.Answered() {
		rows = append(rows, indentedField(indent, "your answer: ", question.Answer, body)...)
	} else {
		rows = append(rows, indent+accentSt.Render("unanswered"))
	}
	return append(rows, "")
}

// answerEditor is the note editor answering one Agent Question, with the
// question drawn above the input so the Reviewer answers what was asked.
func answerEditor(question daemon.QuestionWire) noteEditor {
	editor := noteEditor{
		title:       "Answer",
		placeholder: answerPlaceholder,
		action:      "save",
		context: func(_ model, width int) []string {
			return strings.Split(wrapTo(question.Text, width), "\n")
		},
		submit: func(m model, text string) string {
			return submitAnswer(m, question, text)
		},
	}
	if question.Answered() {
		editor.title = "Edit Answer"
	}
	return editor
}

// submitAnswer puts the Answer. An empty one clears the Answer there was, and
// is no call at all when there was none.
func submitAnswer(m model, question daemon.QuestionWire, text string) string {
	cleared := strings.TrimSpace(text) == ""
	if cleared && !question.Answered() {
		return ""
	}
	if !m.client.answerQuestion(question.ID, text) {
		return "could not save the Answer"
	}
	if cleared {
		return "Answer cleared"
	}
	return "Answer saved"
}

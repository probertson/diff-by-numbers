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
		// A question asked again comes with what was asked and answered
		// before, so the Reviewer carries on from there rather than starting
		// over (ADR-0017).
		for _, earlier := range question.History {
			answered := "you answered: " + earlier.Answer
			if earlier.Answer == "" {
				answered = "you left it unanswered"
			}
			b.WriteString("\n" + dimSt.Render(wrapTo("asked before: "+earlier.Text, inner)))
			b.WriteString("\n" + dimSt.Render(wrapTo(answered, inner)))
		}
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

// askedWhere names where a question was asked: on a Step, or with the Brief
// for one asked on the Round.
func askedWhere(step int) string {
	if step == 0 {
		return "with the Brief"
	}
	return fmt.Sprintf("Step %d", step)
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
// a Step's own, or on the Overview the Round's and any carried over from a
// replaced Round, which belong to no Step.
func (m model) questionsHere() []daemon.QuestionWire {
	switch {
	case m.inStep():
		return m.view.Step.Questions
	case m.view != nil && m.view.Posted && m.view.Position == 0:
		return append(append([]daemon.QuestionWire{}, m.view.RoundQuestions...), carriedQuestions(m.view.Questions)...)
	}
	return nil
}

// carriedQuestions are the answered questions carried over from a Round the
// agent replaced.
func carriedQuestions(questions []daemon.QuestionWire) []daemon.QuestionWire {
	var out []daemon.QuestionWire
	for _, question := range questions {
		if question.CarriedOver {
			out = append(out, question)
		}
	}
	return out
}

// answer is a: it answers the one question here, or offers the several to
// choose from. A carried-over question is always offered in the list, which is
// where it can be withdrawn.
func (m model) answer() (tea.Model, tea.Cmd) {
	here := m.questionsHere()
	switch {
	case len(here) == 0:
		m.status = "no Agent Question here to answer"
		return m, nil
	case m.view.Finished:
		m.status = "review is handed off — press r to resume before answering"
		return m, nil
	case len(here) == 1 && !here[0].CarriedOver:
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
	if m.confirmingDelete {
		switch readConfirm(key) {
		case confirmProceed:
			m.confirmingDelete = false
			if m.questionCursor < len(here) {
				if refusal := m.client.withdrawQuestion(here[m.questionCursor].ID); refusal != "" {
					m.status = refusal
					return m, nil
				}
			}
			m.mode = modeReview
			return m, m.refresh()
		case confirmCancel:
			m.confirmingDelete = false
		}
		return m, nil // confirmIgnore lands here — swallowed, still armed
	}
	m.status = ""
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
	case "d", "x":
		if m.questionCursor < len(here) && here[m.questionCursor].CarriedOver {
			m.confirmingDelete = true
			return m, nil
		}
		m.status = "only a question carried over from a replaced Round can be withdrawn"
	}
	return m, nil
}

// withdrawQuestionPrompt is the inline y/n guard on withdrawing a carried-over
// question.
const withdrawQuestionPrompt = "Withdraw this carried-over question and its Answer? (y/n)"

// carriedOverQuestions draws the answered questions a replacement kept on the
// Overview: they belong to no Step of this Round, and the Reviewer may withdraw
// any the replacement made moot.
func (m model) carriedOverQuestions(carried []daemon.QuestionWire, width int) string {
	var b strings.Builder
	b.WriteString(labelSt.Render("Carried over from the replaced Round") + "\n")
	for _, question := range carried {
		b.WriteString(strings.Repeat(" ", dispositionIndent) + dimSt.Render(fmt.Sprintf("#%d", question.ID)) + "\n")
		for _, row := range hangingField("agent asked: ", question.Text, dimSt, width) {
			b.WriteString(row + "\n")
		}
		for _, row := range hangingField("you answered: ", question.Answer, dimSt, width) {
			b.WriteString(row + "\n")
		}
	}
	b.WriteString(strings.Repeat(" ", dispositionIndent) + dimSt.Render("a to change an Answer or withdraw a question") + "\n\n")
	return b.String()
}

// questionsKeys is the question list's keybar, offering withdrawal only when
// there is something carried over to withdraw.
func (m model) questionsKeys() string {
	tokens := []string{"↑/↓ move", "enter answer"}
	if len(carriedQuestions(m.questionsHere())) > 0 {
		tokens = append(tokens, "d withdraw carried over")
	}
	return keybar(append(tokens, "<esc> back")...)
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
	if question.CarriedOver {
		rows = append(rows, indent+dimSt.Render("carried over from the replaced Round"))
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
	switch {
	case cleared && !question.Answered():
		return ""
	case cleared && question.CarriedOver:
		// Only answered questions carry over, so dbn refuses to clear one.
		return "a carried-over question keeps its Answer — withdraw it from the list (a, then d) instead"
	}
	if !m.client.answerQuestion(question.ID, text) {
		return "could not save the Answer"
	}
	if cleared {
		return "Answer cleared"
	}
	return "Answer saved"
}

// unansweredQuestions are the Round's Agent Questions the Reviewer has not
// answered, in the order they meet them.
func (m model) unansweredQuestions() []daemon.QuestionWire {
	if m.view == nil {
		return nil
	}
	var out []daemon.QuestionWire
	for _, question := range m.view.Questions {
		if !question.Answered() {
			out = append(out, question)
		}
	}
	return out
}

// updateHandOffCheck drives the list of unanswered questions shown before a
// Hand Off: go to one, hand off anyway, or go back.
func (m model) updateHandOffCheck(key string) (tea.Model, tea.Cmd) {
	unanswered := m.unansweredQuestions()
	switch key {
	case "esc", "n":
		m.mode = m.handOffReturn
		return m, nil
	case "up", "k":
		if m.questionCursor > 0 {
			m.questionCursor--
		}
		return m, nil
	case "down", "j":
		if m.questionCursor < len(unanswered)-1 {
			m.questionCursor++
		}
		return m, nil
	case "enter":
		if m.questionCursor < len(unanswered) {
			// The Overview, for a question asked on the Round.
			m.client.intent(fmt.Sprintf("/goto/%d", unanswered[m.questionCursor].Step))
			m.mode = modeReview
			return m, m.refresh()
		}
		return m, nil
	case "h", "H":
		return m.finish()
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

// handOffCheckView lists the unanswered questions, each with where it was
// asked, and says what handing off anyway means for them.
func (m model) handOffCheckView() string {
	unanswered := m.unansweredQuestions()
	items := make([][]string, 0, len(unanswered))
	for i, question := range unanswered {
		cursor := "  "
		if i == m.questionCursor {
			cursor = accentSt.Render("▸ ")
		}
		rows := []string{cursor + dimSt.Render(askedWhere(question.Step))}
		for _, line := range strings.Split(wrapTo(question.Text, m.width-listItemIndent), "\n") {
			rows = append(rows, strings.Repeat(" ", listItemIndent)+line)
		}
		items = append(items, append(rows, ""))
	}
	title := pluralize(len(unanswered), "Agent Question") + " unanswered"
	lead := wrapTo("They go back to your agent marked unanswered, and it will go ahead on its own judgment or ask again. Answer them first, or hand off anyway.", m.width)
	const leadRows = 2
	return labelSt.Render(title) + "\n\n" + dimSt.Render(lead) + "\n\n" +
		windowItems(items, m.questionCursor, m.bodyHeight()-leadRows-lipgloss.Height(lead)-1, m.width, "this question continues")
}

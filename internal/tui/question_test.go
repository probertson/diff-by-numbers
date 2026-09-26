package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// answerRecorder answers the TUI's requests and keeps the Answers it was sent.
type answerRecorder struct {
	paths   []string
	answers []string
}

func answerServer(t *testing.T) (*answerRecorder, string) {
	t.Helper()
	rec := &answerRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/question/") {
			rec.paths = append(rec.paths, r.Method+" "+r.URL.Path)
			return
		}
		if !strings.Contains(r.URL.Path, "/answer/") {
			return
		}
		var body struct {
			Answer string `json:"answer"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		rec.paths = append(rec.paths, r.Method+" "+r.URL.Path)
		rec.answers = append(rec.answers, body.Answer)
	}))
	t.Cleanup(server.Close)
	return rec, server.URL
}

// askingStep is tallStep with Agent Questions on it.
func askingStep(questions ...daemon.QuestionWire) *daemon.StepWire {
	step := tallStep()
	step.Questions = questions
	return step
}

func question(id int, text, answer string) daemon.QuestionWire {
	return daemon.QuestionWire{ID: id, Step: 1, Text: text, Answer: answer}
}

// askingModel is a Step with Agent Questions, talking to a server that records
// the Answers it is sent.
func askingModel(t *testing.T, questions ...daemon.QuestionWire) (model, *answerRecorder) {
	t.Helper()
	rec, base := answerServer(t)
	m := model{
		client: client{base: base, review: "a1"},
		mode:   modeReview,
		view:   &daemon.ViewWire{Posted: true, Position: 1, StepCount: 1, Step: askingStep(questions...)},
		width:  80, height: 40, ready: true,
		note: newNote(80),
	}
	m.syncCursor()
	return m, rec
}

func TestAStepShowsItsQuestionsInTheirOwnBlockBeforeTheCode(t *testing.T) {
	step := askingStep(question(1, "Should the retry cap be 3 or 5?", ""))

	out := renderStep(step, newStepCursor(step, nil), map[string]bool{}, nil, 80, 40, false, false)

	block := strings.Index(out, "Agent Question")
	asked := strings.Index(out, "Should the retry cap be 3 or 5?")
	code := strings.Index(out, "code line 1")
	if block < 0 || asked < 0 {
		t.Fatalf("expected a labelled question block, got:\n%s", out)
	}
	if !(strings.Index(out, "the explanation") < block && asked < code) {
		t.Errorf("expected the question after the explanation and before the code, got:\n%s", out)
	}
	if !strings.Contains(out, "╭") {
		t.Errorf("expected the question set off in a bordered block, got:\n%s", out)
	}
}

func TestTheQuestionBlockSaysWhetherItIsAnswered(t *testing.T) {
	step := askingStep(question(1, "3 or 5?", "3, the upstream is steady"), question(2, "keep the name?", ""))

	out := flatten(renderStep(step, newStepCursor(step, nil), map[string]bool{}, nil, 80, 40, false, false))

	if !strings.Contains(out, "Your answer: 3, the upstream is steady") {
		t.Errorf("expected the Answer given, got:\n%s", out)
	}
	if !strings.Contains(out, "unanswered") {
		t.Errorf("expected the other marked unanswered, got:\n%s", out)
	}
}

func TestThePlainStepRenderingShowsTheQuestionsToo(t *testing.T) {
	m := model{width: 80, view: &daemon.ViewWire{Posted: true, Position: 1, StepCount: 1,
		Step: askingStep(question(1, "3 or 5?", ""))}}

	out := m.step()

	if !strings.Contains(out, "Agent Question") || !strings.Contains(out, "3 or 5?") {
		t.Errorf("expected the plain rendering to carry the question block, got:\n%s", out)
	}
}

func TestTheCodePaneSizesItselfAroundTheQuestionBlock(t *testing.T) {
	step := askingStep(question(1, strings.Repeat("a long question that wraps ", 12), ""))
	const width, height = 80, 24

	out := renderStep(step, newStepCursor(step, nil), map[string]bool{}, nil, width, height, false, false)

	if got := lipgloss.Height(out); got > height {
		t.Errorf("the Step is %d rows, over the %d it was given", got, height)
	}
	if over := widestLine(out); over > width {
		t.Errorf("the question block is %d cells wide, over the %d it was given", over, width)
	}
}

func TestAStepWithoutQuestionsShowsNoQuestionBlock(t *testing.T) {
	step := tallStep()

	out := renderStep(step, newStepCursor(step, nil), map[string]bool{}, nil, 80, 40, false, false)

	if strings.Contains(out, "Agent Question") {
		t.Errorf("a Step that asks nothing should render as it always did, got:\n%s", out)
	}
}

func TestAOpensTheAnswerEditorOnTheStepsQuestion(t *testing.T) {
	m, _ := askingModel(t, question(4, "3 or 5?", "3"))

	after := press(m, "a")

	if after.mode != modeNote {
		t.Fatalf("expected a to open the editor, got mode %v", after.mode)
	}
	if !strings.Contains(after.noteView(), "3 or 5?") {
		t.Errorf("expected the editor to show the question being answered, got:\n%s", after.noteView())
	}
	if after.note.Value() != "3" {
		t.Errorf("expected the editor to hold the Answer so far, got %q", after.note.Value())
	}
}

func TestSavingAnAnswerSendsItAndReturnsToTheStep(t *testing.T) {
	m, rec := askingModel(t, question(4, "3 or 5?", ""))
	m = press(m, "a")
	m.note.SetValue("5")

	saved, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	sm := saved.(model)

	if len(rec.answers) != 1 || rec.answers[0] != "5" || rec.paths[0] != "PUT /reviews/a1/answer/4" {
		t.Errorf("expected the Answer put to question 4, got %v %v", rec.paths, rec.answers)
	}
	if sm.mode != modeReview {
		t.Errorf("expected to return to the Step, got mode %v", sm.mode)
	}
}

func TestSavingAnEmptyAnswerClearsIt(t *testing.T) {
	m, rec := askingModel(t, question(4, "3 or 5?", "3"))
	m = press(m, "a")
	m.note.SetValue("")

	saved, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if len(rec.answers) != 1 || rec.answers[0] != "" {
		t.Errorf("expected an empty Answer to be sent to clear it, got %v", rec.answers)
	}
	if status := saved.(model).status; !strings.Contains(status, "cleared") {
		t.Errorf("expected the status to say the Answer was cleared, got %q", status)
	}
}

func TestAnswersAreLockedOnceHandedOff(t *testing.T) {
	m, _ := askingModel(t, question(4, "3 or 5?", ""))
	m.view.Finished = true

	after := press(m, "a")

	if after.mode == modeNote {
		t.Error("a handed-off Round's Answers are locked, so a must not open the editor")
	}
	if !strings.Contains(after.status, "resume") {
		t.Errorf("expected to be told to resume first, got %q", after.status)
	}
}

func TestAWithSeveralQuestionsOffersThemToChooseFrom(t *testing.T) {
	m, _ := askingModel(t, question(4, "3 or 5?", ""), question(5, "keep the name?", ""))

	picking := press(m, "a")
	picking = press(picking, "down")
	answering, _ := picking.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if picking.mode != modeQuestions {
		t.Fatalf("expected a to offer the Step's questions, got mode %v", picking.mode)
	}
	if !strings.Contains(picking.questionsView(), "keep the name?") {
		t.Errorf("expected the picker to list the questions, got:\n%s", picking.questionsView())
	}
	if am := answering.(model); am.mode != modeNote || !strings.Contains(am.noteView(), "keep the name?") {
		t.Errorf("expected enter to answer the question under the cursor, got mode %v:\n%s", am.mode, am.noteView())
	}
}

func TestTheStepKeybarOffersAnsweringOnlyWhenThereIsAQuestion(t *testing.T) {
	asking, _ := askingModel(t, question(4, "3 or 5?", ""))
	quiet := stepModel(t, tallStep())

	if !strings.Contains(asking.modeKeys(), "a"+nbsp+"answer") {
		t.Errorf("expected a Step with a question to offer a answer, got %q", asking.modeKeys())
	}
	if strings.Contains(quiet.modeKeys(), "answer") {
		t.Errorf("a Step that asks nothing has nothing to answer, got %q", quiet.modeKeys())
	}
}

func TestTheConclusionScreenDoesNotPromiseTheEndWhenQuestionsWereAsked(t *testing.T) {
	m := concludingModel()
	m.view.Questions = []daemon.QuestionWire{question(1, "3 or 5?", "3"), question(2, "keep the name?", "")}

	out := flatten(m.conclusionView())

	if strings.Contains(out, "completes the review") {
		t.Errorf("a Round with questions is not concluded at Hand Off, got:\n%s", out)
	}
	if !strings.Contains(out, "1 agent question answered and 1 unanswered") || strings.Contains(out, "for your agent") {
		t.Errorf("expected the Answers counted for the agent, got:\n%s", out)
	}
}

func TestTheHandedOffScreenCountsAnswersAlongsideComments(t *testing.T) {
	m := handedOffModel([]string{"flagged"}, daemon.CommentWire{ID: 1, Step: 1})
	m.view.Questions = []daemon.QuestionWire{question(1, "3 or 5?", "3")}

	out := flatten(m.doneView())

	if !strings.Contains(out, "1 Comment across 1 Step is waiting for your agent. 1 answer for your agent.") {
		t.Errorf("expected the Answer counted with the Comment, got:\n%s", out)
	}
}

func TestTheHandedOffScreenCountsAnswersWhenNoCommentWasRaised(t *testing.T) {
	m := handedOffModel([]string{"seen"})
	m.view.Questions = []daemon.QuestionWire{question(1, "3 or 5?", "")}

	out := flatten(m.doneView())

	if !strings.Contains(out, "1 unanswered agent question. Tell your agent") {
		t.Errorf("expected the unanswered question counted, got:\n%s", out)
	}
}

// overviewAsking is the Overview of a Round with Agent Questions on it.
func overviewAsking(t *testing.T, questions ...daemon.QuestionWire) (model, *answerRecorder) {
	t.Helper()
	rec, base := answerServer(t)
	m := roundModel(80)
	m.client = client{base: base, review: "a1"}
	m.note = newNote(80)
	m.width, m.height, m.ready = 80, 40, true
	for i := range questions {
		questions[i].Step = 0
	}
	m.view.RoundQuestions = questions
	m.view.Questions = questions
	return m, rec
}

func TestTheOverviewShowsRoundLevelQuestionsWithTheBrief(t *testing.T) {
	m, _ := overviewAsking(t, question(1, "is threading the id the right approach?", ""))

	out := m.brief()

	approach := strings.Index(out, "Approach")
	asked := strings.Index(out, "is threading the id the right approach?")
	steps := strings.Index(out, "Steps")
	if asked < 0 || !strings.Contains(out, "Agent Question") {
		t.Fatalf("expected the Round-level question in its block, got:\n%s", out)
	}
	if !(approach < asked && asked < steps) {
		t.Errorf("expected the question with the Brief, after the approach and before the Steps, got:\n%s", out)
	}
}

func TestAOnTheOverviewAnswersARoundLevelQuestion(t *testing.T) {
	m, rec := overviewAsking(t, question(3, "right approach?", ""))

	answering := press(m, "a")
	answering.note.SetValue("yes")
	saved, _ := answering.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if answering.mode != modeNote || !strings.Contains(answering.noteView(), "right approach?") {
		t.Fatalf("expected a on the Overview to answer the Round's question, got mode %v", answering.mode)
	}
	if len(rec.paths) != 1 || rec.paths[0] != "PUT /reviews/a1/answer/3" || rec.answers[0] != "yes" {
		t.Errorf("expected the Answer put to question 3, got %v %v", rec.paths, rec.answers)
	}
	if saved.(model).mode != modeReview {
		t.Errorf("expected to return to the Overview, got mode %v", saved.(model).mode)
	}
}

func TestTheOverviewKeybarOffersAnsweringOnlyWhenTheRoundAsks(t *testing.T) {
	asking, _ := overviewAsking(t, question(3, "right approach?", ""))
	quiet := roundModel(80)

	if !strings.Contains(asking.modeKeys(), "a"+nbsp+"answer") {
		t.Errorf("expected the Overview to offer a answer, got %q", asking.modeKeys())
	}
	if strings.Contains(quiet.modeKeys(), "answer") {
		t.Errorf("a Round that asks nothing has nothing to answer, got %q", quiet.modeKeys())
	}
}

// overviewOfSteps is the Overview of a three-Step Round asking the given
// questions.
func overviewOfSteps(questions ...daemon.QuestionWire) model {
	m := roundModel(80)
	m.view.StepNames = []string{"one", "two", "three"}
	m.view.StepCount = 3
	m.view.Seen = []bool{false, false, false}
	m.view.Questions = questions
	return m
}

func TestTheOverviewMarksAStepThatAsksButNotWhatItAsks(t *testing.T) {
	asked := question(1, "3 or 5 retries?", "")
	asked.Step = 2
	m := overviewOfSteps(asked)

	out := m.brief()

	two := lineWith(t, out, "2. two")
	if !strings.Contains(two, "asks you") {
		t.Errorf("expected Step 2 marked as asking something, got %q", two)
	}
	if strings.Contains(out, "3 or 5 retries?") {
		t.Errorf("a Step's question is read in its Step, never on the Overview, got:\n%s", out)
	}
	if strings.Contains(lineWith(t, out, "1. one"), "asks") || strings.Contains(lineWith(t, out, "3. three"), "asks") {
		t.Errorf("expected only Step 2 marked, got:\n%s", out)
	}
}

func TestTheStepStatusLineCountsItsUnansweredQuestions(t *testing.T) {
	m, _ := askingModel(t, question(4, "3 or 5?", ""), question(5, "keep the name?", ""))

	waiting := m.headerLine()
	m.view.Step.Questions[0].Answer = "3"
	one := m.headerLine()
	m.view.Step.Questions[1].Answer = "yes"
	none := m.headerLine()

	if !strings.Contains(waiting, "2 unanswered questions") {
		t.Errorf("expected the status line to count 2 unanswered questions, got %q", waiting)
	}
	if !strings.Contains(one, "1 unanswered question") {
		t.Errorf("expected the count to fall as questions are answered, got %q", one)
	}
	if strings.Contains(none, "unanswered") {
		t.Errorf("expected the count gone once every question is answered, got %q", none)
	}
}

func TestAStepWithoutQuestionsHasTheStatusLineItAlwaysHad(t *testing.T) {
	m := stepModel(t, tallStep())

	if out := m.headerLine(); strings.Contains(out, "question") {
		t.Errorf("a Step that asks nothing should read as it always did, got %q", out)
	}
}

func accountedQuestion(id int, text, answer, status, response string) daemon.AccountedQuestionWire {
	return daemon.AccountedQuestionWire{
		Question: daemon.QuestionWire{ID: id, Step: 2, Text: text, Answer: answer},
		Status:   status, Response: response,
	}
}

func TestTheRevisionOverviewShowsEachEarlierQuestionWithItsAnswerAndStatus(t *testing.T) {
	m := roundModel(80)
	m.view.AccountedQuestions = []daemon.AccountedQuestionWire{
		accountedQuestion(1, "3 or 5 retries?", "5", "addressed", "raised the cap to 5"),
		accountedQuestion(2, "keep the old name?", "", "agents_call", "kept it, since callers use it"),
	}

	out := m.brief()
	flat := flatten(out)

	for _, want := range []string{
		"Agent Questions from the last round",
		"Agent's call (1)", "keep the old name?", "unanswered", "kept it, since callers use it",
		"Addressed (1)", "3 or 5 retries?", "you answered: 5", "raised the cap to 5",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("expected %q on the Overview, got:\n%s", want, out)
		}
	}
	if strings.Index(out, "Agent's call") > strings.Index(out, "Addressed") {
		t.Errorf("the agent's own calls come first: the Reviewer never decided them, got:\n%s", out)
	}
	if strings.Index(out, "Agent Questions from the last round") > strings.Index(out, "Steps") {
		t.Errorf("expected the earlier questions before the Steps, got:\n%s", out)
	}
}

func TestAnOverviewWithNoEarlierQuestionsSaysNothingOfThem(t *testing.T) {
	m := roundModel(80, addressed(1))

	if out := m.brief(); strings.Contains(out, "Agent Questions from") {
		t.Errorf("a round that asked nothing before should not announce it, got:\n%s", out)
	}
}

func carriedQuestion(id int, text, answer string) daemon.QuestionWire {
	return daemon.QuestionWire{ID: id, Text: text, Answer: answer, CarriedOver: true}
}

func TestAReplacementIsAnnouncedWithTheAnswersItCarriedOver(t *testing.T) {
	m := arrive(model{mode: modeReview}, 1, 2, oneLineStep("before"))

	after, _ := m.Update(refreshMsg{view: &daemon.ViewWire{
		Posted: true, Posting: 2, Replaced: true, StepCount: 2,
		Comments:  carried(2),
		Questions: []daemon.QuestionWire{carriedQuestion(1, "3 or 5?", "5")},
	}})

	want := "The agent replaced this Round — 2 Comments and 1 answered question carried over (l and a to review them)"
	if got := after.(model).notice(); got != want {
		t.Errorf("expected\n%q\ngot\n%q", want, got)
	}
}

func TestTheOverviewShowsCarriedOverQuestionsWithTheirAnswers(t *testing.T) {
	m, _ := overviewAsking(t)
	m.view.Questions = []daemon.QuestionWire{carriedQuestion(1, "3 or 5 retries?", "5, the upstream is flaky")}

	out := flatten(m.brief())

	if !strings.Contains(out, "Carried over from the replaced Round") {
		t.Fatalf("expected the carried-over questions in a section of their own, got:\n%s", out)
	}
	if !strings.Contains(out, "3 or 5 retries?") || !strings.Contains(out, "5, the upstream is flaky") {
		t.Errorf("expected each with its Answer, got:\n%s", out)
	}
}

func TestACarriedOverQuestionCanBeWithdrawnFromTheOverview(t *testing.T) {
	m, rec := overviewAsking(t)
	m.view.Questions = []daemon.QuestionWire{carriedQuestion(7, "3 or 5?", "5")}

	picking := press(m, "a")
	armed := press(picking, "d")
	confirmed := press(armed, "y")

	if picking.mode != modeQuestions {
		t.Fatalf("expected a to offer the carried-over question, got mode %v", picking.mode)
	}
	if !armed.confirmingDelete {
		t.Fatal("expected d to ask before withdrawing")
	}
	if len(rec.paths) != 1 || rec.paths[0] != "DELETE /reviews/a1/question/7" {
		t.Errorf("expected question 7 withdrawn, got %v", rec.paths)
	}
	if confirmed.confirmingDelete {
		t.Error("expected the confirm to disarm once answered")
	}
}

func TestAQuestionAskedOnThisRoundCannotBeWithdrawn(t *testing.T) {
	m, rec := overviewAsking(t, question(3, "right approach?", ""), question(4, "log it?", ""))

	picking := press(m, "a")
	after := press(picking, "d")

	if after.confirmingDelete || len(rec.paths) != 0 {
		t.Error("a question asked on this Round is the agent's, not the Reviewer's to withdraw")
	}
	if !strings.Contains(after.status, "carried over") {
		t.Errorf("expected to be told only a carried-over question can be withdrawn, got %q", after.status)
	}
}

func TestAQuestionAskedAgainShowsWhatWasAskedAndAnsweredBefore(t *testing.T) {
	asked := question(9, "exactly 5, or 5 with jitter?", "")
	asked.History = []daemon.QuestionExchangeWire{
		{Text: "3 or 5?", Answer: "5 or so"},
		{Text: "exactly 5?", Answer: ""},
	}
	step := askingStep(asked)

	out := flatten(renderStep(step, newStepCursor(step, nil), map[string]bool{}, nil, 80, 40, false, false))

	for _, want := range []string{"asked before: 3 or 5?", "you answered: 5 or so", "asked before: exactly 5?", "unanswered"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the question's history, got:\n%s", want, out)
		}
	}
	if strings.Index(out, "3 or 5?") > strings.Index(out, "exactly 5, or 5 with jitter?") {
		t.Errorf("expected the history before the question as asked now, got:\n%s", out)
	}
}

func TestTheOverviewSaysWhereAQuestionWasAskedAgain(t *testing.T) {
	m := roundModel(80)
	again := accountedQuestion(1, "3 or 5 retries?", "5 or so", "asked_again", "cannot code '5 or so'")
	again.AskedAgainAs = &daemon.QuestionWire{ID: 2, Step: 3, Text: "exactly 5, or 5 with jitter?"}
	onRound := accountedQuestion(4, "right approach?", "", "asked_again", "it decides the rest")
	onRound.AskedAgainAs = &daemon.QuestionWire{ID: 5, Step: 0, Text: "threading, or a context?"}
	m.view.AccountedQuestions = []daemon.AccountedQuestionWire{again, onRound}

	out := flatten(m.brief())

	for _, want := range []string{"Asked again (2)", "asked again on Step 3", "asked again with the Brief", "cannot code '5 or so'"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q on the Overview, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "exactly 5, or 5 with jitter?") {
		t.Errorf("the Overview says where, not what: the new question is read where it sits, got:\n%s", out)
	}
}

func TestClearingACarriedOverAnswerSaysToWithdrawItInstead(t *testing.T) {
	m, rec := overviewAsking(t)
	m.view.Questions = []daemon.QuestionWire{carriedQuestion(7, "3 or 5?", "5")}
	picking := press(m, "a")
	answering, _ := picking.Update(tea.KeyMsg{Type: tea.KeyEnter})
	am := answering.(model)
	am.note.SetValue("")

	saved, _ := am.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if len(rec.answers) != 0 {
		t.Errorf("expected nothing sent, got %v", rec.answers)
	}
	if status := saved.(model).status; !strings.Contains(status, "withdraw") {
		t.Errorf("expected to be told to withdraw it instead, got %q", status)
	}
}

func TestTheAgentQuestionCountReadsByWhatWasAnswered(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answers []string
		want    string
	}{
		{"all answered", []string{"3", "yes"}, "2 agent questions answered"},
		{"none answered", []string{"", ""}, "2 agent questions left unanswered"},
		{"some answered", []string{"3", "yes", ""}, "2 agent questions answered and 1 unanswered"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := concludingModel()
			for i, answer := range tc.answers {
				m.view.Questions = append(m.view.Questions, question(i+1, "?", answer))
			}

			out := flatten(m.conclusionView())

			if !strings.Contains(out, tc.want) || strings.Contains(out, tc.want+" for your agent") {
				t.Errorf("expected %q, got:\n%s", tc.want, out)
			}
		})
	}
}

func TestTheHandedOffScreenListsAnswersAndUnansweredQuestionsInTheirOwnSentences(t *testing.T) {
	m := handedOffModel([]string{"seen"})
	m.view.AgentTold = true
	m.view.Questions = []daemon.QuestionWire{question(1, "3 or 5?", "3"), question(2, "keep the name?", "")}

	out := flatten(m.doneView())

	if !strings.Contains(out, "1 answer for your agent. 1 unanswered agent question. Your agent has been told.") {
		t.Errorf("expected the answers and the unanswered questions each in a sentence, got:\n%s", out)
	}
}

func TestTheHandedOffScreenPluralisesAnswersAndUnansweredQuestions(t *testing.T) {
	m := handedOffModel([]string{"seen"})
	m.view.Questions = []daemon.QuestionWire{
		question(1, "?", "3"), question(2, "?", "yes"), question(3, "?", ""), question(4, "?", ""),
	}

	out := flatten(m.doneView())

	if !strings.Contains(out, "2 answers for your agent. 2 unanswered agent questions.") {
		t.Errorf("expected plurals, got:\n%s", out)
	}
}

func TestTheConclusionScreenPutsCommentsAndQuestionsOnLinesOfTheirOwn(t *testing.T) {
	m := concludingModel(daemon.CommentWire{ID: 1, Step: 1})
	m.view.Questions = []daemon.QuestionWire{question(1, "3 or 5?", "3")}

	out := m.conclusionView()

	if !strings.Contains(out, "1 Comment for your agent\n") {
		t.Errorf("expected the Comments on a line of their own, got:\n%s", out)
	}
	if !strings.Contains(out, "\n1 agent question answered\n") && !strings.Contains(out, "1 agent question answered\n") {
		t.Errorf("expected the agent questions on the next line, got:\n%s", out)
	}
	if strings.Contains(out, "answered for your agent") {
		t.Errorf("the questions' line should not say agent twice, got:\n%s", out)
	}
}

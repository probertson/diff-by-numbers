package review_test

import (
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// askingOnStep1 is validRound with Agent Questions on its first Step.
func askingOnStep1(questions ...string) review.Round {
	round := validRound()
	for _, text := range questions {
		round.Steps[0].Questions = append(round.Steps[0].Questions, review.Question{Text: text})
	}
	return round
}

// postedAsking posts a Round asking the given questions on Step 1.
func postedAsking(t *testing.T, questions ...string) *review.Session {
	t.Helper()
	session := newSession()
	mustPost(t, session, askingOnStep1(questions...))
	return session
}

func mustAnswer(t *testing.T, session *review.Session, id int, answer string) {
	t.Helper()
	if err := session.AnswerQuestion(id, answer); err != nil {
		t.Fatalf("expected to answer question %d, got %v", id, err)
	}
}

func mustFinish(t *testing.T, session *review.Session) {
	t.Helper()
	if err := session.Finish(); err != nil {
		t.Fatalf("expected to hand off, got %v", err)
	}
}

func TestAStepCarriesTheAgentQuestionsPostedOnIt(t *testing.T) {
	session := postedAsking(t, "Should the retry cap be 3 or 5?")

	mustAdvance(t, session)
	step := session.View().Step

	if len(step.Questions) != 1 {
		t.Fatalf("expected the Step to carry 1 Agent Question, got %d", len(step.Questions))
	}
	question := step.Questions[0]
	if question.Text != "Should the retry cap be 3 or 5?" {
		t.Errorf("unexpected question %q", question.Text)
	}
	if question.Step != 1 || question.ID == 0 {
		t.Errorf("expected an id and Step 1, got %+v", question)
	}
	if question.Answered() {
		t.Error("a question nobody has answered should read as unanswered")
	}
}

func TestAStepShowsOnlyItsOwnQuestions(t *testing.T) {
	round := askingOnStep1("on the first Step")
	round.Steps = append(round.Steps, review.Step{
		Name: "Use the retrier", Explanation: "e", Excerpts: round.Steps[0].Excerpts,
	})

	session := newSession()
	mustPost(t, session, round)
	mustAdvance(t, session)
	mustAdvance(t, session)

	if questions := session.View().Step.Questions; len(questions) != 0 {
		t.Errorf("expected Step 2 to carry no questions, got %+v", questions)
	}
}

func TestAnEmptyAgentQuestionIsRefused(t *testing.T) {
	session := newSession()

	err := session.Post(askingOnStep1("  "))

	assertRejected(t, err, review.RejectedMalformedQuestion)
	assertDetailContains(t, err, "Step 1")
}

func TestTheReviewerCanAnswerEditAndClearAnAgentQuestion(t *testing.T) {
	session := postedAsking(t, "3 or 5?")
	id := session.Questions()[0].ID

	mustAnswer(t, session, id, "3")
	answered := session.Questions()[0]
	mustAnswer(t, session, id, "5, since the upstream is flaky")
	edited := session.Questions()[0]
	mustAnswer(t, session, id, "   ")
	cleared := session.Questions()[0]

	if answered.Answer != "3" || !answered.Answered() {
		t.Errorf("expected the Answer to be recorded, got %+v", answered)
	}
	if edited.Answer != "5, since the upstream is flaky" {
		t.Errorf("expected the Answer to be edited, got %+v", edited)
	}
	if cleared.Answered() {
		t.Errorf("an Answer of nothing but spaces clears it, got %+v", cleared)
	}
}

func TestAnsweringAQuestionThatIsNotThereIsRefused(t *testing.T) {
	session := postedAsking(t, "3 or 5?")

	err := session.AnswerQuestion(99, "3")

	assertRejected(t, err, review.RejectedNoSuchQuestion)
}

func TestHandOffLocksAnswersAndResumingUnlocksThem(t *testing.T) {
	session := postedAsking(t, "3 or 5?")
	id := session.Questions()[0].ID
	mustFinish(t, session)

	locked := session.AnswerQuestion(id, "3")
	if err := session.Reopen(); err != nil {
		t.Fatal(err)
	}
	unlocked := session.AnswerQuestion(id, "3")

	assertRejected(t, locked, review.RejectedRoundHandedOff)
	if unlocked != nil {
		t.Errorf("resuming should unlock the Answer, got %v", unlocked)
	}
}

func TestAHandOffOfARoundWithQuestionsIsNotAConclusion(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer string
	}{
		{"answered", "3"},
		{"unanswered", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := postedAsking(t, "3 or 5?")
			mustAnswer(t, session, session.Questions()[0].ID, tc.answer)

			mustFinish(t, session)

			if session.Concluded() {
				t.Error("a Round that asked the Reviewer something is never inferred concluded")
			}
			if outcome := session.Outcome(); outcome != review.HandedOffWithSomethingRaised {
				t.Errorf("expected the Hand Off to leave the agent something to read, got %q", outcome)
			}
		})
	}
}

func TestAHandOffWithNoQuestionsAndNoCommentsStillConcludes(t *testing.T) {
	session := newSession()
	mustPost(t, session, validRound())

	mustFinish(t, session)

	if !session.Concluded() {
		t.Error("a Hand Off that raised nothing and answered nothing concludes, as it always did")
	}
	if outcome := session.Outcome(); outcome != review.HandedOffNothingRaised {
		t.Errorf("expected nothing raised, got %q", outcome)
	}
}

func TestResultsCarryEveryQuestionWithItsAnswerOrAsUnanswered(t *testing.T) {
	session := postedAsking(t, "3 or 5?", "keep the old name?")
	mustAnswer(t, session, session.Questions()[0].ID, "3")
	mustFinish(t, session)

	results, err := session.Results()

	if err != nil {
		t.Fatal(err)
	}
	if len(results.Questions) != 2 {
		t.Fatalf("expected both questions, got %+v", results.Questions)
	}
	if results.Questions[0].Answer != "3" {
		t.Errorf("expected the first to carry its Answer, got %+v", results.Questions[0])
	}
	if results.Questions[1].Answered() {
		t.Errorf("expected the second to come back unanswered, got %+v", results.Questions[1])
	}
}

func TestARoundLevelQuestionIsAskedBeforeAnyStepAndBelongsToNone(t *testing.T) {
	round := askingOnStep1("on the Step")
	round.Questions = []review.Question{{Text: "is threading the id the right approach?"}}

	session := newSession()
	mustPost(t, session, round)
	questions := session.Questions()

	if len(questions) != 2 {
		t.Fatalf("expected the Round's and the Step's questions, got %+v", questions)
	}
	if questions[0].Text != "is threading the id the right approach?" || questions[0].Step != 0 {
		t.Errorf("expected the Round-level question first, on no Step, got %+v", questions[0])
	}
	if !questions[0].OnRound() || questions[1].OnRound() {
		t.Errorf("expected only the first to read as asked on the Round, got %+v", questions)
	}
	if view := session.View(); len(view.RoundQuestions) != 1 || view.RoundQuestions[0].ID != questions[0].ID {
		t.Errorf("expected the Overview to carry the Round-level question, got %+v", view.RoundQuestions)
	}
}

func TestAnEmptyRoundLevelQuestionIsRefused(t *testing.T) {
	round := validRound()
	round.Questions = []review.Question{{Text: ""}}

	err := newSession().Post(round)

	assertRejected(t, err, review.RejectedMalformedQuestion)
	assertDetailContains(t, err, "the Round")
}

func TestARoundWhoseOnlyQuestionsAreRoundLevelIsNotConcludedAtHandOff(t *testing.T) {
	round := validRound()
	round.Questions = []review.Question{{Text: "right approach?"}}
	session := newSession()
	mustPost(t, session, round)

	mustFinish(t, session)

	if session.Concluded() {
		t.Error("a Round-level question is still a question: the Hand Off is not a conclusion")
	}
}

func TestAReplacementCarriesAnsweredQuestionsOverAndDropsTheRest(t *testing.T) {
	session := postedAsking(t, "3 or 5?", "keep the name?")
	mustAnswer(t, session, 1, "5")

	if err := session.Replace(session.ReviewID(), askingOnStep1("a fresh question")); err != nil {
		t.Fatalf("expected the replacement to be accepted, got %v", err)
	}
	questions := session.Questions()

	if len(questions) != 2 {
		t.Fatalf("expected the answered question and the fresh one, got %+v", questions)
	}
	carried, fresh := questions[0], questions[1]
	if carried.Text != "3 or 5?" || carried.Answer != "5" || !carried.CarriedOver || carried.Step != 0 {
		t.Errorf("expected the answered question carried over, off its Step, with its Answer, got %+v", carried)
	}
	if carried.OnRound() {
		t.Error("a carried-over question was asked on a Step, not on the Round")
	}
	if fresh.Text != "a fresh question" || fresh.CarriedOver || fresh.ID == carried.ID {
		t.Errorf("expected the replacement's own question, with an id of its own, got %+v", fresh)
	}
	if round := session.View().RoundQuestions; len(round) != 0 {
		t.Errorf("a carried-over question is not one asked on the Round, got %+v", round)
	}
}

func TestTheReviewerCanWithdrawACarriedOverQuestion(t *testing.T) {
	session := postedAsking(t, "3 or 5?")
	mustAnswer(t, session, 1, "5")
	if err := session.Replace(session.ReviewID(), validRound()); err != nil {
		t.Fatal(err)
	}

	err := session.WithdrawQuestion(1)

	if err != nil {
		t.Fatalf("expected to withdraw the carried-over question, got %v", err)
	}
	if questions := session.Questions(); len(questions) != 0 {
		t.Errorf("expected it gone, got %+v", questions)
	}
}

func TestOnlyACarriedOverQuestionCanBeWithdrawn(t *testing.T) {
	session := postedAsking(t, "3 or 5?")

	err := session.WithdrawQuestion(1)

	assertRejected(t, err, review.RejectedNoSuchQuestion)
}

func TestCarriedOverAnswersReachTheAgentAtTheNextHandOff(t *testing.T) {
	session := postedAsking(t, "3 or 5?")
	mustAnswer(t, session, 1, "5")
	if err := session.Replace(session.ReviewID(), validRound()); err != nil {
		t.Fatal(err)
	}
	mustFinish(t, session)

	results, _ := session.Results()

	if len(results.Questions) != 1 || !results.Questions[0].CarriedOver || results.Questions[0].Answer != "5" {
		t.Errorf("expected the carried-over Answer to go back to the agent, got %+v", results.Questions)
	}
	if session.Concluded() {
		t.Error("a carried-over Answer is still an Answer for the agent to read")
	}
}

func TestACarriedOverAnswerCanBeChangedButNotCleared(t *testing.T) {
	session := postedAsking(t, "3 or 5?")
	mustAnswer(t, session, 1, "5")
	if err := session.Replace(session.ReviewID(), validRound()); err != nil {
		t.Fatal(err)
	}

	changed := session.AnswerQuestion(1, "3, on reflection")
	cleared := session.AnswerQuestion(1, "")

	if changed != nil {
		t.Errorf("expected a carried-over Answer to be changeable, got %v", changed)
	}
	assertRejected(t, cleared, review.RejectedQuestionCarriedOver)
	assertDetailContains(t, cleared, "withdraw")
	if answer := session.Questions()[0].Answer; answer != "3, on reflection" {
		t.Errorf("expected the carried-over question to keep its Answer, got %q", answer)
	}
}

func TestTheAgentMayConcludeOnlyWhenEveryQuestionWasAnsweredAndNothingElseRaised(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answers []string
		comment bool
		may     bool
	}{
		{"every question answered", []string{"3", "yes"}, false, true},
		{"a question unanswered", []string{"3", ""}, false, false},
		{"a Comment raised as well", []string{"3"}, true, false},
		{"nothing asked, which concludes on its own", nil, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var asked []string
			for range tc.answers {
				asked = append(asked, "?")
			}
			session := postedAsking(t, asked...)
			for i, answer := range tc.answers {
				mustAnswer(t, session, i+1, answer)
			}
			if tc.comment {
				mustAdvance(t, session)
				raise(t, session, 20, 20, "rename")
			}

			mustFinish(t, session)

			if got := session.MayConclude(); got != tc.may {
				t.Errorf("expected MayConclude %v, got %v", tc.may, got)
			}
		})
	}
}

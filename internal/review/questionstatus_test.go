package review_test

import (
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// handedOffAsking posts round 1 over app.ts:1-3 asking one question on its Step
// per answer given, answers each ("" leaves it unanswered), and hands off. The
// deriver is returned so round two can be scoped against it.
func handedOffAsking(t *testing.T, answers ...string) (*review.Session, *roundDeriver) {
	t.Helper()
	deriver := &roundDeriver{lines: changedApp(1, 3)}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver)
	step := appStep(1, 3)
	for range answers {
		step.Questions = append(step.Questions, review.Question{Text: "3 or 5?"})
	}
	mustPost(t, session, appRound([]review.Step{step}, nil))
	for i, answer := range answers {
		mustAnswer(t, session, i+1, answer)
	}
	mustFinish(t, session)
	return session, deriver
}

// accounting is a Revision Round re-showing app.ts:1-3, with statuses for the
// previous round's questions.
func accounting(statuses ...review.QuestionAccount) review.Round {
	round := appRound([]review.Step{appStep(1, 3)}, nil)
	round.Label = ""
	round.QuestionStatuses = statuses
	return round
}

func status(id int, status review.QuestionStatus, response string) review.QuestionAccount {
	return review.QuestionAccount{QuestionID: id, Status: status, Response: response}
}

func TestARevisionRoundMustGiveEveryEarlierQuestionAStatus(t *testing.T) {
	session, _ := handedOffAsking(t, "3", "")

	err := session.Revise(session.ReviewID(), accounting(status(1, review.QuestionNoChangeNeeded, "")))

	assertRejected(t, err, review.RejectedMalformedQuestionStatus)
	assertDetailContains(t, err, "Agent Question 2")
}

func TestAMissingQuestionStatusIsReportedAlongsideOtherProblems(t *testing.T) {
	session, deriver := handedOffAsking(t, "3")
	deriver.lines = changedApp(1, 4)
	deriver.touched = map[string]bool{"app.ts:4": true}

	err := session.Revise(session.ReviewID(), accounting()) // leaves line 4 uncovered, too

	assertRejected(t, err, review.RejectedMalformedQuestionStatus)
	assertRejected(t, err, review.RejectedUncoveredChanges)
}

func TestAQuestionStatusIsJudgedByWhetherTheQuestionWasAnswered(t *testing.T) {
	for _, tc := range []struct {
		name     string
		answer   string
		status   review.QuestionStatus
		response string
		accepted bool
	}{
		{"addressed an Answer", "5", review.QuestionAddressed, "", true},
		{"no change needed for an Answer", "3", review.QuestionNoChangeNeeded, "", true},
		{"agent's call on an unanswered question", "", review.QuestionAgentsCall, "went with 3", true},
		{"agent's call without saying what it chose", "", review.QuestionAgentsCall, "", false},
		{"agent's call over an Answer", "5", review.QuestionAgentsCall, "went with 3", false},
		{"addressed a question nobody answered", "", review.QuestionAddressed, "", false},
		{"no change needed for a question nobody answered", "", review.QuestionNoChangeNeeded, "", false},
		{"declined, which a question cannot be", "5", "declined", "no", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session, _ := handedOffAsking(t, tc.answer)

			err := session.Revise(session.ReviewID(), accounting(status(1, tc.status, tc.response)))

			if tc.accepted && err != nil {
				t.Errorf("expected the status to be accepted, got %v", err)
			}
			if !tc.accepted {
				assertRejected(t, err, review.RejectedMalformedQuestionStatus)
			}
		})
	}
}

func TestTheRevisionRoundShowsEachEarlierQuestionWithItsAnswerAndStatus(t *testing.T) {
	session, _ := handedOffAsking(t, "5", "")

	mustRevise(t, session, accounting(
		status(1, review.QuestionAddressed, "raised the cap to 5"),
		status(2, review.QuestionAgentsCall, "kept the old name"),
	))
	accounted := session.View().AccountedQuestions

	if len(accounted) != 2 {
		t.Fatalf("expected both earlier questions accounted for, got %+v", accounted)
	}
	first := accounted[0]
	if first.Question.Text != "3 or 5?" || first.Question.Answer != "5" ||
		first.Status != review.QuestionAddressed || first.Response != "raised the cap to 5" {
		t.Errorf("expected the question, its Answer and its status, got %+v", first)
	}
	if second := accounted[1]; second.Question.Answered() || second.Status != review.QuestionAgentsCall {
		t.Errorf("expected the unanswered question as the agent's call, got %+v", second)
	}
}

func TestAFirstRoundTakesNoQuestionStatuses(t *testing.T) {
	session := review.NewSession(&textResolver{text: map[string]string{}}, &roundDeriver{lines: changedApp(1, 3)})
	round := appRound([]review.Step{appStep(1, 3)}, nil)
	round.QuestionStatuses = []review.QuestionAccount{status(1, review.QuestionAddressed, "")}

	err := session.Post(round)

	assertRejected(t, err, review.RejectedMalformedQuestionStatus)
}

func TestARevisionRoundAfterNoQuestionsNeedsNoStatuses(t *testing.T) {
	session, _ := keptOpenRound1(t)

	err := session.Revise(session.ReviewID(), revising(appRound([]review.Step{appStep(1, 3)}, nil)))

	if err != nil {
		t.Errorf("expected a round that asked nothing to need no statuses, got %v", err)
	}
}

func TestReplacingARevisionRoundSuppliesItsQuestionStatusesAgain(t *testing.T) {
	session, _ := handedOffAsking(t, "5")
	mustRevise(t, session, accounting(status(1, review.QuestionAddressed, "")))

	without := session.Replace(session.ReviewID(), accounting())
	with := session.Replace(session.ReviewID(), accounting(status(1, review.QuestionNoChangeNeeded, "")))

	assertRejected(t, without, review.RejectedMalformedQuestionStatus)
	if with != nil {
		t.Errorf("expected the replacement re-supplying the statuses to be accepted, got %v", with)
	}
	if accounted := session.View().AccountedQuestions; len(accounted) != 1 || accounted[0].Status != review.QuestionNoChangeNeeded {
		t.Errorf("expected the replacement's status to stand, got %+v", accounted)
	}
}

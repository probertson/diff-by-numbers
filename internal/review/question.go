package review

import "strings"

// Question is an Agent Question as the Authoring Agent posts it: a decision or
// judgment it needs from the Reviewer, written so it can be answered from its
// Step and the Steps before it (ADR-0017).
type Question struct {
	Text string
	// AsksAgain is the previous round's Agent Question this one asks again, or
	// 0 for a question asked for the first time.
	AsksAgain int
}

// QuestionExchange is one earlier asking of a question asked again: how it was
// worded, and what the Reviewer answered, if anything.
type QuestionExchange struct {
	Text   string
	Answer string
}

// AgentQuestion is an Agent Question as the Session holds it, with the
// Reviewer's Answer. It is the reverse of a Comment: the agent asks, and the
// Reviewer answers.
type AgentQuestion struct {
	ID int
	// Step is the Step the question was asked on, or 0 for one asked on the
	// Round, about the approach.
	Step int
	Text string
	// Answer is what the Reviewer said, in free text; empty while unanswered.
	Answer string
	// CarriedOver marks an answered question from a Round since replaced in
	// place. Its Step is gone, so it belongs to none; it keeps its wording and
	// Answer, and goes back to the agent at the next Hand Off.
	CarriedOver bool
	// AsksAgain is the previous round's question this one asks again, and
	// History every earlier asking of it, oldest first, so the Reviewer does
	// not start over.
	AsksAgain int
	History   []QuestionExchange
}

// Answered reports whether the Reviewer has answered the question. One handed
// off unanswered reaches the agent marked so, so it never reads silence as
// agreement.
func (q AgentQuestion) Answered() bool { return q.Answer != "" }

// OnRound reports whether the question was asked on the Round, about the
// approach, rather than about one Step's code. A carried-over question has
// left its Step, but it was not asked on the Round.
func (q AgentQuestion) OnRound() bool { return q.Step == 0 && !q.CarriedOver }

// CountAnswers tallies Agent Questions the Reviewer answered and did not.
func CountAnswers(questions []AgentQuestion) (answered, unanswered int) {
	for _, question := range questions {
		if question.Answered() {
			answered++
		} else {
			unanswered++
		}
	}
	return answered, unanswered
}

// askedIn mints the Agent Questions a Round asks, numbered on from next, in
// the order the Reviewer meets them: the Round's own with the Brief, then each
// Step's. A question asked again takes the history of the one it re-poses,
// from prior, with that asking added.
func askedIn(round Round, next int, prior []AgentQuestion) []AgentQuestion {
	earlier := make(map[int]AgentQuestion, len(prior))
	for _, question := range prior {
		earlier[question.ID] = question
	}
	var out []AgentQuestion
	mint := func(step int, question Question) {
		next++
		minted := AgentQuestion{ID: next, Step: step, Text: question.Text, AsksAgain: question.AsksAgain}
		if before, ok := earlier[question.AsksAgain]; ok {
			minted.History = append(append([]QuestionExchange{}, before.History...),
				QuestionExchange{Text: before.Text, Answer: before.Answer})
		}
		out = append(out, minted)
	}
	for _, question := range round.Questions {
		mint(0, question)
	}
	for i, step := range round.Steps {
		for _, question := range step.Questions {
			mint(i+1, question)
		}
	}
	return out
}

// posted lists every Agent Question a Round asks, wherever it sits.
func posted(round Round) []Question {
	out := append([]Question{}, round.Questions...)
	for _, step := range round.Steps {
		out = append(out, step.Questions...)
	}
	return out
}

// validateQuestions refuses an Agent Question with nothing to answer.
func validateQuestions(questions []Question, where string) *Rejection {
	for i, question := range questions {
		if strings.TrimSpace(question.Text) == "" {
			return reject(RejectedMalformedQuestion,
				"Agent Question %d of %s is empty; ask what you need the Reviewer to decide", i+1, where)
		}
	}
	return nil
}

// Questions lists the Agent Questions of the Round on screen, in the order the
// Reviewer meets them.
func (s *Session) Questions() []AgentQuestion {
	out := make([]AgentQuestion, len(s.questions))
	copy(out, s.questions)
	return out
}

// questionsOnStep lists the Agent Questions asked on one Step, or on the Round
// for step 0.
func (s *Session) questionsOnStep(step int) []AgentQuestion {
	var out []AgentQuestion
	for _, question := range s.questions {
		if question.Step == step && !question.CarriedOver {
			out = append(out, question)
		}
	}
	return out
}

// carryQuestionsOver is what a Replacement keeps of the Round it replaces
// (ADR-0017): each answered question, moved off its Step and marked carried
// over, as Comments are. An unanswered one is dropped. The agent wrote it and
// the replacement is its fresh post, so it asks again what still matters, on
// the Steps that now give it context.
func carryQuestionsOver(questions []AgentQuestion) []AgentQuestion {
	var out []AgentQuestion
	for _, question := range questions {
		if !question.Answered() {
			continue
		}
		question.Step = 0
		question.CarriedOver = true
		out = append(out, question)
	}
	return out
}

// WithdrawQuestion removes a carried-over Agent Question the replacement made
// moot. Only those are the Reviewer's to withdraw: a question asked on this
// Round is the agent's, and the Reviewer answers it or leaves it.
func (s *Session) WithdrawQuestion(id int) error {
	if s.finished {
		return reject(RejectedRoundHandedOff,
			"this Round is handed off; resume it to withdraw a question")
	}
	for i, question := range s.questions {
		if question.ID != id {
			continue
		}
		if !question.CarriedOver {
			return reject(RejectedNoSuchQuestion,
				"Agent Question %d was asked on this Round; only a carried-over question can be withdrawn", id)
		}
		s.questions = append(s.questions[:i], s.questions[i+1:]...)
		return nil
	}
	return reject(RejectedNoSuchQuestion, "there is no Agent Question %d", id)
}

// AnswerQuestion records the Reviewer's Answer to an Agent Question, replacing
// any Answer it had. An Answer of nothing but whitespace clears it, which is how
// the Reviewer takes one back. Like a Comment, an Answer is locked by Hand Off
// and unlocked by resuming.
func (s *Session) AnswerQuestion(id int, answer string) error {
	if s.finished {
		return reject(RejectedRoundHandedOff,
			"this Round is handed off; resume it to change an Answer")
	}
	if strings.TrimSpace(answer) == "" {
		answer = ""
	}
	for i := range s.questions {
		if s.questions[i].ID == id {
			s.questions[i].Answer = answer
			return nil
		}
	}
	return reject(RejectedNoSuchQuestion, "there is no Agent Question %d", id)
}

package review

import "strings"

// Question is an Agent Question as the Authoring Agent posts it: a decision or
// judgment it needs from the Reviewer, written so it can be answered from its
// Step and the Steps before it (ADR-0017).
type Question struct {
	Text string
}

// AgentQuestion is an Agent Question as the Session holds it, with the
// Reviewer's Answer. It is the reverse of a Comment: the agent asks, and the
// Reviewer answers.
type AgentQuestion struct {
	ID int
	// Step is the Step the question was asked on.
	Step int
	Text string
	// Answer is what the Reviewer said, in free text; empty while unanswered.
	Answer string
}

// Answered reports whether the Reviewer has answered the question. One handed
// off unanswered reaches the agent marked so, so it never reads silence as
// agreement.
func (q AgentQuestion) Answered() bool { return q.Answer != "" }

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

// askedIn mints the Agent Questions a Round asks, numbered from next, in the
// order the Reviewer meets them.
func askedIn(steps []Step, next int) []AgentQuestion {
	var out []AgentQuestion
	for i, step := range steps {
		for _, question := range step.Questions {
			next++
			out = append(out, AgentQuestion{ID: next, Step: i + 1, Text: question.Text})
		}
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

// questionsOnStep lists the Agent Questions asked on one Step.
func (s *Session) questionsOnStep(step int) []AgentQuestion {
	var out []AgentQuestion
	for _, question := range s.questions {
		if question.Step == step {
			out = append(out, question)
		}
	}
	return out
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

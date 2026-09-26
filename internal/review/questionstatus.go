package review

// A Revision Round accounts for every Agent Question of the round before, as it
// does for every Comment, so a decision cannot slip through on the way back
// (ADR-0017). dbn holds each question and its Answer, so the agent gives only a
// status; there is no "declined", since the Reviewer was asked to decide and
// the agent cannot overrule the Answer.

// QuestionStatus is what the Authoring Agent did with an Agent Question.
type QuestionStatus string

const (
	// QuestionAddressed means the Answer led to a change.
	QuestionAddressed QuestionStatus = "addressed"
	// QuestionNoChangeNeeded means the Answer agreed with the code as it stood.
	QuestionNoChangeNeeded QuestionStatus = "no_change_needed"
	// QuestionAgentsCall means the question went unanswered and the agent went
	// ahead on its own judgment. It says what it chose.
	QuestionAgentsCall QuestionStatus = "agents_call"
)

// QuestionAccount is the Authoring Agent's account, posted with a Revision
// Round, of one Agent Question from the previous round.
type QuestionAccount struct {
	QuestionID int
	Status     QuestionStatus
	// Response is required for the agent's call, since it is what the agent
	// chose, and optional otherwise.
	Response string
}

// AccountedQuestion pairs a previous-round Agent Question, with its Answer,
// and what the agent did about it, ready to show before any code.
type AccountedQuestion struct {
	Question AgentQuestion
	Status   QuestionStatus
	Response string
}

var questionAccounting = accounting{
	reason: RejectedMalformedQuestionStatus,
	item:   "Agent Question",
	entry:  "status",
	origin: "ask",
}

// accountForQuestions pairs each posted status with the previous round's Agent
// Question it names, and refuses a Revision Round that does not give every one
// a status that fits it.
func accountForQuestions(statuses []QuestionAccount, prior []AgentQuestion) ([]AccountedQuestion, *Rejection) {
	byID := make(map[int]AgentQuestion, len(prior))
	ids := make([]int, 0, len(prior))
	for _, question := range prior {
		byID[question.ID] = question
		ids = append(ids, question.ID)
	}

	check := func(account QuestionAccount) *Rejection {
		return checkQuestionAccount(account, byID[account.QuestionID])
	}
	rejection := accountFor(questionAccounting, ids, statuses,
		func(a QuestionAccount) int { return a.QuestionID }, check)
	if rejection != nil {
		return nil, rejection
	}

	out := make([]AccountedQuestion, 0, len(statuses))
	for _, account := range statuses {
		out = append(out, AccountedQuestion{
			Question: byID[account.QuestionID], Status: account.Status, Response: account.Response,
		})
	}
	return out, nil
}

// checkQuestionAccount judges one status against the question it accounts for.
// Addressed and no change needed both follow an Answer, so neither fits a
// question nobody answered; the agent's call is exactly for one of those, and
// must say what the agent chose.
func checkQuestionAccount(account QuestionAccount, question AgentQuestion) *Rejection {
	id := account.QuestionID
	switch account.Status {
	case QuestionAddressed, QuestionNoChangeNeeded:
		if !question.Answered() {
			return reject(RejectedMalformedQuestionStatus,
				"Agent Question %d went unanswered, so no Answer can have led to a change or agreed with the code; give it %q and say what you chose",
				id, QuestionAgentsCall)
		}
	case QuestionAgentsCall:
		if question.Answered() {
			return reject(RejectedMalformedQuestionStatus,
				"Agent Question %d was answered, so it is not yours to call; follow the Answer", id)
		}
		if account.Response == "" {
			return reject(RejectedMalformedQuestionStatus,
				"making the call on Agent Question %d needs a response saying what you chose", id)
		}
	default:
		return reject(RejectedMalformedQuestionStatus,
			"Agent Question %d must be given the status %q, %q or %q; there is no declining an Answer",
			id, QuestionAddressed, QuestionNoChangeNeeded, QuestionAgentsCall)
	}
	return nil
}

// AccountedQuestions reports how the previous round's Agent Questions were
// accounted for, for display before any code.
func (s *Session) AccountedQuestions() []AccountedQuestion {
	out := make([]AccountedQuestion, len(s.accountedQuestions))
	copy(out, s.accountedQuestions)
	return out
}

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
	// QuestionAskedAgain means the agent re-poses the question in this Round: it
	// went unanswered, or its Answer cannot be acted on as written. It says why,
	// and a question of this Round names it in AsksAgain. It is how an agent
	// that cannot follow an Answer says so, since it may not overrule one.
	QuestionAskedAgain QuestionStatus = "asked_again"
)

// QuestionAccount is the Authoring Agent's account, posted with a Revision
// Round, of one Agent Question from the previous round.
type QuestionAccount struct {
	QuestionID int
	Status     QuestionStatus
	// Response is required for the agent's call, since it is what the agent
	// chose, and for asking again, since it is why; optional otherwise.
	Response string
}

// AccountedQuestion pairs a previous-round Agent Question, with its Answer,
// and what the agent did about it, ready to show before any code.
type AccountedQuestion struct {
	Question AgentQuestion
	Status   QuestionStatus
	Response string
	// AskedAgainAs is the question of this Round that asks it again, when the
	// status is asked again, so the Overview can say where it now sits.
	AskedAgainAs AgentQuestion
}

var questionAccounting = accounting{
	reason: RejectedMalformedQuestionStatus,
	item:   "Agent Question",
	entry:  "status",
	origin: "ask",
}

// askedAgainLinks is which of the previous round's questions this Round asks
// again: posted counts the questions it posts naming each, and carried the
// carried-over questions that already do.
type askedAgainLinks struct {
	posted  map[int]int
	carried map[int]bool
}

// links finds the questions a Round asks again, among those it posts and
// those a replacement carries over.
func links(round Round, carried []AgentQuestion) askedAgainLinks {
	out := askedAgainLinks{posted: map[int]int{}, carried: map[int]bool{}}
	for _, question := range posted(round) {
		if question.AsksAgain != 0 {
			out.posted[question.AsksAgain]++
		}
	}
	for _, question := range carried {
		if question.AsksAgain != 0 {
			out.carried[question.AsksAgain] = true
		}
	}
	return out
}

// accountForQuestions pairs each posted status with the previous round's Agent
// Question it names, and refuses a Revision Round that does not give every one
// a status that fits it.
func accountForQuestions(statuses []QuestionAccount, prior []AgentQuestion, linked askedAgainLinks) ([]AccountedQuestion, *Rejection) {
	byID := make(map[int]AgentQuestion, len(prior))
	ids := make([]int, 0, len(prior))
	for _, question := range prior {
		byID[question.ID] = question
		ids = append(ids, question.ID)
	}

	check := func(account QuestionAccount) *Rejection {
		if account.Status == QuestionAskedAgain {
			return checkAskedAgain(account, linked)
		}
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
			"Agent Question %d must be given the status %q, %q, %q or %q; there is no declining an Answer: if you cannot follow one, ask again and say why",
			id, QuestionAddressed, QuestionNoChangeNeeded, QuestionAgentsCall, QuestionAskedAgain)
	}
	return nil
}

// checkAskedAgain judges an asked-again status: it says why, and exactly one
// question of this Round asks it again. A carried-over question already asking
// it counts too; the replacement may ask it once more, since the agent cannot
// see mid-round which questions were answered, and the Reviewer resolves the
// pair (ADR-0017).
func checkAskedAgain(account QuestionAccount, linked askedAgainLinks) *Rejection {
	id := account.QuestionID
	switch {
	case account.Response == "":
		return reject(RejectedMalformedQuestionStatus,
			"asking Agent Question %d again needs a response saying why: what is still unclear, or why the Answer cannot be followed", id)
	case linked.posted[id] > 1:
		return reject(RejectedMalformedQuestionStatus,
			"%d questions ask Agent Question %d again; ask it again once", linked.posted[id], id)
	case linked.posted[id] == 0 && !linked.carried[id]:
		return reject(RejectedMalformedQuestionStatus,
			"Agent Question %d is asked again, but no question of this Round asks it: give the new question asks_again: %d", id, id)
	}
	return nil
}

// validateAskingAgain refuses a question that asks again one the previous round
// did not leave to be asked again: a first round has no earlier question, and
// an earlier question is asked again only when its status says so.
func validateAskingAgain(questions []Question, statuses []QuestionAccount, earlier *earlierRound) *Rejection {
	askedAgain := map[int]bool{}
	for _, account := range statuses {
		if account.Status == QuestionAskedAgain {
			askedAgain[account.QuestionID] = true
		}
	}
	asked := map[int]bool{}
	if earlier != nil {
		for _, question := range earlier.questions {
			asked[question.ID] = true
		}
	}
	for _, question := range questions {
		switch {
		case question.AsksAgain == 0:
		case earlier == nil:
			return reject(RejectedMalformedQuestion,
				"a question asks Agent Question %d again, but this is round 1; there is no earlier question to ask again", question.AsksAgain)
		case !asked[question.AsksAgain]:
			return reject(RejectedMalformedQuestion,
				"a question asks Agent Question %d again, which the previous round did not ask", question.AsksAgain)
		case !askedAgain[question.AsksAgain]:
			return reject(RejectedMalformedQuestion,
				"a question asks Agent Question %d again, but the previous round's question %d is not given the status %q",
				question.AsksAgain, question.AsksAgain, QuestionAskedAgain)
		}
	}
	return nil
}

// linkAskedAgain points each asked-again status at the question of this Round
// asking it, preferring one the Round posts to one carried over.
func linkAskedAgain(accounted []AccountedQuestion, questions []AgentQuestion) []AccountedQuestion {
	for i := range accounted {
		if accounted[i].Status != QuestionAskedAgain {
			continue
		}
		for _, question := range questions {
			if question.AsksAgain != accounted[i].Question.ID {
				continue
			}
			if accounted[i].AskedAgainAs.ID == 0 || !question.CarriedOver {
				accounted[i].AskedAgainAs = question
			}
		}
	}
	return accounted
}

// AccountedQuestions reports how the previous round's Agent Questions were
// accounted for, for display before any code.
func (s *Session) AccountedQuestions() []AccountedQuestion {
	out := make([]AccountedQuestion, len(s.accountedQuestions))
	copy(out, s.accountedQuestions)
	return out
}

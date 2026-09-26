package review

import "path/filepath"

// StepStatus is what a Step's disposition is at a glance, derived rather than
// declared: no verdict keypress, just what the Reviewer did.
type StepStatus string

const (
	// StepUnseen means the Reviewer has not visited the Step.
	StepUnseen StepStatus = "unseen"
	// StepSeen means the Reviewer visited it and raised nothing.
	StepSeen StepStatus = "seen"
	// StepFlagged means the Reviewer raised at least one Comment on it.
	StepFlagged StepStatus = "flagged"
)

// StepReport is a Step's name and final disposition, for the Authoring Agent's
// record.
type StepReport struct {
	Number int
	Name   string
	Status StepStatus
}

func (s *Session) stepStatus(step int) StepStatus {
	if s.commentsForStep(step) > 0 {
		return StepFlagged
	}
	if s.seen[step] {
		return StepSeen
	}
	return StepUnseen
}

func (s *Session) stepStatuses() []StepStatus {
	statuses := make([]StepStatus, len(s.current.Steps))
	for i := range statuses {
		statuses[i] = s.stepStatus(i + 1)
	}
	return statuses
}

// Finish hands off the Round. It refuses while any Changed Line is
// unaccounted for — an invariant the post-time coverage check already
// guarantees, re-asserted here so the finish boundary stays honest if that ever
// weakens.
func (s *Session) Finish() error {
	if s.current == nil {
		return reject(RejectedNoRound, "there is no Round to hand off")
	}
	if rejection := s.ledger.validateCoverage(s.current.Steps); rejection != nil {
		return rejection
	}
	s.finished = true
	return nil
}

// Reopen undoes a Finish so the Reviewer can add or change more before handing
// off. Finishing is a soft signal in the MVP, not a one-way door. It also clears
// any conclusion — inferred or explicit — so a Reviewer who finished and then
// thought better of it is not locked out, and the daemon knows the review is
// live again.
func (s *Session) Reopen() error {
	if s.current == nil {
		return reject(RejectedNoRound, "there is no Round to resume")
	}
	s.finished = false
	s.concluded = false
	return nil
}

// Conclude ends the review named by id, so the Authoring Agent can release a
// review it is done with and let the daemon stop holding it. Concluding is
// idempotent; concluding an id that is not the review under review is refused, so
// a stale reference cannot end the wrong review.
func (s *Session) Conclude(id string) error {
	if s.current == nil {
		return reject(RejectedNoRound, "there is no review to conclude")
	}
	if id != s.id {
		return reject(RejectedUnknownReview,
			"no review with id %q; the review under review is %q", id, s.id)
	}
	s.concluded = true
	return nil
}

// isConcluded reports whether the review has reached a terminal disposition:
// declared so explicitly, or inferred from a round finished with nothing
// raised — the natural end of the review loop.
func (s *Session) isConcluded() bool {
	return s.concluded || (s.finished && s.raisedNothing())
}

// raisedNothing reports whether the Round on screen leaves the Authoring Agent
// nothing to read: no Comment raised, and no Agent Question asked. A question
// counts whether or not it was answered: an Answer may call for a change, which
// must be reviewed like any other, and one left unanswered is still the agent's
// to act on (ADR-0017). It is the one place that is decided, so the inferred
// conclusion, the Round's outcome and what the agent is told all agree on it.
func (s *Session) raisedNothing() bool {
	return len(s.comments) == 0 && len(s.questions) == 0
}

// Open describes the review this Session holds, for a surface listing what the
// daemon is holding. It reports false once the review is over, since a review
// that has ended is not one the Authoring Agent can still post to.
func (s *Session) Open() (OpenReview, bool) {
	if s.dismissed || !s.Active() {
		return OpenReview{}, false
	}
	return OpenReview{
		ID:           s.id,
		Label:        s.label,
		HandedOff:    s.finished,
		Opened:       s.opened,
		Repositories: s.openRepositories(),
		Round:        s.roundNumber,
		Position:     s.position,
		StepCount:    len(s.current.Steps),
		CommentCount: len(s.comments),
		Posted:       s.posted,
	}, true
}

// MayConclude reports whether a Hand Off leaves the Authoring Agent free to end
// the review itself: it asked Agent Questions, every one was answered, and no
// Comment was raised. Only the agent can tell whether an Answer calls for a
// change, so dbn never infers the conclusion; this is the one place that says
// when the agent may draw it (ADR-0017).
func (s *Session) MayConclude() bool {
	if s.current == nil || !s.finished || len(s.comments) > 0 || len(s.questions) == 0 {
		return false
	}
	_, unanswered := CountAnswers(s.questions)
	return unanswered == 0
}

// RoundOutcome is what has become of the Round on screen that its Authoring
// Agent can act on: the Reviewer handed it off, or dismissed the review.
// Nothing else the Reviewer does — opening it, moving through it, raising a
// Comment — is the agent's business until then (ADR-0016).
type RoundOutcome string

const (
	// NoOutcomeYet is a Round still with the Reviewer.
	NoOutcomeYet RoundOutcome = ""
	// HandedOffWithSomethingRaised is a Hand Off with something for the agent to
	// read: Comments to work, or Answers to its Agent Questions.
	HandedOffWithSomethingRaised RoundOutcome = "handed_off"
	// HandedOffNothingRaised is a Hand Off with nothing raised, which concludes
	// the review.
	HandedOffNothingRaised RoundOutcome = "concluded"
	// Dismissed is the Reviewer discarding the review, and with it the Round.
	Dismissed RoundOutcome = "dismissed"
)

// Outcome reports what has become of the Round on screen. Every post starts
// its Round afresh, so a Hand Off of an earlier Round the agent has since
// answered with a Revision Round is not reported again.
func (s *Session) Outcome() RoundOutcome {
	switch {
	case s.current == nil:
		return NoOutcomeYet
	case s.dismissed:
		return Dismissed
	case !s.finished:
		return NoOutcomeYet
	case s.raisedNothing():
		return HandedOffNothingRaised
	}
	return HandedOffWithSomethingRaised
}

// Concluded reports whether the posted review is over. It is false when nothing
// is posted: there is no review to have concluded.
func (s *Session) Concluded() bool {
	return s.current != nil && s.isConcluded()
}

// Active reports whether a posted review still needs the daemon — posted and not
// yet concluded. It is what the daemon consults to decide it may exit.
func (s *Session) Active() bool {
	return s.current != nil && !s.dismissed && !s.isConcluded()
}

// openRepositories names each repository under review the way a row does: by
// its own name, with the branch its work is on.
func (s *Session) openRepositories() []OpenRepository {
	repositories := make([]OpenRepository, 0, len(s.current.ChangeSet.Repositories))
	for _, repository := range s.current.ChangeSet.Repositories {
		repositories = append(repositories, OpenRepository{
			Name:   filepath.Base(repository.Root),
			Branch: s.ledger.branches[repository.Root],
		})
	}
	return repositories
}

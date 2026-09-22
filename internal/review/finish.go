package review

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
// declared so explicitly, or inferred from a round finished with no Comment
// raised — the natural end of the review loop.
func (s *Session) isConcluded() bool {
	return s.concluded || (s.finished && len(s.comments) == 0)
}

// Open describes the review this Session holds, for a surface listing what the
// daemon is holding. It reports false once the review is over, since a review
// that has ended is not one the Authoring Agent can still post to.
func (s *Session) Open() (OpenReview, bool) {
	if !s.Active() {
		return OpenReview{}, false
	}
	return OpenReview{ID: s.id, Label: s.label, HandedOff: s.finished}, true
}

// Concluded reports whether the posted review is over. It is false when nothing
// is posted: there is no review to have concluded.
func (s *Session) Concluded() bool {
	return s.current != nil && s.isConcluded()
}

// Active reports whether a posted review still needs the daemon — posted and not
// yet concluded. It is what the daemon consults to decide it may exit.
func (s *Session) Active() bool {
	return s.current != nil && !s.isConcluded()
}

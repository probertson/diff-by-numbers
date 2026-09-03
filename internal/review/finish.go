package review

// StepStatus is what a Step's disposition is at a glance, derived rather than
// declared: no verdict keypress, just what the Reviewer did.
type StepStatus string

const (
	// StepUnseen means the Reviewer has not visited the Step.
	StepUnseen StepStatus = "unseen"
	// StepSeen means the Reviewer visited it and raised nothing.
	StepSeen StepStatus = "seen"
	// StepFlagged means the Reviewer raised at least one Change Request on it.
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
	if s.changeRequestsForStep(step) > 0 {
		return StepFlagged
	}
	if s.seen[step] {
		return StepSeen
	}
	return StepUnseen
}

func (s *Session) stepStatuses() []StepStatus {
	statuses := make([]StepStatus, len(s.walkthrough.Steps))
	for i := range statuses {
		statuses[i] = s.stepStatus(i + 1)
	}
	return statuses
}

// Finish completes the Walkthrough. It refuses while any Changed Line is
// unaccounted for — an invariant the post-time coverage check already
// guarantees, re-asserted here so the finish boundary stays honest if that ever
// weakens.
func (s *Session) Finish() error {
	if s.walkthrough == nil {
		return reject(RejectedNoWalkthrough, "there is no Walkthrough to finish")
	}
	if rejection := s.ledger.validateCoverage(s.walkthrough.Steps); rejection != nil {
		return rejection
	}
	s.finished = true
	return nil
}

// Reopen undoes a Finish so the Reviewer can add or change more before handing
// off. Finishing is a soft signal in the MVP, not a one-way door.
func (s *Session) Reopen() error {
	if s.walkthrough == nil {
		return reject(RejectedNoWalkthrough, "there is no Walkthrough to reopen")
	}
	s.finished = false
	return nil
}

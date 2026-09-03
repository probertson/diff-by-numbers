package review

// ChangeRequest is a Reviewer's request for an edit, anchored to the code it
// concerns. It is a proposal, not an instruction: the Authoring Agent works it
// after the Walkthrough ends and may address or decline it. Nothing on disk moves
// while the Walkthrough is under review (ADR-0004).
type ChangeRequest struct {
	ID     int
	Step   int
	Anchor Anchor
	Note   string
}

// RaiseChangeRequest anchors a note to a selection in the Step in view and
// records it. The Step it lands on becomes flagged.
func (s *Session) RaiseChangeRequest(target AnchorTarget, note string) (ChangeRequest, error) {
	if s.finished {
		return ChangeRequest{}, reject(RejectedWalkthroughFinished,
			"this Walkthrough is finished; reopen a new one to add more")
	}
	anchor, err := s.Anchor(target)
	if err != nil {
		return ChangeRequest{}, err
	}
	s.nextCRID++
	cr := ChangeRequest{ID: s.nextCRID, Step: s.position, Anchor: anchor, Note: note}
	s.changeRequests = append(s.changeRequests, cr)
	return cr, nil
}

// ChangeRequests lists what the Reviewer has raised so far, in the order raised.
func (s *Session) ChangeRequests() []ChangeRequest {
	out := make([]ChangeRequest, len(s.changeRequests))
	copy(out, s.changeRequests)
	return out
}

// WithdrawChangeRequest removes a Change Request the Reviewer has reconsidered.
func (s *Session) WithdrawChangeRequest(id int) error {
	for i, cr := range s.changeRequests {
		if cr.ID == id {
			s.changeRequests = append(s.changeRequests[:i], s.changeRequests[i+1:]...)
			return nil
		}
	}
	return reject(RejectedNoSuchChangeRequest, "there is no Change Request %d", id)
}

func (s *Session) changeRequestsForStep(step int) int {
	count := 0
	for _, cr := range s.changeRequests {
		if cr.Step == step {
			count++
		}
	}
	return count
}

// EditChangeRequest replaces the note of an existing Change Request, so the
// Reviewer can refine a comment without withdrawing and re-anchoring it.
func (s *Session) EditChangeRequest(id int, note string) error {
	for i := range s.changeRequests {
		if s.changeRequests[i].ID == id {
			s.changeRequests[i].Note = note
			return nil
		}
	}
	return reject(RejectedNoSuchChangeRequest, "there is no Change Request %d", id)
}

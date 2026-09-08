package review

// A Revision Round is a second Walkthrough posted after a Finish. It re-derives
// the full Change Set, pre-marks as shown every Changed Line whose content is
// unchanged since the previous round (ADR-0007), and accounts for every Change
// Request the previous round raised. All of this lives in memory: restarting the
// daemon mid-review starts the review over.

// DispositionStatus is what the Authoring Agent did with a Change Request.
type DispositionStatus string

const (
	// DispositionAddressed means the agent made the change.
	DispositionAddressed DispositionStatus = "addressed"
	// DispositionDeclined means the agent did not, and says why.
	DispositionDeclined DispositionStatus = "declined"
)

// Disposition is the Authoring Agent's account, posted with a Revision Round, of
// what it did with one Change Request from the previous round.
type Disposition struct {
	ChangeRequestID int
	Status          DispositionStatus
	// Reasoning is required when Declined: a decline the Reviewer cannot weigh is
	// just a refusal.
	Reasoning string
}

// ResolvedDisposition pairs a previous-round Change Request with what the agent
// did about it, ready to show before any code in a Revision Round.
type ResolvedDisposition struct {
	ChangeRequest ChangeRequest
	Status        DispositionStatus
	Reasoning     string
}

// contentKey identifies a Changed Line by its content rather than its position,
// so a line that keeps its content across a Revision Round is recognised even
// when edits above it have shifted its number.
type contentKey struct {
	repository string
	file       string
	side       Side
	text       string
}

// resolveChangedTexts reads the current text of every new-side Changed Line,
// resolving each file's span once. Old-side lines are removals with no
// working-tree text, so they are not included — and therefore never pre-marked.
func (s *Session) resolveChangedTexts(l ledger) map[ChangedLine]string {
	byFile := map[fileRef][]ChangedLine{}
	for _, line := range l.lines {
		if line.Side != NewSide {
			continue
		}
		ref := fileRef{line.Repository, line.File}
		byFile[ref] = append(byFile[ref], line)
	}

	out := map[ChangedLine]string{}
	for ref, lines := range byFile {
		first, last := lines[0].Line, lines[0].Line
		for _, line := range lines {
			if line.Line < first {
				first = line.Line
			}
			if line.Line > last {
				last = line.Line
			}
		}
		resolved, err := s.resolver.Resolve(Excerpt{Repository: ref.repository, File: ref.file, Side: NewSide, FirstLine: first, LastLine: last})
		if err != nil {
			continue
		}
		text := make(map[int]string, len(resolved))
		for _, line := range resolved {
			text[line.Number] = line.Text
		}
		for _, line := range lines {
			if t, ok := text[line.Line]; ok {
				out[line] = t
			}
		}
	}
	return out
}

// captureContent records the content of the round's Changed Lines, so the next
// Revision Round can tell what has since moved.
func (s *Session) captureContent(l ledger) map[contentKey]bool {
	out := map[contentKey]bool{}
	for line, text := range s.resolveChangedTexts(l) {
		out[contentKey{line.Repository, line.File, line.Side, text}] = true
	}
	return out
}

// preMarkUnchanged returns the Changed Lines of a Revision Round whose content
// matches a Changed Line of the previous round: already reviewed, so pre-marked
// as shown. The Coverage Ledger is then left pointing at exactly what moved.
func (s *Session) preMarkUnchanged(l ledger) map[ChangedLine]bool {
	if len(s.priorContent) == 0 {
		return nil
	}
	preShown := map[ChangedLine]bool{}
	for line, text := range s.resolveChangedTexts(l) {
		if s.priorContent[contentKey{line.Repository, line.File, line.Side, text}] {
			preShown[line] = true
		}
	}
	return preShown
}

// resolveDispositions pairs each posted Disposition with the previous round's
// Change Request it names, and refuses a Revision Round that does not account for
// every one — addressed, or declined with a reason.
func (s *Session) resolveDispositions(dispositions []Disposition) ([]ResolvedDisposition, *Rejection) {
	prior := s.changeRequests
	byID := make(map[int]ChangeRequest, len(prior))
	for _, cr := range prior {
		byID[cr.ID] = cr
	}

	seen := map[int]bool{}
	out := make([]ResolvedDisposition, 0, len(dispositions))
	for _, disposition := range dispositions {
		cr, ok := byID[disposition.ChangeRequestID]
		if !ok {
			return nil, reject(RejectedMalformedDisposition,
				"a disposition names Change Request %d, which the previous round did not raise", disposition.ChangeRequestID)
		}
		if seen[disposition.ChangeRequestID] {
			return nil, reject(RejectedMalformedDisposition,
				"Change Request %d has more than one disposition", disposition.ChangeRequestID)
		}
		seen[disposition.ChangeRequestID] = true

		switch disposition.Status {
		case DispositionAddressed:
		case DispositionDeclined:
			if disposition.Reasoning == "" {
				return nil, reject(RejectedMalformedDisposition,
					"declining Change Request %d needs a reason the Reviewer can weigh", disposition.ChangeRequestID)
			}
		default:
			return nil, reject(RejectedMalformedDisposition,
				"Change Request %d must be disposed as %q or %q", disposition.ChangeRequestID, DispositionAddressed, DispositionDeclined)
		}
		out = append(out, ResolvedDisposition{ChangeRequest: cr, Status: disposition.Status, Reasoning: disposition.Reasoning})
	}

	for _, cr := range prior {
		if !seen[cr.ID] {
			return nil, reject(RejectedMalformedDisposition,
				"Change Request %d from the previous round has no disposition; a Revision Round must account for every one", cr.ID)
		}
	}
	return out, nil
}

// Dispositions reports how the previous round's Change Requests were resolved,
// for display before any code.
func (s *Session) Dispositions() []ResolvedDisposition {
	out := make([]ResolvedDisposition, len(s.dispositions))
	copy(out, s.dispositions)
	return out
}

// ReRaise raises again a Change Request the agent declined, carrying its original
// note and Anchor into the current round, so the Reviewer can insist without
// re-composing it. Only a declined Change Request can be re-raised.
func (s *Session) ReRaise(changeRequestID int) (ChangeRequest, error) {
	if s.finished {
		return ChangeRequest{}, reject(RejectedWalkthroughFinished,
			"this Walkthrough is finished; reopen it before re-raising")
	}
	for _, disposition := range s.dispositions {
		if disposition.ChangeRequest.ID != changeRequestID {
			continue
		}
		if disposition.Status != DispositionDeclined {
			return ChangeRequest{}, reject(RejectedNoSuchChangeRequest,
				"Change Request %d was addressed, not declined; only a declined one can be re-raised", changeRequestID)
		}
		s.nextCRID++
		cr := ChangeRequest{
			ID:     s.nextCRID,
			Step:   disposition.ChangeRequest.Step,
			Anchor: disposition.ChangeRequest.Anchor,
			Note:   disposition.ChangeRequest.Note,
		}
		s.changeRequests = append(s.changeRequests, cr)
		return cr, nil
	}
	return ChangeRequest{}, reject(RejectedNoSuchChangeRequest,
		"there is no declined Change Request %d to re-raise", changeRequestID)
}

package review

// Results is what the Authoring Agent receives when it asks how a review went.
type Results struct {
	// Posted reports whether a Walkthrough exists at all. Without it, an agent
	// whose post was rejected would be told the Reviewer "has not finished yet"
	// and wait on a Walkthrough that was never created.
	Posted bool
	// Finished reports whether the Reviewer has completed the Walkthrough. The
	// agent does not wait for this; it asks and is told.
	Finished       bool
	ChangeRequests []ChangeRequest
	// Brief and StepReports let the agent re-ground itself when it comes to work
	// the Change Requests, since its own context may have moved on or been
	// compacted since it posted (ADR-0008).
	Brief       Brief
	StepReports []StepReport
}

// Session holds the one Walkthrough currently under review. Several concurrent
// Walkthroughs are deliberately out of scope until the multiplexed inbox exists.
type Session struct {
	walkthrough *Walkthrough
	ledger      ledger
	resolver    Resolver
	deriver     Deriver
	// position is where the Reviewer is: 0 for the Brief, 1..len(Steps) for a
	// Step. It lives here rather than in any UI so that a reattaching surface
	// finds the review where it was left.
	position int
	// seen records which Steps the Reviewer has visited. A Step becomes seen the
	// moment it is in view, whether reached by advancing, going back, or jumping.
	seen           map[int]bool
	changeRequests []ChangeRequest
	nextCRID       int
	finished       bool
	// hashes fingerprints each new-side Excerpt file as it was when the
	// Walkthrough was accepted, so a Step whose file later changes can refuse to
	// show code beneath an explanation that has stopped describing it.
	hashes map[fileRef]string
	// priorContent holds the content of the previous round's Changed Lines, so a
	// Revision Round can tell what has since moved (ADR-0007).
	priorContent map[contentKey]bool
	// preShown marks the current round's Changed Lines that were unchanged since
	// the previous round: already reviewed, and counted as seen from the start.
	preShown map[ChangedLine]bool
	// dispositions accounts for the previous round's Change Requests in a Revision
	// Round, for display before any code.
	dispositions []ResolvedDisposition
}

// NewSession returns a Session with no Walkthrough posted. The resolver turns
// Excerpts into lines when a view is drawn; the deriver reports what git says
// actually changed. The core itself neither reads files nor runs git.
func NewSession(resolver Resolver, deriver Deriver) *Session {
	return &Session{resolver: resolver, deriver: deriver}
}

// Post submits a Walkthrough for review. Posting after the previous Walkthrough
// finished is a Revision Round: it re-derives the full Change Set, pre-marks what
// is unchanged, and must account for the previous round's Change Requests.
func (s *Session) Post(w Walkthrough) error {
	revision := s.walkthrough != nil && s.finished
	if s.walkthrough != nil && !s.finished {
		return reject(RejectedWalkthroughActive,
			"a Walkthrough is already under review; finish or abandon it first")
	}
	if rejection := validate(w); rejection != nil {
		return rejection
	}

	// Dispositions account for the previous round's Change Requests. Resolve them
	// before any state is reset, while the previous round's Change Requests still
	// stand.
	var dispositions []ResolvedDisposition
	if revision {
		resolved, rejection := s.resolveDispositions(w.Dispositions)
		if rejection != nil {
			return rejection
		}
		dispositions = resolved
	} else if len(w.Dispositions) > 0 {
		return reject(RejectedMalformedDisposition,
			"this is the first Walkthrough; there are no Change Requests to dispose of")
	}

	// Derive what git says changed, then hold the plan to it. Order matters:
	// structural faults are named before coverage, so an agent fixes the obvious
	// thing first.
	ledger, err := buildLedger(w.ChangeSet, s.deriver)
	if err != nil {
		return reject(RejectedDerivationFailed,
			"could not derive the changes under review: %v", err)
	}
	if rejection := validateNewSideResolves(w.Steps, s.resolver); rejection != nil {
		return rejection
	}

	// In a Revision Round, a Changed Line whose content is unchanged since the
	// previous round is pre-marked as shown, so coverage is enforced over what
	// actually moved.
	var preShown map[ChangedLine]bool
	if revision {
		preShown = s.preMarkUnchanged(ledger)
	}

	if rejection := ledger.validateBudget(w.Steps); rejection != nil {
		return rejection
	}
	if rejection := ledger.validateAcknowledgements(w.Steps); rejection != nil {
		return rejection
	}
	if rejection := ledger.validateCoverage(w.Steps, preShown); rejection != nil {
		return rejection
	}

	s.walkthrough = &w
	s.ledger = ledger
	s.position = 0
	s.seen = map[int]bool{}
	s.changeRequests = nil
	s.nextCRID = 0
	s.finished = false
	s.hashes = s.hashExcerptFiles(w.Steps)
	s.preShown = preShown
	s.dispositions = dispositions
	s.priorContent = s.captureContent(ledger)
	return nil
}

// moveTo places the Reviewer at a position and records a Step as seen. It is the
// single path every navigation goes through, so seen-tracking cannot be skipped.
func (s *Session) moveTo(position int) {
	s.position = position
	if position > 0 {
		if s.seen == nil {
			s.seen = map[int]bool{}
		}
		s.seen[position] = true
	}
}

// validateNewSideResolves rejects a new-side Excerpt whose range the working
// tree cannot satisfy. Old-side Excerpts are not checked here: reading the old
// side needs the derived revision and is deferred, so an old-side range that
// cannot be shown yet is a display limitation, not a malformed plan.
func validateNewSideResolves(steps []Step, resolver Resolver) *Rejection {
	for i, step := range steps {
		for j, excerpt := range step.Excerpts {
			if excerpt.Side != NewSide {
				continue
			}
			if _, err := resolver.Resolve(excerpt); err != nil {
				return reject(RejectedUnresolvableExcerpt,
					"Excerpt %d of Step %d does not resolve: %v", j+1, i+1, err)
			}
		}
	}
	return nil
}

// Abandon discards the Walkthrough under review without submitting anything.
// It is the Reviewer's act, not the Authoring Agent's, so it is not exposed as
// an MCP tool: an agent cannot dismiss a review of its own work.
func (s *Session) Abandon() error {
	if s.walkthrough == nil {
		return reject(RejectedNoWalkthrough, "there is no Walkthrough to abandon")
	}
	s.walkthrough = nil
	return nil
}

// Results reports how the review is going. It answers immediately, whether or
// not the Reviewer has finished.
func (s *Session) Results() (Results, error) {
	if s.walkthrough == nil {
		return Results{Posted: false}, nil
	}
	reports := make([]StepReport, len(s.walkthrough.Steps))
	for i, step := range s.walkthrough.Steps {
		reports[i] = StepReport{Number: i + 1, Name: step.Name, Status: s.stepStatus(i + 1)}
	}
	return Results{
		Posted:         true,
		Finished:       s.finished,
		ChangeRequests: s.ChangeRequests(),
		Brief:          s.walkthrough.Brief,
		StepReports:    reports,
	}, nil
}

// Advance moves the Reviewer one position forward: from the Brief to Step 1, or
// from a Step to the next. At the last Step it stays put — running out of road
// is not an error.
func (s *Session) Advance() error {
	if s.walkthrough == nil {
		return reject(RejectedNoWalkthrough, "there is no Walkthrough to advance through")
	}
	if s.position < len(s.walkthrough.Steps) {
		s.moveTo(s.position + 1)
	}
	return nil
}

// Back moves the Reviewer one position toward the Brief, so an earlier change
// can be revisited once a later one has given it meaning. At the Brief it stays.
func (s *Session) Back() error {
	if s.walkthrough == nil {
		return reject(RejectedNoWalkthrough, "there is no Walkthrough to move back through")
	}
	if s.position > 0 {
		s.moveTo(s.position - 1)
	}
	return nil
}

// GoTo jumps straight to a position: 0 for the Brief, 1..len(Steps) for a Step.
// Navigation is entirely local — the Authoring Agent is never consulted.
func (s *Session) GoTo(position int) error {
	if s.walkthrough == nil {
		return reject(RejectedNoWalkthrough, "there is no Walkthrough to navigate")
	}
	if position < 0 || position > len(s.walkthrough.Steps) {
		return reject(RejectedNoSuchStep,
			"there is no Step %d; this Walkthrough has %d", position, len(s.walkthrough.Steps))
	}
	s.moveTo(position)
	return nil
}

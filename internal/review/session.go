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
}

// ChangeRequest is a Reviewer's request for an edit. A proposal rather than an
// instruction: it resolves to addressed or declined.
type ChangeRequest struct {
	Note string
}

// Session holds the one Walkthrough currently under review. Several concurrent
// Walkthroughs are deliberately out of scope until the multiplexed inbox exists.
type Session struct {
	walkthrough *Walkthrough
	resolver    Resolver
	// position is where the Reviewer is: 0 for the Brief, 1..len(Steps) for a
	// Step. It lives here rather than in any UI so that a reattaching surface
	// finds the review where it was left.
	position int
}

// NewSession returns a Session with no Walkthrough posted. The resolver is what
// turns Excerpts into lines when a view is drawn; the core itself reads nothing.
func NewSession(resolver Resolver) *Session {
	return &Session{resolver: resolver}
}

// Post submits a Walkthrough for review.
func (s *Session) Post(w Walkthrough) error {
	if s.walkthrough != nil {
		return reject(RejectedWalkthroughActive,
			"a Walkthrough is already under review; finish or abandon it first")
	}
	if rejection := validate(w); rejection != nil {
		return rejection
	}
	s.walkthrough = &w
	s.position = 0
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
	return Results{Posted: true, Finished: false}, nil
}

// Advance moves the Reviewer one position forward: from the Brief to Step 1, or
// from a Step to the next. At the last Step it stays put — running out of road
// is not an error.
func (s *Session) Advance() error {
	if s.walkthrough == nil {
		return reject(RejectedNoWalkthrough, "there is no Walkthrough to advance through")
	}
	if s.position < len(s.walkthrough.Steps) {
		s.position++
	}
	return nil
}

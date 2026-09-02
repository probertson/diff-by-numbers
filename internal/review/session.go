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
}

// NewSession returns a Session with no Walkthrough posted.
func NewSession() *Session {
	return &Session{}
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

package review

import "fmt"

// RejectionReason names why a Walkthrough was refused. The Authoring Agent acts
// on this, so it is a value rather than prose: a rejection must tell the agent
// what to fix, not merely that something was wrong.
type RejectionReason string

const (
	// RejectedWalkthroughActive means a Walkthrough is already under review.
	RejectedWalkthroughActive RejectionReason = "walkthrough_active"
	// RejectedNoWalkthrough means the operation needs a Walkthrough and none is
	// posted.
	RejectedNoWalkthrough RejectionReason = "no_walkthrough"
	// RejectedEmptyChangeSet means there is nothing to review.
	RejectedEmptyChangeSet RejectionReason = "empty_change_set"
	// RejectedMalformedBrief means the Brief is missing something required.
	RejectedMalformedBrief RejectionReason = "malformed_brief"
	// RejectedMalformedStep means a Step is missing something required, or an
	// Excerpt range is not self-consistent.
	RejectedMalformedStep RejectionReason = "malformed_step"
	// RejectedDerivationFailed means the Changed Lines could not be derived from
	// git — usually a range that does not resolve.
	RejectedDerivationFailed RejectionReason = "derivation_failed"
	// RejectedUnresolvableExcerpt means an Excerpt names a range the working
	// tree cannot satisfy.
	RejectedUnresolvableExcerpt RejectionReason = "unresolvable_excerpt"
	// RejectedUncoveredChanges means the plan leaves Changed Lines unshown.
	RejectedUncoveredChanges RejectionReason = "uncovered_changes"
	// RejectedOversizedStep means a Step exceeds the budget without justifying it.
	RejectedOversizedStep RejectionReason = "oversized_step"
	// RejectedNoSuchStep means a navigation names a position that does not exist.
	RejectedNoSuchStep RejectionReason = "no_such_step"
	// RejectedBadSelection means an Anchor selection is malformed or out of range.
	RejectedBadSelection RejectionReason = "bad_selection"
	// RejectedNoSuchChangeRequest means a withdrawal names an unknown Change Request.
	RejectedNoSuchChangeRequest RejectionReason = "no_such_change_request"
	// RejectedWalkthroughFinished means an edit was attempted after finishing.
	RejectedWalkthroughFinished RejectionReason = "walkthrough_finished"
	// RejectedEmptyAcknowledgement means an Acknowledgement claims a file that has
	// no changes, so it accounts for nothing.
	RejectedEmptyAcknowledgement RejectionReason = "empty_acknowledgement"
	// RejectedNoSuchAcknowledgement means an expansion names an Acknowledgement
	// that does not exist on that Step.
	RejectedNoSuchAcknowledgement RejectionReason = "no_such_acknowledgement"
)

// Rejection is a refusal that names its cause.
type Rejection struct {
	Reason RejectionReason
	Detail string
}

func (r *Rejection) Error() string {
	return fmt.Sprintf("%s: %s", r.Reason, r.Detail)
}

func reject(reason RejectionReason, format string, args ...any) *Rejection {
	return &Rejection{Reason: reason, Detail: fmt.Sprintf(format, args...)}
}

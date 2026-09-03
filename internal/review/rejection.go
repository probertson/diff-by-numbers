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

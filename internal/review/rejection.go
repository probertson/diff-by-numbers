package review

import (
	"fmt"
	"strings"
)

// RejectionReason names why a Round was refused. The Authoring Agent acts
// on this, so it is a value rather than prose: a rejection must tell the agent
// what to fix, not merely that something was wrong.
type RejectionReason string

const (
	// RejectedWalkthroughActive means a Round is already under review. Its old
	// name stays until the Inbox (#109) removes it.
	RejectedWalkthroughActive RejectionReason = "walkthrough_active"
	// RejectedNoRound means the operation needs a Round and none is posted.
	RejectedNoRound RejectionReason = "no_round"
	// RejectedEmptyChangeSet means there is nothing to review.
	RejectedEmptyChangeSet RejectionReason = "empty_change_set"
	// RejectedMalformedBase means a repository's base ref is missing, or is a
	// range where a single ref belongs.
	RejectedMalformedBase RejectionReason = "malformed_base"
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
	// RejectedNoSuchComment means a withdrawal names an unknown Comment.
	RejectedNoSuchComment RejectionReason = "no_such_comment"
	// RejectedRoundHandedOff means an edit was attempted after the Round was
	// handed off.
	RejectedRoundHandedOff RejectionReason = "round_handed_off"
	// RejectedEmptyAcknowledgement means an Acknowledgement claims a file that has
	// no changes, so it accounts for nothing.
	RejectedEmptyAcknowledgement RejectionReason = "empty_acknowledgement"
	// RejectedNoSuchAcknowledgement means an expansion names an Acknowledgement
	// that does not exist on that Step.
	RejectedNoSuchAcknowledgement RejectionReason = "no_such_acknowledgement"
	// RejectedMalformedDisposition means a Revision Round does not account for the
	// previous round's Comments correctly.
	RejectedMalformedDisposition RejectionReason = "malformed_disposition"
	// RejectedUnknownReview means a conclude or a replacement names a review id
	// that is not the one currently under review — including one that has been
	// concluded, which is no longer under review at all.
	RejectedUnknownReview RejectionReason = "unknown_review"
	// RejectedReviewOver means a post was made to a Session whose Review has
	// ended. A Session holds one Review for its whole life, so new work belongs in
	// a new one.
	RejectedReviewOver RejectionReason = "review_over"
	// RejectedNoPreviousRound means the Reviewer asked to compare a first round
	// with the round before it, which does not exist.
	RejectedNoPreviousRound RejectionReason = "no_previous_round"
	// RejectedAlreadyReRaised means a resolution the Reviewer disputed already has
	// a Comment standing against it this round, so re-raising again would only
	// send the agent the same point twice.
	RejectedAlreadyReRaised RejectionReason = "already_re_raised"
)

// Problem is one thing wrong with a post: what kind, and what specifically.
type Problem struct {
	Reason RejectionReason
	Detail string
}

// Rejection is a refusal that names every cause it could find.
//
// A post used to be refused at the first fault, so an Authoring Agent found
// them one at a time — and each attempt re-sent the entire Round, Brief
// and every Excerpt, to learn about the next. Four posts to get one accepted
// was ordinary. Everything the later checks can see is present on the first
// attempt, so they all run and all report.
//
// Problems carries at most one entry per reason, in the order the checks run.
// A structural failure is a list of one: the later checks mean nothing without
// a well-formed Round, so their guesses would be noise.
type Rejection struct {
	Problems []Problem
}

// Has reports whether any problem has this reason.
func (r *Rejection) Has(reason RejectionReason) bool {
	for _, problem := range r.Problems {
		if problem.Reason == reason {
			return true
		}
	}
	return false
}

// rejectAll gathers several problems into one refusal, dropping the stages that
// found nothing. It returns nil when they all passed, so callers can treat it
// like any other check.
func rejectAll(problems []Problem) *Rejection {
	if len(problems) == 0 {
		return nil
	}
	return &Rejection{Problems: problems}
}

func (r *Rejection) Error() string {
	if len(r.Problems) == 1 {
		return fmt.Sprintf("%s: %s", r.Problems[0].Reason, r.Problems[0].Detail)
	}
	parts := make([]string, 0, len(r.Problems))
	for _, problem := range r.Problems {
		parts = append(parts, fmt.Sprintf("%s: %s", problem.Reason, problem.Detail))
	}
	return strings.Join(parts, "\n")
}

func reject(reason RejectionReason, format string, args ...any) *Rejection {
	return &Rejection{Problems: []Problem{{Reason: reason, Detail: fmt.Sprintf(format, args...)}}}
}

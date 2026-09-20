package review

// A Revision Round is a second Walkthrough posted after a Finish. It re-derives
// the full Change Set, pre-marks as shown every Changed Line whose content is
// unchanged since the previous round (ADR-0007), and accounts for every Comment
// the previous round raised. All of this lives in memory: restarting the
// daemon mid-review starts the review over.

// DispositionStatus is what the Authoring Agent did with a Comment.
type DispositionStatus string

const (
	// DispositionAddressed means the agent made the change.
	DispositionAddressed DispositionStatus = "addressed"
	// DispositionAnswered means the agent responded without changing anything,
	// as it would to a question.
	DispositionAnswered DispositionStatus = "answered"
	// DispositionDeclined means the agent will not make the change, and says why.
	DispositionDeclined DispositionStatus = "declined"
)

// Disposition is the Authoring Agent's account, posted with a Revision Round, of
// what it did with one Comment from the previous round.
type Disposition struct {
	CommentID int
	Status    DispositionStatus
	// Response is required when Answered, since it is the answer, and when
	// Declined, since a decline the Reviewer cannot weigh is just a refusal. It
	// is optional when Addressed.
	Response string
}

// ResolvedDisposition pairs a previous-round Comment with what the agent
// did about it, ready to show before any code in a Revision Round.
type ResolvedDisposition struct {
	Comment  Comment
	Status   DispositionStatus
	Response string
}

// roundState is what one accepted round leaves behind for the next to be
// scoped against: where every repository's working tree stood, which base it was
// derived from, and which lines and Opaque Changes the Reviewer was shown.
type roundState struct {
	// snapshots and bases are keyed by repository root.
	snapshots map[string]string
	bases     map[string]string
	lines     map[ChangedLine]bool
	opaque    map[fileRef]bool
}

// snapshotAll records every repository's working tree. A repository that cannot
// be snapshotted yields no entry, and the next round simply demands its lines:
// pre-marking is an optimisation over the coverage guarantee, never a hole in it.
func snapshotAll(snapshotter Snapshotter, set ChangeSet) map[string]string {
	out := map[string]string{}
	for _, repo := range set.Repositories {
		tree, err := snapshotter.Snapshot(repo.Root)
		if err != nil {
			continue
		}
		out[repo.Root] = tree
	}
	return out
}

// captureRound records what this round showed, for the next round to scope
// against.
func captureRound(l ledger, snapshots map[string]string, bases map[string]string) roundState {
	state := roundState{
		snapshots: snapshots,
		bases:     bases,
		lines:     map[ChangedLine]bool{},
		opaque:    map[fileRef]bool{},
	}
	for _, line := range l.lines {
		state.lines[line] = true
	}
	for _, opaque := range l.opaque {
		state.opaque[fileRef{opaque.Repository, opaque.File}] = true
	}
	return state
}

// preMarkUnchanged returns the Changed Lines of a Revision Round the Reviewer
// has already read, so the Coverage Ledger is left pointing at what moved.
//
// A line is pre-marked when it maps to a line that was a Changed Line in the
// previous round, on the same side, and nothing touched it in between. The
// mapping is positional — a diff of the two rounds' working-tree snapshots — so
// a blank line or a lone `}` is handled as exactly as anything else. The rule it
// replaces matched on line content and required uniqueness within the file,
// which in any braced language disqualified a large share of every Change Set
// (ADR-0007, superseded in part by ADR-0014).
//
// The new side maps through the round-over-round snapshot diff. The old side is
// relative to the merge-base: while that has not moved, an old-side line sits
// where it sat, so the mapping is the identity; when the branch has been rebased
// underneath the review the base itself has moved, and old-side lines map
// through a diff of the two bases.
//
// Anything that cannot be worked out is simply not pre-marked, which demands the
// line — the same conservative answer as a daemon restart.
func (s *Session) preMarkUnchanged(l ledger, set ChangeSet, snapshots, bases map[string]string) (map[ChangedLine]bool, map[fileRef]bool) {
	snapshotter, ok := s.deriver.(Snapshotter)
	if !ok || s.prior.snapshots == nil {
		return nil, nil
	}

	preShown := map[ChangedLine]bool{}
	preShownOpaque := map[fileRef]bool{}
	for _, repo := range set.Repositories {
		previous, had := s.prior.snapshots[repo.Root]
		current, took := snapshots[repo.Root]
		if !had || !took {
			continue
		}
		forward, err := snapshotter.MapBetween(repo.Root, previous, current)
		if err != nil {
			continue
		}
		oldSide := s.oldSideMapping(snapshotter, repo, bases, forward)

		for _, line := range l.lines {
			if line.Repository != repo.Root {
				continue
			}
			mapping := forward
			if line.Side == OldSide {
				mapping = oldSide
			}
			if mapping == nil {
				continue
			}
			at, mapped := mapping.Lookup(line.File, line.Line)
			if !mapped {
				continue
			}
			was := ChangedLine{Repository: repo.Root, File: at.File, Side: line.Side, Line: at.Line}
			if s.prior.lines[was] {
				preShown[line] = true
			}
		}

		// An Opaque Change has no lines to map. It counts as already reviewed
		// when the file is identical between the two rounds — same path, blob and
		// mode, which is what an absence from the diff means — and it was in the
		// previous round's Change Set.
		//
		// The base has to be the same commit as well. What makes an Opaque Change
		// what it is — the source it was renamed from, the mode it changed from —
		// is derived against the merge-base, so a base that moved can change the
		// Opaque Change the Reviewer would be shown while leaving both working
		// trees byte-identical. Rather than compare those facts separately, a
		// moved base simply demands them again.
		sameBase := s.prior.bases[repo.Root] == bases[repo.Root] && bases[repo.Root] != ""
		for _, opaque := range l.opaque {
			if opaque.Repository != repo.Root || !sameBase || forward.Touched(opaque.File) {
				continue
			}
			ref := fileRef{opaque.Repository, opaque.File}
			if s.prior.opaque[ref] {
				preShownOpaque[ref] = true
			}
		}
	}
	return preShown, preShownOpaque
}

// oldSideMapping is how an old-side line finds its previous-round counterpart.
//
// Old-side lines are positions in the merge-base, not the working tree, so the
// working-tree snapshot diff says nothing about them. While the base is the same
// commit they have not moved at all, and identityMapping says so. If the branch
// was rebased under the review the base itself changed, and the two bases are
// diffed exactly as two snapshots are.
func (s *Session) oldSideMapping(snapshotter Snapshotter, repo Repository, bases map[string]string, forward RoundMapping) RoundMapping {
	previous, had := s.prior.bases[repo.Root]
	current, have := bases[repo.Root]
	if !had || !have {
		return nil
	}
	if previous == current {
		// The base has not moved, so an old-side line sits exactly where it sat.
		// Its path can still have moved, though: the ledger files old-side lines
		// under the file's current name, so a file renamed between rounds would
		// otherwise never match what the previous round recorded.
		return unmovedSince{forward}
	}
	mapping, err := snapshotter.MapBetween(repo.Root, previous, current)
	if err != nil {
		return nil
	}
	return mapping
}

// unmovedSince is the mapping for lines whose numbering cannot have changed —
// old-side positions in a merge-base that is still the same commit — but whose
// file may have been renamed. Line numbers pass through untouched; the path
// comes from the round-over-round mapping.
type unmovedSince struct{ forward RoundMapping }

func (u unmovedSince) Lookup(file string, line int) (Position, bool) {
	return Position{File: u.forward.PathIn(file), Line: line}, true
}

func (u unmovedSince) Touched(file string) bool { return u.forward.Touched(file) }

func (u unmovedSince) PathIn(file string) string { return u.forward.PathIn(file) }

// resolveDispositions pairs each posted Disposition with the previous round's
// Comment it names, and refuses a Revision Round that does not account for
// every one — addressed, or answered or declined with a response.
func (s *Session) resolveDispositions(dispositions []Disposition) ([]ResolvedDisposition, *Rejection) {
	prior := s.comments
	byID := make(map[int]Comment, len(prior))
	for _, comment := range prior {
		byID[comment.ID] = comment
	}

	seen := map[int]bool{}
	out := make([]ResolvedDisposition, 0, len(dispositions))
	for _, disposition := range dispositions {
		comment, ok := byID[disposition.CommentID]
		if !ok {
			return nil, reject(RejectedMalformedDisposition,
				"a disposition names Comment %d, which the previous round did not raise", disposition.CommentID)
		}
		if seen[disposition.CommentID] {
			return nil, reject(RejectedMalformedDisposition,
				"Comment %d has more than one disposition", disposition.CommentID)
		}
		seen[disposition.CommentID] = true

		switch disposition.Status {
		case DispositionAddressed:
		case DispositionAnswered:
			if disposition.Response == "" {
				return nil, reject(RejectedMalformedDisposition,
					"answering Comment %d needs a response: the response is the answer", disposition.CommentID)
			}
		case DispositionDeclined:
			if disposition.Response == "" {
				return nil, reject(RejectedMalformedDisposition,
					"declining Comment %d needs a response the Reviewer can weigh", disposition.CommentID)
			}
		default:
			return nil, reject(RejectedMalformedDisposition,
				"Comment %d must be disposed as %q, %q or %q", disposition.CommentID, DispositionAddressed, DispositionAnswered, DispositionDeclined)
		}
		out = append(out, ResolvedDisposition{Comment: comment, Status: disposition.Status, Response: disposition.Response})
	}

	for _, comment := range prior {
		if !seen[comment.ID] {
			return nil, reject(RejectedMalformedDisposition,
				"Comment %d from the previous round has no disposition; a Revision Round must account for every one", comment.ID)
		}
	}
	return out, nil
}

// Dispositions reports how the previous round's Comments were resolved,
// for display before any code.
func (s *Session) Dispositions() []ResolvedDisposition {
	out := make([]ResolvedDisposition, len(s.dispositions))
	copy(out, s.dispositions)
	return out
}

// ReRaise raises again a Comment the agent declined or answered, carrying its
// original Anchor into the current round so the Reviewer can insist in place
// without re-composing it. A resolution the agent addressed is not re-raisable:
// the code moved, and there is fresh code to comment on instead.
//
// note is what the Comment says. An empty one keeps the original wording, which
// is what a Reviewer who simply disagrees wants; anything else replaces it, so a
// follow-up or a counter-argument can go with the push-back.
//
// A resolution can carry only one standing re-raise (#80). Withdrawing that
// Comment frees it to be raised again.
func (s *Session) ReRaise(commentID int, note string) (Comment, error) {
	if s.finished {
		return Comment{}, reject(RejectedWalkthroughFinished,
			"this Walkthrough is handed off; resume it before re-raising")
	}
	for _, disposition := range s.dispositions {
		if disposition.Comment.ID != commentID {
			continue
		}
		if disposition.Status == DispositionAddressed {
			return Comment{}, reject(RejectedNoSuchComment,
				"Comment %d was addressed, so there is nothing to push back on; comment on the new code instead", commentID)
		}
		if standing, ok := s.reRaiseOf(commentID); ok {
			return Comment{}, reject(RejectedAlreadyReRaised,
				"Comment %d is already re-raised as Comment %d; withdraw that one to raise it afresh", commentID, standing.ID)
		}
		if note == "" {
			note = disposition.Comment.Note
		}
		s.nextCommentID++
		// Step is left 0: the previous round's Step number means nothing in this
		// round, whose Steps are authored afresh. The Anchor carries the location,
		// and it is self-contained. Step 0 keeps it from flagging the wrong Step.
		comment := Comment{
			ID:           s.nextCommentID,
			Step:         0,
			Anchor:       disposition.Comment.Anchor,
			Note:         note,
			ReRaisedFrom: commentID,
		}
		s.comments = append(s.comments, comment)
		return comment, nil
	}
	return Comment{}, reject(RejectedNoSuchComment,
		"there is no Comment %d from the previous round to re-raise", commentID)
}

// reRaiseOf is the Comment standing against a previous round's resolution this
// round, if the Reviewer has raised one.
func (s *Session) reRaiseOf(commentID int) (Comment, bool) {
	for _, comment := range s.comments {
		if comment.ReRaisedFrom == commentID {
			return comment, true
		}
	}
	return Comment{}, false
}

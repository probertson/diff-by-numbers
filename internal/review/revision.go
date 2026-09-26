package review

// A Revision Round is a Round posted after a Hand Off. It re-derives
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
// scoped against: its Round — where every repository's working tree stood and
// which base it was derived from — and which lines and Opaque Changes the
// Reviewer was shown.
type roundState struct {
	RoundSource
	lines  map[ChangedLine]bool
	opaque map[fileRef]bool
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
func captureRound(l ledger, round RoundSource) roundState {
	state := roundState{
		RoundSource: round,
		lines:       map[ChangedLine]bool{},
		opaque:      map[fileRef]bool{},
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
func (s *Session) preMarkUnchanged(l ledger, set ChangeSet, current RoundSource, earlier roundState) (map[ChangedLine]bool, map[fileRef]bool) {
	snapshotter, ok := s.deriver.(Snapshotter)
	if !ok || earlier.Snapshots == nil {
		return nil, nil
	}

	preShown := map[ChangedLine]bool{}
	preShownOpaque := map[fileRef]bool{}
	for _, repo := range set.Repositories {
		previous, had := earlier.Snapshots[repo.Root]
		now, took := current.Snapshots[repo.Root]
		if !had || !took {
			continue
		}
		forward, err := snapshotter.MapBetween(repo.Root, previous, now)
		if err != nil {
			continue
		}
		oldSide := oldSideMapping(snapshotter, repo, current, earlier, forward)

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
			if earlier.lines[was] {
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
		sameBase := earlier.Bases[repo.Root] == current.Bases[repo.Root] && current.Bases[repo.Root] != ""
		for _, opaque := range l.opaque {
			if opaque.Repository != repo.Root || !sameBase || forward.Touched(opaque.File) {
				continue
			}
			ref := fileRef{opaque.Repository, opaque.File}
			if earlier.opaque[ref] {
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
func oldSideMapping(snapshotter Snapshotter, repo Repository, current RoundSource, earlier roundState, forward RoundMapping) RoundMapping {
	previous, had := earlier.Bases[repo.Root]
	now, have := current.Bases[repo.Root]
	if !had || !have {
		return nil
	}
	if previous == now {
		// The base has not moved, so an old-side line sits exactly where it sat.
		// Its path can still have moved, though: the ledger files old-side lines
		// under the file's current name, so a file renamed between rounds would
		// otherwise never match what the previous round recorded.
		return unmovedSince{forward}
	}
	mapping, err := snapshotter.MapBetween(repo.Root, previous, now)
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

// Edits has nothing to report: this mapping stands for old-side positions in a
// merge-base that has not moved.
func (u unmovedSince) Edits(string) []RoundEdit { return nil }

func (u unmovedSince) Files() []string { return nil }

// accounting is one kind of thing a Revision Round must account for, item by
// item: what the previous round left, and what each entry accounting for one is
// called. It is what lets a second kind join Comments without repeating the
// rule that every one is accounted for exactly once.
type accounting struct {
	reason RejectionReason
	// item names what is accounted for, and entry what accounts for one.
	item  string
	entry string
	// origin is what the previous round did to leave an item: raised a Comment.
	origin string
}

var commentAccounting = accounting{
	reason: RejectedMalformedDisposition,
	item:   "Comment",
	entry:  "disposition",
	origin: "raise",
}

// accountFor checks that entries account for every one of the previous round's
// items, named by id, once each. check judges an entry on its own terms — the
// statuses its kind allows, and which of them need a response — and is only
// asked about an entry naming an item that round left.
func accountFor[E any](kind accounting, prior []int, entries []E, idOf func(E) int, check func(E) *Rejection) *Rejection {
	known := make(map[int]bool, len(prior))
	for _, id := range prior {
		known[id] = true
	}

	seen := map[int]bool{}
	for _, entry := range entries {
		id := idOf(entry)
		if !known[id] {
			return reject(kind.reason,
				"a %s names %s %d, which the previous round did not %s", kind.entry, kind.item, id, kind.origin)
		}
		if seen[id] {
			return reject(kind.reason, "%s %d has more than one %s", kind.item, id, kind.entry)
		}
		seen[id] = true
		if rejection := check(entry); rejection != nil {
			return rejection
		}
	}

	for _, id := range prior {
		if !seen[id] {
			return reject(kind.reason,
				"%s %d from the previous round has no %s; a Revision Round must account for every one", kind.item, id, kind.entry)
		}
	}
	return nil
}

// resolveDispositions pairs each posted Disposition with the previous round's
// Comment it names, and refuses a Revision Round that does not account for
// every one — addressed, or answered or declined with a response.
func resolveDispositions(dispositions []Disposition, prior []Comment) ([]ResolvedDisposition, *Rejection) {
	byID := make(map[int]Comment, len(prior))
	ids := make([]int, 0, len(prior))
	for _, comment := range prior {
		byID[comment.ID] = comment
		ids = append(ids, comment.ID)
	}

	rejection := accountFor(commentAccounting, ids, dispositions,
		func(d Disposition) int { return d.CommentID }, checkDisposition)
	if rejection != nil {
		return nil, rejection
	}

	out := make([]ResolvedDisposition, 0, len(dispositions))
	for _, disposition := range dispositions {
		out = append(out, ResolvedDisposition{
			Comment: byID[disposition.CommentID], Status: disposition.Status, Response: disposition.Response,
		})
	}
	return out, nil
}

// checkDisposition judges one Disposition: a status a Comment can have, with the
// response it needs.
func checkDisposition(disposition Disposition) *Rejection {
	switch disposition.Status {
	case DispositionAddressed:
	case DispositionAnswered:
		if disposition.Response == "" {
			return reject(RejectedMalformedDisposition,
				"answering Comment %d needs a response: the response is the answer", disposition.CommentID)
		}
	case DispositionDeclined:
		if disposition.Response == "" {
			return reject(RejectedMalformedDisposition,
				"declining Comment %d needs a response the Reviewer can weigh", disposition.CommentID)
		}
	default:
		return reject(RejectedMalformedDisposition,
			"Comment %d must be disposed as %q, %q or %q", disposition.CommentID, DispositionAddressed, DispositionAnswered, DispositionDeclined)
	}
	return nil
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
		return Comment{}, reject(RejectedRoundHandedOff,
			"this Round is handed off; resume it before re-raising")
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

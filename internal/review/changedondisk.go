package review

// DiskWatch is the optional capability that says whether a file has been edited
// since the round was posted.
//
// dbn reads every Walkthrough's code from its Round Snapshot, never from disk, so
// what the Reviewer sees, expands and anchors is exactly what was posted however
// the files move afterwards (ADR-0004). An edit therefore changes nothing on
// screen except a warning: the Reviewer is told they are looking at the posted
// version, and an Anchor says its line numbers are from it. A Resolver that
// cannot answer — a stub in a test that does not care — flags nothing.
type DiskWatch interface {
	// ChangedOnDisk reports whether a file's current content differs from the
	// blob the snapshot holds for it. A file that can no longer be read has
	// changed.
	ChangedOnDisk(repository, snapshot, file string) bool
}

// changedOnDisk reports whether the file an Excerpt reads has been edited since
// this round's snapshot was taken. Only the new side is read from the snapshot:
// the before-side sits at the merge-base, which no edit on disk can move, so an
// old-side Excerpt is never flagged. Nor is a repository git could not snapshot,
// whose code is read from disk and so cannot differ from it.
func (s *Session) changedOnDisk(e Excerpt) bool {
	if e.Side != NewSide {
		return false
	}
	watch, ok := s.resolver.(DiskWatch)
	if !ok {
		return false
	}
	snapshot, ok := s.round.Snapshots[e.Repository]
	if !ok {
		return false
	}
	return watch.ChangedOnDisk(e.Repository, snapshot, e.File)
}

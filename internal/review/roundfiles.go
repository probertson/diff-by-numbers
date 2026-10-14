package review

// RoundFiles says where a surface reads a file of the Round on screen that no
// Excerpt names, such as an image a Document refers to (#189): a file in the
// Change Set from the Round Snapshot, so it matches what is under review, and
// any other from disk. It is a copy, so it can be read outside the lock that
// guards the Session.
type RoundFiles struct {
	repositories map[string]bool
	snapshots    map[string]string
	changed      map[fileRef]bool
}

// RoundFiles is where the Round on screen's files are read from.
func (s *Session) RoundFiles() RoundFiles {
	files := RoundFiles{repositories: map[string]bool{}, snapshots: map[string]string{}, changed: map[fileRef]bool{}}
	if s.current == nil {
		return files
	}
	for _, repo := range s.current.ChangeSet.Repositories {
		files.repositories[repo.Root] = true
	}
	for repository, tree := range s.latest.Snapshots {
		files.snapshots[repository] = tree
	}
	for _, line := range s.ledger.lines {
		files.changed[fileRef{line.Repository, line.File}] = true
	}
	for _, change := range s.ledger.opaque {
		files.changed[fileRef{change.Repository, change.File}] = true
	}
	return files
}

// Holds reports whether a repository is under review in the Round.
func (f RoundFiles) Holds(repository string) bool { return f.repositories[repository] }

// Snapshot is the Round Snapshot a file is read from: ok is true when the file
// is in the Change Set and its repository was snapshotted. Otherwise the file
// is read from disk, as code is for a repository git could not snapshot.
func (f RoundFiles) Snapshot(repository, file string) (tree string, ok bool) {
	if !f.changed[fileRef{repository, file}] {
		return "", false
	}
	tree, ok = f.snapshots[repository]
	return tree, ok
}

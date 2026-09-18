package review

// Hasher fingerprints a file's current content, so the core can tell when a file
// has changed under review without reading bytes itself. It is an optional
// capability of a Resolver: when the resolver provides it, staleness is enforced;
// when it does not (a stub in a test that does not care), staleness is skipped.
type Hasher interface {
	Hash(repository, file string) (string, error)
}

// fileRef identifies a file within a repository — the unit staleness tracks,
// since a change anywhere in a file shifts the line numbers an Excerpt named.
type fileRef struct {
	repository string
	file       string
}

// hashExcerptFiles fingerprints every working-tree file a Step may read, taken
// the moment the Walkthrough is accepted: those a new-side Excerpt reads, and
// those an Acknowledgement claims, since an expanded one can be anchored. Old-side
// Excerpts are not read from the working tree, so they are not tracked. A file
// that cannot be hashed (a deleted one, say) is simply left untracked rather than
// failing the post.
func (s *Session) hashExcerptFiles(steps []Step) map[fileRef]string {
	hasher, ok := s.resolver.(Hasher)
	if !ok {
		return nil
	}
	hashes := map[fileRef]string{}
	track := func(repository, file string) {
		ref := fileRef{repository, file}
		if _, done := hashes[ref]; done {
			return
		}
		if hash, err := hasher.Hash(repository, file); err == nil {
			hashes[ref] = hash
		}
	}
	for _, step := range steps {
		for _, excerpt := range step.Excerpts {
			if excerpt.Side == NewSide {
				track(excerpt.Repository, excerpt.File)
			}
		}
		for _, ack := range step.Acknowledgements {
			for _, file := range ack.Files {
				track(ack.Repository, file)
			}
		}
	}
	return hashes
}

// fileChanged reports whether a tracked file has changed since the Walkthrough
// was accepted. A file that no longer hashes has changed too; an untracked one
// never reads as changed.
func (s *Session) fileChanged(repository, file string) bool {
	hasher, ok := s.resolver.(Hasher)
	if !ok || s.hashes == nil {
		return false
	}
	original, tracked := s.hashes[fileRef{repository, file}]
	if !tracked {
		return false
	}
	current, err := hasher.Hash(repository, file)
	return err != nil || current != original
}

// staleFiles reports the files a Step's Excerpts read that have changed since the
// Walkthrough was accepted. A file that no longer hashes (deleted, unreadable) is
// stale too: whatever the Excerpt described is no longer there. The result is the
// blast radius of the change, which is per file and never the whole Walkthrough.
func (s *Session) staleFiles(step Step) []string {
	if _, ok := s.resolver.(Hasher); !ok || s.hashes == nil {
		return nil
	}
	seen := map[fileRef]bool{}
	var stale []string
	for _, excerpt := range step.Excerpts {
		if excerpt.Side != NewSide {
			continue
		}
		ref := fileRef{excerpt.Repository, excerpt.File}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		if s.fileChanged(excerpt.Repository, excerpt.File) {
			stale = append(stale, excerpt.File)
		}
	}
	return stale
}

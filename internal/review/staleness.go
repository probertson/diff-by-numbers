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

// hashExcerptFiles fingerprints every file a new-side Excerpt reads, taken the
// moment the Walkthrough is accepted. Old-side Excerpts are not read from the
// working tree, so they are not tracked. A file that cannot be hashed is simply
// left untracked rather than failing the post.
func (s *Session) hashExcerptFiles(steps []Step) map[fileRef]string {
	hasher, ok := s.resolver.(Hasher)
	if !ok {
		return nil
	}
	hashes := map[fileRef]string{}
	for _, step := range steps {
		for _, excerpt := range step.Excerpts {
			if excerpt.Side != NewSide {
				continue
			}
			ref := fileRef{excerpt.Repository, excerpt.File}
			if _, done := hashes[ref]; done {
				continue
			}
			if hash, err := hasher.Hash(excerpt.Repository, excerpt.File); err == nil {
				hashes[ref] = hash
			}
		}
	}
	return hashes
}

// staleFiles reports the files a Step's Excerpts read that have changed since the
// Walkthrough was accepted. A file that no longer hashes (deleted, unreadable) is
// stale too: whatever the Excerpt described is no longer there. The result is the
// blast radius of the change, which is per file and never the whole Walkthrough.
func (s *Session) staleFiles(step Step) []string {
	hasher, ok := s.resolver.(Hasher)
	if !ok || s.hashes == nil {
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
		original, tracked := s.hashes[ref]
		if !tracked {
			continue
		}
		current, err := hasher.Hash(excerpt.Repository, excerpt.File)
		if err != nil || current != original {
			stale = append(stale, excerpt.File)
		}
	}
	return stale
}

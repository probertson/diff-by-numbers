// Package workingtree reads Excerpt content off disk. It is the file-reading
// half of the git adapter: the review core names ranges, this resolves them
// against what is actually in the working tree, so nothing the Reviewer sees
// was supplied by the agent.
package workingtree

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
)

type fileRef struct{ repository, file string }

// Resolver reads Excerpt content: the after-side from the working tree, and the
// before-side from git's object store at each repository's merge-base. It learns
// those ranges from the Change Set when a Walkthrough is posted.
//
// The before-side is an immutable committed blob at a fixed merge-base, so both the
// merge-base and the before-side file content are cached for the life of the
// Walkthrough — the view is re-resolved on every poll, and re-shelling out to git
// each time would be needless. The caches are reset when a new Walkthrough is
// posted, and caching the base at post time also keeps it consistent with the
// baseline the Coverage Ledger was derived from.
type Resolver struct {
	ranges map[string]string    // repository root -> range ref
	bases  map[string]string    // repository root -> merge-base revision
	before map[fileRef][]string // (repository, file) -> before-side lines at the base
}

func NewResolver() *Resolver {
	return &Resolver{}
}

// UseChangeSet records each repository's range so the before-side can be read from
// the right merge-base, and clears the caches from any previous Walkthrough. It is
// called when a Walkthrough is posted (the core hands ranges to the resolver via
// the ChangeSetAware capability).
func (r *Resolver) UseChangeSet(cs review.ChangeSet) {
	r.ranges = map[string]string{}
	r.bases = map[string]string{}
	r.before = map[fileRef][]string{}
	for _, repo := range cs.Repositories {
		r.ranges[repo.Root] = repo.Range
	}
}

// Resolve reads the named range. The after-side comes from the working tree; the
// before-side from git history. It refuses rather than approximates: a range the
// source cannot satisfy is a problem to show the Reviewer, never a partial answer.
func (r *Resolver) Resolve(e review.Excerpt) ([]review.Line, error) {
	if e.Side == review.OldSide {
		return r.resolveBefore(e)
	}

	content, err := os.ReadFile(filepath.Join(e.Repository, filepath.FromSlash(e.File)))
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", e.File, err)
	}

	all := splitLines(string(content))
	if e.LastLine > len(all) {
		return nil, fmt.Errorf("%s has %d lines, but the Excerpt asks for %d-%d", e.File, len(all), e.FirstLine, e.LastLine)
	}

	lines := make([]review.Line, 0, e.LastLine-e.FirstLine+1)
	for n := e.FirstLine; n <= e.LastLine; n++ {
		lines = append(lines, review.Line{Number: n, Text: all[n-1]})
	}
	return lines, nil
}

// resolveBefore reads a before-side range from git, caching the merge-base per
// repository and the before-side file content per file so a repeated render does
// not re-shell to git for content that cannot change.
func (r *Resolver) resolveBefore(e review.Excerpt) ([]review.Line, error) {
	rangeRef, ok := r.ranges[e.Repository]
	if !ok {
		return nil, fmt.Errorf("the before-side of %s cannot be read: no range is known for %s", e.File, e.Repository)
	}

	base, ok := r.bases[e.Repository]
	if !ok {
		resolved, err := git.MergeBase(e.Repository, rangeRef)
		if err != nil {
			return nil, err
		}
		base = resolved
		r.bases[e.Repository] = base
	}

	key := fileRef{e.Repository, e.File}
	all, ok := r.before[key]
	if !ok {
		lines, err := git.ReadFileAt(e.Repository, base, e.File)
		if err != nil {
			return nil, err
		}
		all = lines
		r.before[key] = all
	}

	if e.LastLine > len(all) {
		return nil, fmt.Errorf("the before-side of %s has %d lines, but the Excerpt asks for %d-%d", e.File, len(all), e.FirstLine, e.LastLine)
	}
	out := make([]review.Line, 0, e.LastLine-e.FirstLine+1)
	for n := e.FirstLine; n <= e.LastLine; n++ {
		out = append(out, review.Line{Number: n, Text: all[n-1]})
	}
	return out, nil
}

// Hash fingerprints a file's current content, so the review core can tell when a
// file has changed under review. It hashes whole-file bytes: a change anywhere in
// the file shifts the line numbers an Excerpt named, so the whole file is the
// unit of staleness.
func (r *Resolver) Hash(repository, file string) (string, error) {
	content, err := os.ReadFile(filepath.Join(repository, filepath.FromSlash(file)))
	if err != nil {
		return "", fmt.Errorf("could not read %s: %w", file, err)
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}

// splitLines splits file content into lines without inventing a final empty
// line for a trailing newline, and without losing a final unterminated one.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

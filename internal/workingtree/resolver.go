// Package workingtree reads Excerpt content. It is the file-reading half of the
// git adapter: the review core names a Round, and this reads what that Round
// holds, so nothing the Reviewer sees was supplied by the agent.
package workingtree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
)

// blobRef names one file as one git object holds it: a Round Snapshot for the
// after-side, a merge-base for the before-side.
type blobRef struct{ repository, rev, file string }

// Resolver reads Excerpt content: the after-side from the Round Snapshot, and the
// before-side from the merge-base the Change Set was derived from.
//
// Both are immutable git objects, so what is read from them is cached for good —
// the view is re-resolved on every poll, and re-shelling out to git each time
// would be needless. A new round names new objects, so nothing ever needs
// clearing for one.
type Resolver struct {
	files map[blobRef][]string // the lines of each file read, by the object read from
	blobs map[blobRef]string   // each posted file's blob id, by its snapshot
}

func NewResolver() *Resolver {
	return &Resolver{files: map[blobRef][]string{}, blobs: map[blobRef]string{}}
}

// Resolve reads the named range as the Round holds it. It refuses rather than
// approximates: a range the source cannot satisfy is a problem to show the
// Reviewer, never a partial answer.
func (r *Resolver) Resolve(e review.Excerpt, round review.Round) ([]review.Line, error) {
	if e.Side == review.OldSide {
		return r.resolveBefore(e, round)
	}

	snapshot, ok := round.Snapshots[e.Repository]
	if !ok {
		return resolveOnDisk(e)
	}
	all, err := r.read(blobRef{e.Repository, snapshot, e.File}, git.ReadBlob)
	if err != nil {
		return nil, err
	}
	return linesInRange(all, e, e.File)
}

// resolveOnDisk reads the after-side from the working tree, for a repository git
// could not snapshot. Nothing is cached, since the file can change.
func resolveOnDisk(e review.Excerpt) ([]review.Line, error) {
	content, err := os.ReadFile(filepath.Join(e.Repository, filepath.FromSlash(e.File)))
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", e.File, err)
	}
	return linesInRange(splitLines(string(content)), e, e.File)
}

// resolveBefore reads a before-side range at the round's merge-base.
func (r *Resolver) resolveBefore(e review.Excerpt, round review.Round) ([]review.Line, error) {
	base, ok := round.Bases[e.Repository]
	if !ok {
		return nil, fmt.Errorf("the before-side of %s cannot be read: no merge-base is known for %s", e.File, e.Repository)
	}
	all, err := r.read(blobRef{e.Repository, base, e.File}, git.ReadFileAt)
	if err != nil {
		return nil, err
	}
	return linesInRange(all, e, "the before-side of "+e.File)
}

// read returns a file's lines as a git object holds them, reading each one once.
func (r *Resolver) read(ref blobRef, from func(root, rev, file string) ([]string, error)) ([]string, error) {
	if lines, ok := r.files[ref]; ok {
		return lines, nil
	}
	lines, err := from(ref.repository, ref.rev, ref.file)
	if err != nil {
		return nil, err
	}
	r.files[ref] = lines
	return lines, nil
}

// ChangedOnDisk reports whether a file has been edited since the snapshot was
// taken, by comparing the blob the snapshot holds with what git would store for
// the file now. The snapshot's side never changes, so only the file on disk is
// hashed each time. A file that can no longer be read, or that the snapshot does
// not hold, has changed.
func (r *Resolver) ChangedOnDisk(repository, snapshot, file string) bool {
	ref := blobRef{repository, snapshot, file}
	posted, ok := r.blobs[ref]
	if !ok {
		id, err := git.BlobID(repository, snapshot, file)
		if err != nil {
			return true
		}
		posted = id
		r.blobs[ref] = id
	}
	current, err := git.HashFile(repository, file)
	return err != nil || current != posted
}

// linesInRange takes an Excerpt's range out of a file's lines, refusing one the
// file is too short to satisfy. what names the file in the refusal.
func linesInRange(all []string, e review.Excerpt, what string) ([]review.Line, error) {
	if e.LastLine > len(all) {
		return nil, fmt.Errorf("%s has %d lines, but the Excerpt asks for %d-%d", what, len(all), e.FirstLine, e.LastLine)
	}
	lines := make([]review.Line, 0, e.LastLine-e.FirstLine+1)
	for n := e.FirstLine; n <= e.LastLine; n++ {
		lines = append(lines, review.Line{Number: n, Text: all[n-1]})
	}
	return lines, nil
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

// The resolver is both things the review core reads code through. Asserted here
// so a signature drift is a build failure, not warnings that silently stop.
var (
	_ review.Resolver  = (*Resolver)(nil)
	_ review.DiskWatch = (*Resolver)(nil)
)

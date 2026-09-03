// Package workingtree reads Excerpt content off disk. It is the file-reading
// half of the git adapter: the review core names ranges, this resolves them
// against what is actually in the working tree, so nothing the Reviewer sees
// was supplied by the agent.
package workingtree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/review"
)

type Resolver struct{}

func NewResolver() Resolver {
	return Resolver{}
}

// Resolve reads the named range from the working tree. It refuses rather than
// approximates: a range the file cannot satisfy is a problem to show the
// Reviewer, never a partial answer.
func (Resolver) Resolve(e review.Excerpt) ([]review.Line, error) {
	if e.Side == review.OldSide {
		// The old side lives in git object storage and needs the derived
		// change-set to know which revision "old" means. Until that lands,
		// refusing is honest and faking it from the working tree is not.
		return nil, fmt.Errorf("the old side of %s is not available yet; dbn currently reads only the working tree", e.File)
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

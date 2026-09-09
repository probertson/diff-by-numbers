package git

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// MergeBase returns the merge-base of a repository's range with HEAD — the "before"
// revision of the changes under review, the same baseline the Changed Lines are
// derived from. It is exported so a caller can resolve it once and cache it: the
// before-side is an immutable committed blob, so nothing about it changes while a
// Walkthrough is under review.
func MergeBase(root, rangeRef string) (string, error) {
	return mergeBase(root, rangeRef)
}

// ReadFileAt returns the full lines of a file as it stood at a revision, following
// a rename so a file edited and renamed is read from its old path. It refuses
// rather than approximates: a file the revision does not hold is an error, never a
// blank.
func ReadFileAt(root, rev, file string) ([]string, error) {
	path := file
	if !existsAtRev(root, rev, file) {
		if source, ok := renameSource(root, rev, file); ok {
			path = source
		}
	}
	content, err := runGit(root, "show", fmt.Sprintf("%s:%s", rev, path))
	if err != nil {
		return nil, fmt.Errorf("could not read the before-side of %s at %s: %w", file, shortRev(rev), err)
	}
	return splitGitLines(content), nil
}

// ReadBefore returns the lines [first, last] of a file's before-side — the file as
// it stood at the merge-base of the repository's range with HEAD. It reads from
// git's object store, not the working tree, and refuses a range the before-side
// cannot satisfy rather than answering partially.
func ReadBefore(root, rangeRef, file string, first, last int) ([]review.Line, error) {
	base, err := MergeBase(root, rangeRef)
	if err != nil {
		return nil, err
	}
	all, err := ReadFileAt(root, base, file)
	if err != nil {
		return nil, err
	}
	return sliceLines(all, file, first, last)
}

// sliceLines takes the [first, last] range out of a file's lines, refusing a range
// the file is too short to satisfy.
func sliceLines(all []string, file string, first, last int) ([]review.Line, error) {
	if last > len(all) {
		return nil, fmt.Errorf("the before-side of %s has %d lines, but the Excerpt asks for %d-%d", file, len(all), first, last)
	}
	lines := make([]review.Line, 0, last-first+1)
	for n := first; n <= last; n++ {
		lines = append(lines, review.Line{Number: n, Text: all[n-1]})
	}
	return lines, nil
}

// shortRev trims a revision to a readable prefix for an error message.
func shortRev(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
}

// existsAtRev reports whether a path exists in a revision's tree, so a renamed
// file's before-side is not looked for under its new name.
func existsAtRev(root, rev, file string) bool {
	cmd := exec.Command("git", "cat-file", "-e", fmt.Sprintf("%s:%s", rev, file))
	cmd.Dir = root
	return cmd.Run() == nil
}

// renameSource finds the old path of a file that git detected as renamed between
// the base and the working tree, so its before-side can be read from where it
// actually lived. The diff is taken without a pathspec: filtering to the new path
// hides the deleted old path, and git needs to see both to pair them into a
// rename.
func renameSource(root, base, file string) (string, bool) {
	out, err := runGit(root, "-c", "core.quotePath=false", "diff", "--name-status", "-M", "--find-renames", base)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "R") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) >= 3 && fields[2] == file { // status, old path, new path
			return fields[1], true
		}
	}
	return "", false
}

// splitGitLines splits `git show` output into lines without inventing a final
// empty line for a trailing newline, matching how the working-tree resolver reads.
func splitGitLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// Package git derives the Changed Lines of a repository's range. It is the
// "what actually changed" half of the git adapter, kept apart from the core so
// the core runs no git of its own. It shells out to the git binary rather than
// taking a git library dependency.
package git

import (
	"bufio"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/review"
)

type Deriver struct{}

func NewDeriver() Deriver { return Deriver{} }

// Derive returns the Changed Lines of a repository, comparing the merge-base of
// its range with HEAD against the working tree. Using the merge-base — rather
// than the branch tip — means only what this branch changed since it diverged,
// including staged and unstaged work, and never what the other branch did in the
// meantime.
func (Deriver) Derive(repo review.Repository) (review.Derivation, error) {
	base, err := mergeBase(repo.Root, repo.Range)
	if err != nil {
		return review.Derivation{}, err
	}
	// --unified=0 emits no context lines, so every line inside a hunk is a
	// changed line and no reference lines are misread as changed. -M asks git to
	// detect renames so a pure rename surfaces as one Opaque Change rather than a
	// whole-file delete-and-add. core.quotePath=false stops git C-quoting any
	// non-ASCII path (its default), which would otherwise derive a mangled File
	// no agent-authored Excerpt or Acknowledgement could ever match — leaving that
	// file's coverage permanently unsatisfiable.
	out, err := runGit(repo.Root, "-c", "core.quotePath=false", "diff", "--unified=0", "--no-color", "-M", base, "--")
	if err != nil {
		return review.Derivation{}, err
	}
	return parseUnifiedZero(repo.Root, out)
}

func mergeBase(root, rangeRef string) (string, error) {
	if strings.TrimSpace(rangeRef) == "" {
		return "", fmt.Errorf("no range given for %s", root)
	}
	out, err := runGit(root, "merge-base", rangeRef, "HEAD")
	if err != nil {
		return "", fmt.Errorf("could not find the merge-base of %q and HEAD in %s: %w", rangeRef, root, err)
	}
	return strings.TrimSpace(out), nil
}

func runGit(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// block accumulates the header facts of one file's diff, so that when the file
// ends we can tell whether it changed by lines (it had hunks) or is an Opaque
// Change with none (a binary file, a mode change, a pure rename).
type block struct {
	oldPath, newPath string
	hadHunk          bool
	binary           bool
	rename           bool
	oldMode, newMode bool
	newModeValue     string
}

// opaque reports the Opaque Change a hunk-less block represents, if any. A block
// that had hunks is line-represented and never opaque; an empty new file is
// neither and yields nothing.
func (b *block) opaque(root string) (review.OpaqueChange, bool) {
	path := b.newPath
	if path == "" {
		path = b.oldPath
	}
	// Conditions can co-occur (a renamed binary, a renamed mode change). The Kind
	// takes the most consequential present, but the Detail records them all so the
	// manifest never misleads the Reviewer about what happened to the file.
	var kind review.OpaqueKind
	var details []string
	if b.binary {
		kind = review.OpaqueBinary
		details = append(details, "binary file")
	}
	if b.rename {
		if kind == "" {
			kind = review.OpaqueRename
		}
		details = append(details, fmt.Sprintf("renamed from %s", b.oldPath))
	}
	if b.oldMode && b.newMode {
		if kind == "" {
			kind = review.OpaqueMode
		}
		details = append(details, fmt.Sprintf("mode %s", b.newModeValue))
	}
	if kind == "" {
		return review.OpaqueChange{}, false
	}
	return review.OpaqueChange{Repository: root, File: path, Kind: kind, Detail: strings.Join(details, ", ")}, true
}

// parseUnifiedZero reads `git diff --unified=0 -M` output. With zero context the
// @@ ranges are exactly the Changed Lines; the header lines of a hunk-less file
// tell it apart as an Opaque Change.
func parseUnifiedZero(root, diff string) (review.Derivation, error) {
	var (
		result review.Derivation
		file   string // current file for line attribution, from the +++/--- headers
		blk    *block
	)

	flush := func() {
		if blk == nil {
			return
		}
		if !blk.hadHunk {
			if opaque, ok := blk.opaque(root); ok {
				result.Opaque = append(result.Opaque, opaque)
			}
		}
		blk = nil
	}

	scanner := bufio.NewScanner(strings.NewReader(diff))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			blk = &block{}
			blk.oldPath, blk.newPath = parseDiffGitPaths(line)
			file = ""
		case strings.HasPrefix(line, "old mode "):
			blk.oldMode = true
		case strings.HasPrefix(line, "new mode "):
			blk.newMode = true
			blk.newModeValue = strings.TrimSpace(line[len("new mode "):])
		case strings.HasPrefix(line, "rename from "):
			blk.rename = true
		case strings.HasPrefix(line, "rename to "):
			blk.rename = true
			blk.newPath = strings.TrimSpace(line[len("rename to "):])
		case strings.HasPrefix(line, "Binary files "):
			blk.binary = true
		case strings.HasPrefix(line, "+++ "):
			// A whole-file deletion has "+++ /dev/null" (an empty path); it must not
			// overwrite the old-side name set from "--- a/<file>", or the deletion's
			// old lines lose their file.
			if name := pathFromHeader(line); name != "" {
				file = name
			}
		case strings.HasPrefix(line, "--- ") && file == "":
			// A deletion has +++ /dev/null; take the old-side name so a deleted
			// file's old lines are still attributed to a file.
			file = pathFromHeader(line)
		case strings.HasPrefix(line, "@@"):
			oldStart, oldCount, newStart, newCount, ok := parseHunkHeader(line)
			if !ok {
				return review.Derivation{}, fmt.Errorf("could not parse hunk header %q", line)
			}
			if file == "" {
				return review.Derivation{}, fmt.Errorf("hunk header before any file header: %q", line)
			}
			if blk != nil {
				blk.hadHunk = true
			}
			for n := oldStart; n < oldStart+oldCount; n++ {
				result.Lines = append(result.Lines, review.ChangedLine{Repository: root, File: file, Side: review.OldSide, Line: n})
			}
			for n := newStart; n < newStart+newCount; n++ {
				result.Lines = append(result.Lines, review.ChangedLine{Repository: root, File: file, Side: review.NewSide, Line: n})
			}
			// Each hunk is one edit: pair the lines it removed with the lines that
			// replaced them, so showing the after-side can account for and render the
			// before-side (the "point once" behaviour). A count of zero on a side
			// leaves that side empty — a pure addition or a pure deletion.
			corr := review.Correspondence{Repository: root, File: file}
			if oldCount > 0 {
				corr.OldFirst, corr.OldLast = oldStart, oldStart+oldCount-1
			}
			if newCount > 0 {
				corr.NewFirst, corr.NewLast = newStart, newStart+newCount-1
			}
			result.Correspondences = append(result.Correspondences, corr)
		}
	}
	flush()
	return result, scanner.Err()
}

// parseDiffGitPaths reads the old and new path from a "diff --git a/OLD b/NEW"
// header. Derive runs git with core.quotePath=false, so non-ASCII paths arrive
// literally; the one remaining ambiguity is a path containing the literal " b/"
// sequence, which is rare enough to accept.
func parseDiffGitPaths(header string) (oldPath, newPath string) {
	rest := strings.TrimPrefix(header, "diff --git ")
	marker := strings.Index(rest, " b/")
	if marker < 0 {
		return "", ""
	}
	oldPath = strings.TrimPrefix(rest[:marker], "a/")
	newPath = rest[marker+len(" b/"):]
	return oldPath, newPath
}

// pathFromHeader turns "+++ b/src/app.ts" or "--- a/src/app.ts" into "src/app.ts",
// and "/dev/null" into "".
func pathFromHeader(header string) string {
	path := strings.TrimSpace(header[4:])
	if path == "/dev/null" {
		return ""
	}
	if len(path) > 2 && (path[1] == '/') && (path[0] == 'a' || path[0] == 'b') {
		return path[2:]
	}
	return path
}

// parseHunkHeader reads "@@ -oldStart,oldCount +newStart,newCount @@ ...".
// A missing count means 1.
func parseHunkHeader(header string) (oldStart, oldCount, newStart, newCount int, ok bool) {
	fields := strings.Fields(header)
	if len(fields) < 3 || fields[0] != "@@" {
		return 0, 0, 0, 0, false
	}
	oldStart, oldCount, ok = parseRange(fields[1], '-')
	if !ok {
		return 0, 0, 0, 0, false
	}
	newStart, newCount, ok = parseRange(fields[2], '+')
	if !ok {
		return 0, 0, 0, 0, false
	}
	return oldStart, oldCount, newStart, newCount, true
}

func parseRange(field string, sign byte) (start, count int, ok bool) {
	if len(field) == 0 || field[0] != sign {
		return 0, 0, false
	}
	body := field[1:]
	count = 1
	if comma := strings.IndexByte(body, ','); comma >= 0 {
		c, err := strconv.Atoi(body[comma+1:])
		if err != nil {
			return 0, 0, false
		}
		count = c
		body = body[:comma]
	}
	s, err := strconv.Atoi(body)
	if err != nil {
		return 0, 0, false
	}
	return s, count, true
}

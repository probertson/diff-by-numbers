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
func (Deriver) Derive(repo review.Repository) ([]review.ChangedLine, error) {
	base, err := mergeBase(repo.Root, repo.Range)
	if err != nil {
		return nil, err
	}
	// --unified=0 emits no context lines, so every line inside a hunk is a
	// changed line and no reference lines are misread as changed.
	out, err := runGit(repo.Root, "diff", "--unified=0", "--no-color", base, "--")
	if err != nil {
		return nil, err
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

// parseUnifiedZero reads `git diff --unified=0` output. It cares only about the
// file headers and the hunk headers: with zero context, the @@ ranges are
// exactly the changed lines.
func parseUnifiedZero(root, diff string) ([]review.ChangedLine, error) {
	var (
		lines []review.ChangedLine
		file  string
	)
	scanner := bufio.NewScanner(strings.NewReader(diff))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "+++ "):
			file = pathFromHeader(line)
		case strings.HasPrefix(line, "--- ") && file == "":
			// A deletion has +++ /dev/null; fall back to the old-side name so a
			// deleted file's old lines are still attributed to a file.
			file = pathFromHeader(line)
		case strings.HasPrefix(line, "@@"):
			oldStart, oldCount, newStart, newCount, ok := parseHunkHeader(line)
			if !ok {
				return nil, fmt.Errorf("could not parse hunk header %q", line)
			}
			name := file
			if strings.HasPrefix(line, "@@") && name == "" {
				return nil, fmt.Errorf("hunk header before any file header: %q", line)
			}
			for n := oldStart; n < oldStart+oldCount; n++ {
				lines = append(lines, review.ChangedLine{Repository: root, File: name, Side: review.OldSide, Line: n})
			}
			for n := newStart; n < newStart+newCount; n++ {
				lines = append(lines, review.ChangedLine{Repository: root, File: name, Side: review.NewSide, Line: n})
			}
		case strings.HasPrefix(line, "diff --git "):
			file = "" // reset for the next file
		}
	}
	return lines, scanner.Err()
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

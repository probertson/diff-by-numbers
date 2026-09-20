package git

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// Mapping says, for each line of a newer tree, where that line stood in an
// older one — or that it has no counterpart because it was touched in between.
//
// This is the capability a Revision Round is scoped with, and the one #44 needs
// to shade a Step by what moved since the last round rather than since the
// merge-base. It carries no policy: it reports what git says moved, and leaves
// the core to decide what that means for coverage.
type Mapping struct {
	// files is keyed by the path in the newer tree.
	files map[string]fileMapping
}

// fileMapping is one file's story between the two trees.
type fileMapping struct {
	// oldPath is where the file lived in the older tree, which differs from its
	// current path when git detected a rename.
	oldPath string
	// edits are the diff's hunks, in ascending order of newFirst.
	edits []hunkSpan
}

// hunkSpan is one hunk reduced to what a lookup needs: the span of newer-tree
// lines it introduced, and how much it shifted everything after it.
type hunkSpan struct {
	newFirst, newCount int
	oldFirst, oldCount int
}

// entirelyAbove reports whether this hunk sits wholly above a line, and so
// shifted it.
//
// The two cases differ by one, and getting it wrong is not a rounding error. A
// hunk with new-side lines occupies [newFirst, newFirst+newCount-1], so it is
// above a line once that span has ended. A pure deletion has no new-side lines
// at all, and git reports newFirst as the surviving line *above* the gap — so
// that line did not move, and only lines strictly below it did. Shifting it
// anyway maps it onto one of the lines that was deleted, which is exactly the
// sort of line the previous round showed: a line the Reviewer never saw would
// then be pre-marked as already read.
func (h hunkSpan) entirelyAbove(line int) bool {
	if h.newCount == 0 {
		return h.newFirst < line
	}
	return h.newFirst+h.newCount <= line
}

// Lookup returns where a line of the newer tree stood in the older one.
//
// ok is false when the line has no counterpart: it falls inside a hunk, so it
// was added or edited between the two trees, or its file did not exist in the
// older tree at all. Those are the lines a Revision Round must demand, because
// nothing about them has been reviewed yet.
func (m Mapping) Lookup(file string, line int) (review.Position, bool) {
	mapped, ok := m.files[file]
	if !ok {
		// git reported no difference for this file, so it stands exactly where it
		// stood. Untouched files are the overwhelming majority and never appear
		// in the diff at all.
		return review.Position{File: file, Line: line}, true
	}

	shift := 0
	for _, edit := range mapped.edits {
		if edit.newCount > 0 && line >= edit.newFirst && line < edit.newFirst+edit.newCount {
			return review.Position{}, false // inside the hunk: touched
		}
		if edit.entirelyAbove(line) {
			shift += edit.newCount - edit.oldCount
		}
	}
	return review.Position{File: mapped.oldPath, Line: line - shift}, true
}

// Touched reports whether git saw any difference in this file between the two
// trees. Absence from the diff means identical path, blob and mode.
func (m Mapping) Touched(file string) bool {
	_, differs := m.files[file]
	return differs
}

// PathIn returns the file's path in the earlier tree, following a rename.
func (m Mapping) PathIn(file string) string {
	mapped, ok := m.files[file]
	if !ok || mapped.oldPath == "" {
		return file
	}
	return mapped.oldPath
}

// MapBetween asks git how two trees differ and builds the line mapping from it.
//
// --unified=0 so every line inside a hunk is one git actually touched, and -M so
// a file moved between rounds maps through the rename instead of reading as a
// whole file withdrawn and another invented.
func (Deriver) MapBetween(root, from, to string) (review.RoundMapping, error) {
	out, err := runGit(root, "-c", "core.quotePath=false", "diff", "--unified=0", "--no-color", "-M", from, to)
	if err != nil {
		return Mapping{}, fmt.Errorf("could not compare %s with %s in %s: %w", shortRev(from), shortRev(to), root, err)
	}
	return parseMapping(out)
}

func parseMapping(diff string) (Mapping, error) {
	m := Mapping{files: map[string]fileMapping{}}

	var path string
	var current fileMapping
	flush := func() {
		if path == "" {
			return
		}
		// A binary or mode-only change carries no rename lines, so nothing has
		// named an old path: it is the same path. Without this the lookup would
		// answer with an empty file name, which matches nothing.
		if current.oldPath == "" {
			current.oldPath = path
		}
		m.files[path] = current
	}

	scanner := bufio.NewScanner(strings.NewReader(diff))
	scanner.Buffer(make([]byte, 0, 64*1024), maxDiffLine)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			path, current = "", fileMapping{}
			// A change with no hunks and no rename — a binary file, or a mode
			// change — carries neither a +++ line nor a "rename to", so this
			// header is the only place its path appears. Those files have no
			// lines to map, but an Opaque Change still has to be recognised as
			// touched, or it would be pre-marked as already read.
			_, path = parseDiffGitPaths(line)

		case strings.HasPrefix(line, "rename from "):
			current.oldPath = strings.TrimPrefix(line, "rename from ")

		case strings.HasPrefix(line, "rename to "):
			// A rename with no edits carries no ---/+++ lines and no hunks at all,
			// so this is the only place its new path appears. Without it the file
			// would never enter the mapping and its lines would answer with their
			// current path, losing the rename.
			path = strings.TrimPrefix(line, "rename to ")

		case strings.HasPrefix(line, "+++ "):
			// /dev/null on the new side means the file was deleted, and trims to
			// the empty string. Keep the header's path rather than losing the file.
			if named := trimDiffPath(strings.TrimPrefix(line, "+++ ")); named != "" {
				path = named
			}

		case strings.HasPrefix(line, "@@ "):
			oldFirst, oldCount, newFirst, newCount, ok := parseHunkHeader(line)
			if !ok {
				continue
			}
			current.edits = append(current.edits, hunkSpan{
				newFirst: newFirst, newCount: newCount,
				oldFirst: oldFirst, oldCount: oldCount,
			})
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return Mapping{}, fmt.Errorf("could not read the diff between trees: %w", err)
	}
	return m, nil
}

// maxDiffLine bounds one line of diff output, which a minified file can make
// very long. Generous enough that no real source line reaches it.
const maxDiffLine = 4 << 20

// trimDiffPath strips the a/ or b/ prefix git puts on diff paths, and handles
// /dev/null for a file that does not exist on that side.
func trimDiffPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "/dev/null" {
		return ""
	}
	if len(raw) > 2 && (raw[:2] == "a/" || raw[:2] == "b/") {
		return raw[2:]
	}
	return raw
}

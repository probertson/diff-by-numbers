package review

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// rangeCap bounds how many entries an uncovered-changes message lists.
//
// The point of the message is to name everything, so an agent fixes it all in
// one go instead of posting again to discover the rest. This is a safety valve
// for a pathological Change Set — thousands of scattered single lines — not a
// budget: collapsing consecutive lines means a real branch is far under it.
const rangeCap = 100

// uncoveredFile is everything unaccounted for in one file of one repository.
type uncoveredFile struct {
	repository string
	file       string
	// bySide holds the line numbers, kept apart because the two sides are
	// different places: old-side numbers are positions in the merge-base and
	// new-side ones in the working tree, so a reader who conflated them would
	// look in the wrong file for half the list.
	bySide map[Side][]int
	// opaque are the Opaque Changes on this file, which have no lines at all.
	opaque []OpaqueChange
}

// summarizeUncovered names every change nothing accounts for, grouped by file
// and side, with consecutive lines collapsed into ranges:
//
//	no Excerpt or Acknowledgement accounts for 264 changed lines in 7 files:
//	  CLAUDE.md       new 16-18, 40
//	  CONTEXT.md      old 125-131
//	  README.md       old 11, 191-240; new 12-60
//
// Files are keyed by repository as well as name (ADR-0009): a Change Set may
// span several, and two same-named files merged into one row would interleave
// line numbers that belong to different trees.
func summarizeUncovered(lines []ChangedLine, opaque []OpaqueChange) string {
	files := map[fileRef]*uncoveredFile{}
	at := func(repository, file string) *uncoveredFile {
		ref := fileRef{repository, file}
		if files[ref] == nil {
			files[ref] = &uncoveredFile{repository: repository, file: file, bySide: map[Side][]int{}}
		}
		return files[ref]
	}
	for _, line := range lines {
		entry := at(line.Repository, line.File)
		entry.bySide[line.Side] = append(entry.bySide[line.Side], line.Line)
	}
	for _, change := range opaque {
		entry := at(change.Repository, change.File)
		entry.opaque = append(entry.opaque, change)
	}

	ordered := make([]*uncoveredFile, 0, len(files))
	for _, entry := range files {
		ordered = append(ordered, entry)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].repository != ordered[j].repository {
			return ordered[i].repository < ordered[j].repository
		}
		return ordered[i].file < ordered[j].file
	})

	// The repository is shown only when there is more than one, so the common
	// single-repository case stays as short as the paths the agent sent.
	qualify := spansRepositories(ordered)
	labels := make([]string, len(ordered))
	for i, entry := range ordered {
		labels[i] = entry.file
		if qualify {
			labels[i] = path.Join(entry.repository, entry.file)
		}
	}

	var rows []string
	listed, dropped, truncated := 0, 0, 0
	for i, entry := range ordered {
		parts, left := entry.describe(rangeCap - listed)
		listed += entry.entries() - left
		if left > 0 {
			dropped += left
			truncated++
		}
		if len(parts) == 0 {
			continue // wholly truncated: counted above, but nothing to show
		}
		rows = append(rows, fmt.Sprintf("  %-*s  %s", width(labels), labels[i], strings.Join(parts, "; ")))
	}

	var out strings.Builder
	fmt.Fprintf(&out, "no Excerpt or Acknowledgement accounts for %s in %s:",
		countOf(len(lines), len(opaque)), pluralize(len(files), "file"))
	for _, row := range rows {
		out.WriteString("\n" + row)
	}
	if dropped > 0 {
		fmt.Fprintf(&out, "\n  … and %s in %s",
			pluralize(dropped, "more range"), pluralize(truncated, "file"))
	}
	return out.String()
}

// countOf says how much is unaccounted for, naming Opaque Changes separately
// because they have no lines and are fixed by an Acknowledgement, not an
// Excerpt. Without this a branch whose only uncovered atom is a binary read
// "0 changed lines".
func countOf(lines, opaque int) string {
	switch {
	case opaque == 0:
		return pluralize(lines, "changed line")
	case lines == 0:
		return pluralize(opaque, "Opaque Change")
	default:
		return pluralize(lines, "changed line") + " and " + pluralize(opaque, "Opaque Change")
	}
}

// entries is how many things this file would list in full.
func (u *uncoveredFile) entries() int {
	total := len(u.opaque)
	for _, numbers := range u.bySide {
		total += len(collapse(numbers))
	}
	return total
}

// describe renders this file's entries, listing at most room of them, and
// returns how many it had to leave out.
func (u *uncoveredFile) describe(room int) (parts []string, left int) {
	if room < 0 {
		room = 0
	}
	for _, side := range []Side{OldSide, NewSide} {
		numbers := u.bySide[side]
		if len(numbers) == 0 {
			continue
		}
		spans := collapse(numbers)
		if len(spans) > room {
			left += len(spans) - room
			spans = spans[:room]
		}
		room -= len(spans)
		if len(spans) > 0 {
			parts = append(parts, fmt.Sprintf("%s %s", side, strings.Join(spans, ", ")))
		}
	}
	for _, change := range u.opaque {
		if room == 0 {
			left++
			continue
		}
		room--
		parts = append(parts, string(change.Kind))
	}
	return parts, left
}

// spansRepositories reports whether the uncovered files come from more than one.
func spansRepositories(files []*uncoveredFile) bool {
	for _, entry := range files {
		if entry.repository != files[0].repository {
			return true
		}
	}
	return false
}

// width is how wide the label column should be, so the entries line up and the
// eye can run down them. Capped, so one very long path does not indent the rest.
func width(labels []string) int {
	const max = 40
	widest := 0
	for _, label := range labels {
		if len(label) > widest && len(label) <= max {
			widest = len(label)
		}
	}
	return widest
}

// collapse turns a list of line numbers into the fewest ranges that cover them:
// consecutive numbers become "16-18", a lone one stays "40". Duplicates are
// folded in, which a repository-qualified ledger should never produce but which
// would otherwise render as a zero-length range.
func collapse(numbers []int) []string {
	sort.Ints(numbers)

	var spans []string
	for i := 0; i < len(numbers); {
		first := numbers[i]
		last := first
		for i++; i < len(numbers) && (numbers[i] == last || numbers[i] == last+1); i++ {
			last = numbers[i]
		}
		if first == last {
			spans = append(spans, fmt.Sprintf("%d", first))
			continue
		}
		spans = append(spans, fmt.Sprintf("%d-%d", first, last))
	}
	return spans
}

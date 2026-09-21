package review

import (
	"fmt"

	"github.com/probertson/diff-by-numbers/internal/intraline"
)

// changesWithin is what changed inside one edit, by line number on each side:
// for each removed line matched with the added line that took its place, and
// for that added line, the runes that differ (#81). A line with no match has no
// entry.
type changesWithin struct {
	removed, added map[int][]intraline.Range
}

// changesWithinEdit reads one edit's removed and added lines whole and matches
// them, so every row of the edit is marked against the line it really took the
// place of — whichever Step or range each row happens to be drawn in. Lines are
// never matched across edits.
//
// It is remembered for the round, under key: the round's content cannot change
// while it is on screen, and the view is drawn every second.
func (s *Session) changesWithinEdit(key string, removed, added Excerpt, removedFrom, addedFrom Round) changesWithin {
	if found, ok := s.editChanges[key]; ok {
		return found
	}
	var found changesWithin
	removedLines, removedErr := s.resolver.Resolve(removed, removedFrom)
	addedLines, addedErr := s.resolver.Resolve(added, addedFrom)
	if removedErr == nil && addedErr == nil {
		removedRanges, addedRanges := intraline.Emphasis(textsOf(removedLines), textsOf(addedLines))
		found = changesWithin{removed: byNumber(removedLines, removedRanges), added: byNumber(addedLines, addedRanges)}
	}
	// A Session that has accepted nothing has no memory to keep this in; it
	// is then worked out afresh, which is only ever a cost, never a wrong answer.
	if s.editChanges != nil {
		s.editChanges[key] = found
	}
	return found
}

// modificationChanges is what changed inside a merge-base modification: its
// before-side from the merge-base against its after-side from the round.
func (s *Session) modificationChanges(c Correspondence) changesWithin {
	key := fmt.Sprintf("base\x00%s\x00%s\x00%d-%d\x00%d-%d", c.Repository, c.File, c.OldFirst, c.OldLast, c.NewFirst, c.NewLast)
	return s.changesWithinEdit(key,
		Excerpt{Repository: c.Repository, File: c.File, Side: OldSide, FirstLine: c.OldFirst, LastLine: c.OldLast},
		Excerpt{Repository: c.Repository, File: c.File, Side: NewSide, FirstLine: c.NewFirst, LastLine: c.NewLast},
		s.latest.Round, s.latest.Round)
}

// roundEditChanges is what changed inside an edit made since the previous round:
// the previous round's lines, under the file's name then, against this round's.
func (s *Session) roundEditChanges(repository, file, earlierPath string, edit RoundEdit) changesWithin {
	if edit.OldCount == 0 || edit.NewCount == 0 {
		return changesWithin{}
	}
	key := fmt.Sprintf("round\x00%s\x00%s\x00%d+%d\x00%d+%d", repository, file, edit.OldFirst, edit.OldCount, edit.NewFirst, edit.NewCount)
	return s.changesWithinEdit(key,
		Excerpt{Repository: repository, File: earlierPath, Side: NewSide, FirstLine: edit.OldFirst, LastLine: edit.OldFirst + edit.OldCount - 1},
		Excerpt{Repository: repository, File: file, Side: NewSide, FirstLine: edit.NewFirst, LastLine: edit.NewFirst + edit.NewCount - 1},
		s.previous.round, s.latest.Round)
}

func textsOf(lines []Line) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = line.Text
	}
	return out
}

func byNumber(lines []Line, ranges [][]intraline.Range) map[int][]intraline.Range {
	out := map[int][]intraline.Range{}
	for i, line := range lines {
		if ranges[i] != nil {
			out[line.Number] = ranges[i]
		}
	}
	return out
}

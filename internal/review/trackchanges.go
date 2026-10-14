package review

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// WordChange is what an edit did to a run of a Document's text.
type WordChange string

const (
	WordsKept    WordChange = "kept"
	WordsRemoved WordChange = "removed"
	WordsAdded   WordChange = "added"
)

// WordRun is a run of an edited block's text that the edit treated alike: text
// it kept, words it struck, or text it added. A row of an edited block reads as
// its runs in order: the row's own text, cut into kept and added runs, with the
// words the edit struck there put back between them, so the Reviewer reads the
// edit in place, as track changes. Kept and added runs are the row's text
// exactly, spacing included; a removed run is the struck words, a space
// between each.
type WordRun struct {
	Text   string
	Change WordChange
}

// trackedDocument is a Document's changes as its formatted view shows them: the
// word diff of its two sides, and the merged runs each changed line of its
// after-side reads as. It is worked out once per comparison and remembered for
// the round, since the view is drawn every second.
type trackedDocument struct {
	documentDiff
	// afterBlocks are each group's after-side blocks, as indexes into
	// after.blocks, in file order; hasBefore says a group has a before-side block.
	afterBlocks map[int][]int
	hasBefore   map[int]bool
	// runs are the merged runs of each line of an edited block, by after-side
	// line.
	runs map[int][]WordRun
	// added and struck are what runs were merged from, by after-side line: the
	// byte ranges of added words, and the struck words put back on it.
	added  map[int][][2]int
	struck map[int][]struckAt
	// later are struck words put on an earlier line that go on this one
	// instead, by after-side line, when an Excerpt shows that earlier line only
	// as context, or not at all, so it carries no runs.
	later map[int][]movedStruck
}

// afterBlocksOf is the after-side blocks in a group, in file order.
func (t trackedDocument) afterBlocksOf(group int) []int { return t.afterBlocks[group] }

// afterTreatment is how an after-side block was changed: not at all (context);
// made from nothing (new); or made from text it replaced (edited), which takes in
// a rewrap whose words did not change, since its lines did.
func (t trackedDocument) afterTreatment(n int) Treatment {
	group := t.group[len(t.before.blocks)+n]
	switch {
	case !t.linesChanged[group] && !t.wordsChanged[group]:
		return TreatmentContext
	case !t.hasBefore[group]:
		return TreatmentNew
	default:
		return TreatmentEdited
	}
}

// track works out the merged runs of every line of every edited block.
//
// A struck word is put back beside the text it stood with: just before the next
// word the edit kept or added in its group, where that is on the same line as
// the word before it, so a struck word reads before the word that replaced it;
// otherwise just after the word before it, so a word struck at the end of a line
// stays on that line, unless an Excerpt shows that line only as context, or not at all (runsOn).
// A group with no after-side is removed whole and drawn on
// its own, so its words are put nowhere.
func track(d documentDiff) trackedDocument {
	t := trackedDocument{documentDiff: d, afterBlocks: map[int][]int{}, hasBefore: map[int]bool{}, runs: map[int][]WordRun{},
		added: map[int][][2]int{}, struck: map[int][]struckAt{}, later: map[int][]movedStruck{}}
	for n := range d.before.blocks {
		t.hasBefore[d.group[n]] = true
	}
	for n := range d.after.blocks {
		group := d.group[len(d.before.blocks)+n]
		t.afterBlocks[group] = append(t.afterBlocks[group], n)
	}
	inserted, added := t.struck, t.added
	put := func(line, at int, word string) {
		list := inserted[line]
		if n := len(list); n > 0 && list[n-1].at == at {
			list[n-1].text += " " + word
			return
		}
		inserted[line] = append(list, struckAt{at: at, text: word})
	}
	// putLater notes where a word put at the end of line from goes instead.
	putLater := func(from int, line, at int, word string) {
		list := t.later[line]
		if n := len(list); n > 0 && list[n-1].from == from && list[n-1].at == at {
			list[n-1].text += " " + word
			return
		}
		t.later[line] = append(list, movedStruck{from: from, struckAt: struckAt{at: at, text: word}})
	}

	prev := make([]*docWord, len(d.steps))
	var last *docWord
	for i, step := range d.steps {
		prev[i] = last
		if step.after != nil {
			last = step.after
		}
	}
	next := make([]*docWord, len(d.steps))
	last = nil
	for i := len(d.steps) - 1; i >= 0; i-- {
		next[i] = last
		if d.steps[i].after != nil {
			last = d.steps[i].after
		}
	}

	for i, step := range d.steps {
		switch {
		case step.before == nil:
			added[step.after.line] = append(added[step.after.line], [2]int{step.after.start, step.after.end})
		case step.after == nil:
			group := d.group[step.before.node]
			inGroup := func(w *docWord) bool { return w != nil && d.group[w.node] == group }
			p, n := prev[i], next[i]
			switch {
			case inGroup(n) && (!inGroup(p) || p.line == n.line):
				put(n.line, n.start, step.before.text)
			case inGroup(p):
				put(p.line, p.end, step.before.text)
				if inGroup(n) {
					putLater(p.line, n.line, n.start, step.before.text)
				}
			case len(t.afterBlocksOf(group)) > 0:
				// Nothing kept or added in its group stands near it: it is put
				// at the end of the group's first after-side block.
				b := d.after.blocks[t.afterBlocksOf(group)[0]]
				put(b.lines.Last, len(d.after.lines[b.lines.Last-1]), step.before.text)
			}
		}
	}

	for n, b := range d.after.blocks {
		if t.afterTreatment(n) != TreatmentEdited {
			continue
		}
		for line := b.lines.First; line <= b.lines.Last; line++ {
			t.runs[line] = mergedRuns(d.after.lines[line-1], slices.Clone(added[line]), slices.Clone(inserted[line]))
		}
	}
	return t
}

// runsOn is the merged runs of a line of an edited block, in an Excerpt in
// which unshown reports the lines that carry no runs: the ones it is only
// widened by, as context, and the ones it does not show. A word struck at the
// end of such a line is put at the start of the next line instead, so it is
// never lost.
func (t trackedDocument) runsOn(line int, unshown func(line int) bool) []WordRun {
	var moved []struckAt
	for _, m := range t.later[line] {
		if unshown(m.from) {
			moved = append(moved, m.struckAt)
		}
	}
	if len(moved) == 0 {
		return t.runs[line]
	}
	return mergedRuns(t.after.lines[line-1], slices.Clone(t.added[line]), append(moved, t.struck[line]...))
}

// struckAt is struck words to put back at a byte offset of a line.
type struckAt struct {
	at   int
	text string
}

// movedStruck is struck words put at the end of line from, and where they go
// on another line when from is shown only as context.
type movedStruck struct {
	from int
	struckAt
}

// mergedRuns cuts one line into kept and added runs, the whitespace between two
// added words added too, and puts the struck words back where they go.
func mergedRuns(text string, added [][2]int, struck []struckAt) []WordRun {
	isAdded := make([]bool, len(text))
	for _, r := range added {
		for i := r[0]; i < r[1]; i++ {
			isAdded[i] = true
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i][0] < added[j][0] })
	for k := 1; k < len(added); k++ {
		gap := text[added[k-1][1]:added[k][0]]
		if strings.TrimFunc(gap, unicode.IsSpace) == "" {
			for i := added[k-1][1]; i < added[k][0]; i++ {
				isAdded[i] = true
			}
		}
	}
	sort.SliceStable(struck, func(i, j int) bool { return struck[i].at < struck[j].at })

	var out []WordRun
	emit := func(run WordRun) {
		if run.Text == "" {
			return
		}
		if n := len(out); n > 0 && out[n-1].Change == run.Change && run.Change != WordsRemoved {
			out[n-1].Text += run.Text
			return
		}
		out = append(out, run)
	}
	change := func(i int) WordChange {
		if isAdded[i] {
			return WordsAdded
		}
		return WordsKept
	}
	from := 0
	cutAt := func(i int) {
		if i > from {
			emit(WordRun{Text: text[from:i], Change: change(from)})
			from = i
		}
	}
	k := 0
	for i := range len(text) {
		if from < i && change(i) != change(from) {
			cutAt(i)
		}
		for ; k < len(struck) && struck[k].at == i; k++ {
			cutAt(i)
			emit(WordRun{Text: struck[k].text, Change: WordsRemoved})
		}
	}
	cutAt(len(text))
	for ; k < len(struck); k++ {
		emit(WordRun{Text: struck[k].text, Change: WordsRemoved})
	}
	return out
}

// trackedBase is a Document's changes since the merge-base, as the round on
// screen holds it.
func (s *Session) trackedBase(ref fileRef) trackedDocument {
	return s.trackedOnce(fmt.Sprintf("base\x00%s\x00%s", ref.repository, ref.file), func() trackedDocument {
		return track(s.ledger.baseDiff(ref))
	})
}

// trackedSincePrevious is a Document's changes since the previous round: the
// text that round's snapshot held against the text this one holds, by what the
// comparison of the two says was touched, as code is shaded since then. A
// previous text that cannot be read, as of a file that round did not have,
// stands as nothing, so all of the Document's text reads as new.
func (s *Session) trackedSincePrevious(ref fileRef, mapping RoundMapping) trackedDocument {
	return s.trackedOnce(fmt.Sprintf("since\x00%s\x00%s", ref.repository, ref.file), func() trackedDocument {
		var before document
		if reader, ok := s.resolver.(FileReader); ok {
			if lines, err := reader.ReadFile(ref.repository, mapping.PathIn(ref.file), NewSide, s.previous.round); err == nil {
				before = document{lines: lines, blocks: blocksOf(ref.file, lines)}
			}
		}
		after := s.ledger.documents[documentSide{ref, NewSide}]
		changedBefore, changedAfter := map[int]bool{}, map[int]bool{}
		for _, edit := range mapping.Edits(ref.file) {
			for n := edit.OldFirst; n < edit.OldFirst+edit.OldCount; n++ {
				changedBefore[n] = true
			}
		}
		for n := 1; n <= len(after.lines); n++ {
			if _, untouched := mapping.Lookup(ref.file, n); !untouched {
				changedAfter[n] = true
			}
		}
		return track(diffDocument(before, after, changedBefore, changedAfter))
	})
}

// trackedOnce remembers a Document's changes for the round under key.
func (s *Session) trackedOnce(key string, work func() trackedDocument) trackedDocument {
	if found, ok := s.trackedDocuments[key]; ok {
		return found
	}
	found := work()
	// A Session that has accepted nothing has no memory to keep this in; it is
	// then worked out afresh, which is only ever a cost, never a wrong answer.
	if s.trackedDocuments != nil {
		s.trackedDocuments[key] = found
	}
	return found
}

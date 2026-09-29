// Package intraline finds the words that changed within one edit, so the
// Reviewer is pointed at a one-word edit in a long line rather than left to
// compare the two word by word (#81), and a rewrapped passage lights only what
// was rewritten (#125).
//
// It delegates the algorithm, not the rendering (ADR-0005): it answers with
// ranges of runes, and whatever draws the rows styles them. dbn owns every row it
// draws — interleaved before-sides, previous-round rows, wrapping — so styled
// text from an outside renderer could not be mapped back onto them.
package intraline

import (
	"strings"
	"unicode"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// Range is a half-open run of runes, [Start, End), within one line.
type Range struct{ Start, End int }

// An edit's changed words are lit only when its two sides are alike enough to
// be one passage edited rather than one passage replaced by another; a
// replacement is lit whole, since all of its text was removed or added. An edit
// is one passage edited when either side's words are mostly shared overall
// (similarityThreshold, delta's choice), or when most of the smaller side's words
// are found in the larger (containmentThreshold, with at least containmentMinimum
// of them), which keeps a small edit made beside many added lines from losing
// its emphasis (#125).
const (
	similarityThreshold  = 0.4
	containmentThreshold = 0.6
	containmentMinimum   = 4
)

// Each distinct token is diffed as one rune from Unicode's private-use planes
// 15 and 16, so no token can collide with a rune diff-match-patch treats
// specially. They hold encodable runes: far more distinct tokens than an edit
// of hand-written text has, but a generated file can have more, and past that
// the encoding cannot tell tokens apart, so such an edit gets no emphasis.
const (
	firstEncoded = rune(0xF0000)
	encodable    = 0x110000 - 0xF0000
)

// minimumMatch is the fewest unchanged words that keep two edits apart. A
// shorter unchanged run between them is emphasised as part of the edit, so a
// rewritten passage is lit as a few spans rather than a scatter of fragments
// around the words it happens to share.
const minimumMatch = 2

// Tokens splits a line into the units a change is measured in: runs of letters,
// digits and underscores (words, identifiers and numbers), runs of whitespace,
// and single characters of anything else. Diffing tokens rather than characters
// keeps a renamed identifier highlighted whole rather than as a scatter of
// letters it happens to share with its old name.
func Tokens(line string) []string {
	var out []string
	runes := []rune(line)
	for i := 0; i < len(runes); {
		j := i + 1
		switch {
		case isWord(runes[i]):
			for j < len(runes) && isWord(runes[j]) {
				j++
			}
		case unicode.IsSpace(runes[i]):
			for j < len(runes) && unicode.IsSpace(runes[j]) {
				j++
			}
		}
		out = append(out, string(runes[i:j]))
		i = j
	}
	return out
}

func isWord(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// Emphasis says, for each line of one edit, which of its runes changed. It diffs
// the removed lines against the added lines as one passage each, with every line
// break read as whitespace, and maps what changed back onto the lines (#125).
// There is no pairing of one line with another, so a lit run means "this text
// was removed" or "this text was added", and a word that only moved across a
// wrap is unchanged. The two lists must come from one edit: nothing is compared
// with a line from somewhere else in the file.
//
// Whitespace is never emphasised on its own: whitespace tokens all compare
// equal, and ranges are trimmed of it. When the two sides are too unalike to be
// one passage edited, or too large to diff faithfully, every line is lit whole:
// it was replaced, and a row with nothing lit must only ever mean its text is
// on the other side too. Every line gets a list, empty when nothing on it
// changed.
func Emphasis(removed, added []string) ([][]Range, [][]Range) {
	before := passageTokens(removed, carriedMarkers(added))
	after := passageTokens(added, carriedMarkers(removed))
	encoded := map[string]rune{}
	encode := func(tokens []token) []rune {
		out := make([]rune, len(tokens))
		for i, t := range tokens {
			key := t.text
			if t.isSpace() {
				key = " "
			}
			r, ok := encoded[key]
			if !ok {
				r = firstEncoded + rune(len(encoded))
				encoded[key] = r
			}
			out[i] = r
		}
		return out
	}
	beforeRunes, afterRunes := encode(before), encode(after)
	if len(encoded) > encodable {
		return litWhole(removed), litWhole(added)
	}
	diffs := diffmatchpatch.New().DiffMainRunes(beforeRunes, afterRunes, false)

	if !alike(before, after, diffs) {
		return litWhole(removed), litWhole(added)
	}
	diffs = absorbShortMatches(diffs, before, after)

	removedOut, addedOut := emptyRanges(len(removed)), emptyRanges(len(added))
	for _, s := range spans(diffs, before, after) {
		switch s.diff.Type {
		case diffmatchpatch.DiffDelete:
			mark(s.before, removedOut)
		case diffmatchpatch.DiffInsert:
			mark(s.after, addedOut)
		}
	}
	trimWhitespace(removed, removedOut)
	trimWhitespace(added, addedOut)
	return removedOut, addedOut
}

// token is one token of a passage and where it sits: runes [start, end) of line.
// A line break sits on no line; it stands for the whitespace around the break
// and whatever the next line's start folds into it.
type token struct {
	text       string
	line       int
	start, end int
}

// lineBreak is the line of a token that stands for a line break.
const lineBreak = -1

func (t token) isSpace() bool { return unicode.IsSpace([]rune(t.text)[0]) }
func (t token) isWord() bool  { return isWord([]rune(t.text)[0]) }

// commentMarkers are the line starts a rewrap repeats on every line: a comment's
// leader, a quote, a block comment's star.
var commentMarkers = []string{"//", "#", ">", "*"}

// passageTokens reads lines as one passage. Each line break is one whitespace
// token, joined with the whitespace either side of it, the next line's
// indentation, and a comment marker the next line shares with the line before
// it, when foldable says the other side carries that marker too. Those are what
// a rewrap moves around, so they never read as changed.
func passageTokens(lines []string, foldable map[string]bool) []token {
	var out []token
	for i, line := range lines {
		folded := 0
		if i > 0 {
			folded = foldedPrefix(lines[i-1], line, foldable)
		}
		at := 0
		for _, text := range Tokens(line) {
			n := len([]rune(text))
			if at >= folded {
				out = append(out, token{text: text, line: i, start: at, end: at + n})
			}
			at += n
		}
		if i == len(lines)-1 {
			break
		}
		if last := len(out) - 1; last >= 0 && out[last].isSpace() {
			out[last].line = lineBreak
		} else {
			out = append(out, token{text: " ", line: lineBreak})
		}
	}
	return out
}

// foldedPrefix is how many runes at the start of line the line break before it
// takes: its indentation, and a foldable comment marker it shares with prev,
// with the whitespace after the marker.
func foldedPrefix(prev, line string, foldable map[string]bool) int {
	runes := []rune(line)
	at := skipSpace(runes, 0)
	marker := markerOf(line)
	if marker == "" || marker != markerOf(prev) || !foldable[marker] {
		return at
	}
	return skipSpace(runes, at+len([]rune(marker)))
}

// carriedMarkers are the comment markers lines carry down two or more
// consecutive lines, as a comment or quote does. A marker is folded into the
// line breaks on one side only when the other side carries it too: removing
// "//" from lines, or a "*" dereference from one of two, is a real edit, and
// folding it on one side alone would hide it.
func carriedMarkers(lines []string) map[string]bool {
	out := map[string]bool{}
	for i := 1; i < len(lines); i++ {
		if marker := markerOf(lines[i]); marker != "" && marker == markerOf(lines[i-1]) {
			out[marker] = true
		}
	}
	return out
}

func markerOf(line string) string {
	rest := strings.TrimLeftFunc(line, unicode.IsSpace)
	for _, marker := range commentMarkers {
		if strings.HasPrefix(rest, marker) {
			return marker
		}
	}
	return ""
}

func skipSpace(runes []rune, at int) int {
	for at < len(runes) && unicode.IsSpace(runes[at]) {
		at++
	}
	return at
}

// alike says whether the two sides are one passage edited rather than one
// replaced by another. It counts words only: nearly every line of code shares
// its spaces, brackets and operators with every other. A passage with no words
// at all is measured by all its tokens but whitespace.
func alike(before, after []token, diffs []diffmatchpatch.Diff) bool {
	counted := func(t token) bool { return t.isWord() }
	if countOf(before, counted)+countOf(after, counted) == 0 {
		counted = func(t token) bool { return !t.isSpace() }
	}
	shared := 0
	for _, s := range spans(diffs, before, after) {
		if s.diff.Type == diffmatchpatch.DiffEqual {
			shared += countOf(s.before, counted)
		}
	}
	a, b := countOf(before, counted), countOf(after, counted)
	if a+b == 0 {
		return true
	}
	if float64(2*shared)/float64(a+b) >= similarityThreshold {
		return true
	}
	return shared >= containmentMinimum && float64(shared)/float64(min(a, b)) >= containmentThreshold
}

// span is one diff with the tokens it covers: on the before side for an
// unchanged run or a deletion, on the after side for an unchanged run or an
// insertion.
type span struct {
	diff          diffmatchpatch.Diff
	before, after []token
}

// spans pairs each diff with the tokens it covers, which is the one place the
// token positions on each side are counted.
func spans(diffs []diffmatchpatch.Diff, before, after []token) []span {
	out := make([]span, len(diffs))
	var i, j int
	for k, d := range diffs {
		n := len([]rune(d.Text))
		out[k].diff = d
		if d.Type != diffmatchpatch.DiffInsert {
			out[k].before = before[i : i+n]
			i += n
		}
		if d.Type != diffmatchpatch.DiffDelete {
			out[k].after = after[j : j+n]
			j += n
		}
	}
	return out
}

func countOf(tokens []token, counted func(token) bool) int {
	n := 0
	for _, t := range tokens {
		if counted(t) {
			n++
		}
	}
	return n
}

// absorbShortMatches makes an unchanged run of fewer than minimumMatch words
// between two edits part of the edit, then merges each run of edits into one
// deletion and one insertion. An unchanged run at either end of the passage is
// kept, since it lies between an edit and nothing.
func absorbShortMatches(diffs []diffmatchpatch.Diff, before, after []token) []diffmatchpatch.Diff {
	var out []diffmatchpatch.Diff
	var deleted, inserted strings.Builder
	flush := func() {
		if deleted.Len() > 0 {
			out = append(out, diffmatchpatch.Diff{Type: diffmatchpatch.DiffDelete, Text: deleted.String()})
		}
		if inserted.Len() > 0 {
			out = append(out, diffmatchpatch.Diff{Type: diffmatchpatch.DiffInsert, Text: inserted.String()})
		}
		deleted.Reset()
		inserted.Reset()
	}
	for k, s := range spans(diffs, before, after) {
		d := s.diff
		switch d.Type {
		case diffmatchpatch.DiffDelete:
			deleted.WriteString(d.Text)
		case diffmatchpatch.DiffInsert:
			inserted.WriteString(d.Text)
		case diffmatchpatch.DiffEqual:
			between := k > 0 && k < len(diffs)-1
			if between && countOf(s.before, token.isWord) < minimumMatch {
				deleted.WriteString(d.Text)
				inserted.WriteString(d.Text)
			} else {
				flush()
				out = append(out, d)
			}
		}
	}
	flush()
	return out
}

// litWhole lights each line from its first non-whitespace rune to its last:
// what an edit shows when it is not one passage edited but one replaced, since
// all of its text was removed or added.
func litWhole(lines []string) [][]Range {
	out := make([][]Range, len(lines))
	for i, line := range lines {
		out[i] = []Range{{0, len([]rune(line))}}
	}
	trimWhitespace(lines, out)
	return out
}

func emptyRanges(n int) [][]Range {
	out := make([][]Range, n)
	for i := range out {
		out[i] = []Range{}
	}
	return out
}

// mark emphasises tokens on the lines they sit on. A line break sits on none.
func mark(tokens []token, out [][]Range) {
	for _, t := range tokens {
		if t.line != lineBreak {
			out[t.line] = appendMerged(out[t.line], Range{t.start, t.end})
		}
	}
}

// trimWhitespace takes whitespace off each range's ends and drops a range
// that was nothing else.
func trimWhitespace(lines []string, out [][]Range) {
	for i, ranges := range out {
		runes := []rune(lines[i])
		kept := []Range{}
		for _, r := range ranges {
			for r.Start < r.End && unicode.IsSpace(runes[r.Start]) {
				r.Start++
			}
			for r.End > r.Start && unicode.IsSpace(runes[r.End-1]) {
				r.End--
			}
			if r.Start < r.End {
				kept = append(kept, r)
			}
		}
		out[i] = kept
	}
}

// appendMerged adds a range, joining it to the last one when they touch.
func appendMerged(ranges []Range, r Range) []Range {
	if r.Start == r.End {
		return ranges
	}
	if n := len(ranges); n > 0 && ranges[n-1].End == r.Start {
		ranges[n-1].End = r.End
		return ranges
	}
	return append(ranges, r)
}

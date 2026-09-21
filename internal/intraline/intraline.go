// Package intraline finds the words that changed between a removed line and the
// line that replaced it, so the Reviewer is pointed at a one-word edit in a long
// line rather than left to compare the two word by word (#81).
//
// It delegates the algorithm, not the rendering (ADR-0005): it answers with
// ranges of runes, and whatever draws the rows styles them. dbn owns every row it
// draws — interleaved before-sides, previous-round rows, wrapping — so styled
// text from an outside renderer could not be mapped back onto them.
package intraline

import (
	"unicode"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// Range is a half-open run of runes, [Start, End), within one line.
type Range struct{ Start, End int }

// matchThreshold is how alike two lines' words must be before they are
// treated as one line edited rather than two unrelated lines — delta's choice.
// Below it, emphasising the differences would highlight nearly everything and
// point at nothing.
const matchThreshold = 0.4

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

// Emphasis matches removed lines with the added lines that took their place, and
// says, for each line, which of its runes changed. The two lists must come from one
// edit: matching never looks beyond them, so a line is never compared with one
// from somewhere else in the file.
//
// Matching walks both lists in order. Each removed line takes the next added line
// alike enough to be the same line edited; a line that finds no partner, or is
// never chosen, gets nil. A matched line whose text did not change gets an empty,
// non-nil list.
func Emphasis(removed, added []string) ([][]Range, [][]Range) {
	removedOut := make([][]Range, len(removed))
	addedOut := make([][]Range, len(added))
	next := 0
	for i, before := range removed {
		for j := next; j < len(added); j++ {
			diffs, similarity := compare(before, added[j])
			if similarity < matchThreshold {
				continue
			}
			removedOut[i], addedOut[j] = rangesOf(diffs, before, added[j])
			next = j + 1
			break
		}
	}
	return removedOut, addedOut
}

// compare diffs two lines token by token and says how alike they are: the
// share of their words the two have in common. Whitespace and punctuation are
// diffed but not counted towards likeness, since nearly every line of code
// shares its spaces, brackets and operators with every other and would match
// with anything. A line with no words at all is measured by all its tokens.
func compare(before, after string) ([]diffmatchpatch.Diff, float64) {
	a, b := Tokens(before), Tokens(after)
	encoded := map[string]rune{}
	decoded := map[rune]string{}
	encode := func(tokens []string) []rune {
		out := make([]rune, len(tokens))
		for i, token := range tokens {
			r, ok := encoded[token]
			if !ok {
				// A private-use plane, so no token can collide with a rune
				// diff-match-patch treats specially.
				// Planes 15 and 16 hold some 131,000 runes, far more distinct
				// tokens than one line can have.
				r = rune(0xF0000 + len(encoded))
				encoded[token] = r
				decoded[r] = token
			}
			out[i] = r
		}
		return out
	}
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMainRunes(encode(a), encode(b), false)

	counts := func(tokens []string, wordsOnly bool) int {
		n := 0
		for _, token := range tokens {
			if !wordsOnly || isWord([]rune(token)[0]) {
				n++
			}
		}
		return n
	}
	wordsOnly := counts(a, true)+counts(b, true) > 0
	total := counts(a, wordsOnly) + counts(b, wordsOnly)
	if total == 0 {
		return nil, 1
	}
	shared := 0
	for _, d := range diffs {
		if d.Type != diffmatchpatch.DiffEqual {
			continue
		}
		var tokens []string
		for _, r := range d.Text {
			tokens = append(tokens, decoded[r])
		}
		shared += counts(tokens, wordsOnly)
	}
	return dmp.DiffCleanupSemantic(diffs), float64(2*shared) / float64(total)
}

// rangesOf turns a token diff back into rune ranges on each line.
func rangesOf(diffs []diffmatchpatch.Diff, before, after string) ([]Range, []Range) {
	beforeTokens, afterTokens := Tokens(before), Tokens(after)
	removed, added := []Range{}, []Range{}
	var ti, tj, ri, rj int // token and rune positions in each line
	span := func(tokens []string, from, count, at int) (Range, int, int) {
		start := at
		for k := from; k < from+count; k++ {
			at += len([]rune(tokens[k]))
		}
		return Range{start, at}, from + count, at
	}
	for _, d := range diffs {
		n := len([]rune(d.Text))
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			_, ti, ri = span(beforeTokens, ti, n, ri)
			_, tj, rj = span(afterTokens, tj, n, rj)
		case diffmatchpatch.DiffDelete:
			var r Range
			r, ti, ri = span(beforeTokens, ti, n, ri)
			removed = appendMerged(removed, r)
		case diffmatchpatch.DiffInsert:
			var r Range
			r, tj, rj = span(afterTokens, tj, n, rj)
			added = appendMerged(added, r)
		}
	}
	return removed, added
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

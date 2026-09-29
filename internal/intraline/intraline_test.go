package intraline_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/intraline"
)

func TestALineSplitsIntoWordsWhitespaceAndSinglePunctuation(t *testing.T) {
	tokens := intraline.Tokens("retry(max_tries, 42)  ok")

	want := []string{"retry", "(", "max_tries", ",", " ", "42", ")", "  ", "ok"}
	if fmt.Sprint(tokens) != fmt.Sprint(want) {
		t.Errorf("expected %q, got %q", want, tokens)
	}
}

func TestAOneWordChangeEmphasisesJustThatWord(t *testing.T) {
	removed, added := intraline.Emphasis([]string{"return retry(transport, 3)"}, []string{"return retry(transport, 5)"})

	if fmt.Sprint(removed) != "[[{24 25}]]" || fmt.Sprint(added) != "[[{24 25}]]" {
		t.Errorf("expected only the number emphasised on each side, got %v and %v", removed, added)
	}
}

func TestOffsetsAreInRunesNotBytes(t *testing.T) {
	removed, added := intraline.Emphasis([]string{"héllo wörld"}, []string{"héllo world"})

	if fmt.Sprint(removed) != "[[{6 11}]]" || fmt.Sprint(added) != "[[{6 11}]]" {
		t.Errorf("expected the second word, by rune offset, got %v and %v", removed, added)
	}
}

// A line replaced by an unrelated one (from 723d125). Nothing of the old text
// is in the new, so each is lit whole: bright means removed or added, and a
// row with nothing lit only ever means its text is on the other side too.
func TestAnUnrelatedReplacementIsLitWhole(t *testing.T) {
	removed := []string{"\tb.WriteString(labelSt.Render(title) + \"\\n\\n\")"}
	added := []string{"\titems := make([][]string, 0, len(list))"}

	removedRanges, addedRanges := intraline.Emphasis(removed, added)

	expectMarked(t, "removed", removed, removedRanges, []string{"\t[b.WriteString(labelSt.Render(title) + \"\\n\\n\")]"})
	expectMarked(t, "added", added, addedRanges, []string{"\t[items := make([][]string, 0, len(list))]"})
}

func TestIdenticalLinesHaveNothingToEmphasise(t *testing.T) {
	removed, added := intraline.Emphasis([]string{"same line"}, []string{"same line"})

	if len(removed[0]) != 0 || len(added[0]) != 0 {
		t.Errorf("expected no emphasis, got %v and %v", removed, added)
	}
}

func TestLinesWithNoWordsAreComparedByAllTheirTokens(t *testing.T) {
	removed, added := intraline.Emphasis([]string{"}"}, []string{"});"})

	if removed[0] == nil || fmt.Sprint(added) != "[[{1 3}]]" {
		t.Errorf("expected the closing brace matched and \");\" emphasised, got %v and %v", removed, added)
	}
}

// marked shows each line with its emphasised runs in brackets, so a test can
// state what the Reviewer would see lit.
func marked(lines []string, ranges [][]intraline.Range) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		runes := []rune(line)
		var b strings.Builder
		at := 0
		for _, r := range ranges[i] {
			b.WriteString(string(runes[at:r.Start]) + "[" + string(runes[r.Start:r.End]) + "]")
			at = r.End
		}
		b.WriteString(string(runes[at:]))
		out[i] = b.String()
	}
	return out
}

func expectMarked(t *testing.T, side string, lines []string, ranges [][]intraline.Range, want []string) {
	t.Helper()
	got := marked(lines, ranges)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s side:\nwant\n%s\ngot\n%s", side, strings.Join(want, "\n"), strings.Join(got, "\n"))
	}
}

// A rewritten Markdown bullet, rewrapped (from c9f23b4). The words that only
// moved across a wrap are unchanged text, so only the rewrite is lit (#125).
func TestRewrappedProseLightsOnlyTheRewrite(t *testing.T) {
	removed := []string{
		"  Step's `questions`; it is shown at the top of the Step, and only there. A",
		"  question about the approach goes in the Round's own `questions`, shown with the",
		"  Brief.",
	}
	added := []string{
		"  Step's `questions`; it is shown after the Step's explanation and before its",
		"  code, and only there. A question about the approach goes in the Round's own",
		"  `questions`, shown with the Brief.",
	}

	removedRanges, addedRanges := intraline.Emphasis(removed, added)

	expectMarked(t, "removed", removed, removedRanges, []string{
		"  Step's `questions`; it is shown [at the top of the Step], and only there. A",
		"  question about the approach goes in the Round's own `questions`, shown with the",
		"  Brief.",
	})
	expectMarked(t, "added", added, addedRanges, []string{
		"  Step's `questions`; it is shown [after the Step's explanation and before its]",
		"  [code], and only there. A question about the approach goes in the Round's own",
		"  `questions`, shown with the Brief.",
	})
}

// A doc comment rewrapped around a one-word change (from 31ff2c2). "asked" only
// moved across the wrap, and each line's "//" moves with it, so neither is lit.
func TestARewrappedCommentLightsOnlyTheChangedWord(t *testing.T) {
	removed := []string{
		"// declinedItem draws one decline of the re-raise picker: what the Reviewer asked",
		"// and what the agent said back, which is what they weigh before pushing.",
	}
	added := []string{
		"// declinedItem draws one resolution of the re-raise picker: what the Reviewer",
		"// asked and what the agent said back, which is what they weigh before pushing.",
	}

	removedRanges, addedRanges := intraline.Emphasis(removed, added)

	expectMarked(t, "removed", removed, removedRanges, []string{
		"// declinedItem draws one [decline] of the re-raise picker: what the Reviewer asked",
		"// and what the agent said back, which is what they weigh before pushing.",
	})
	expectMarked(t, "added", added, addedRanges, []string{
		"// declinedItem draws one [resolution] of the re-raise picker: what the Reviewer",
		"// asked and what the agent said back, which is what they weigh before pushing.",
	})
}

// A rewritten, rewrapped doc comment (from 15189d8): the rename and the new
// words are lit, and neither the "//" markers nor the words that only moved
// across a wrap are.
func TestARewrittenCommentLightsOnlyTheRewrite(t *testing.T) {
	removed := []string{
		"// wrapRunes hard-wraps a plain (ANSI-free) string so the line under the cursor",
		"// can be read in full where a single row would clip it.",
	}
	added := []string{
		"// wrapCode soft-wraps a plain (ANSI-free) line of code so the line under the",
		"// cursor can be read in full where a single row would clip it.",
	}

	removedRanges, addedRanges := intraline.Emphasis(removed, added)

	expectMarked(t, "removed", removed, removedRanges, []string{
		"// [wrapRunes hard]-wraps a plain (ANSI-free) [string] so the line under the cursor",
		"// can be read in full where a single row would clip it.",
	})
	expectMarked(t, "added", added, addedRanges, []string{
		"// [wrapCode soft]-wraps a plain (ANSI-free) [line of code] so the line under the",
		"// cursor can be read in full where a single row would clip it.",
	})
}

// A sentence appended to a line, running on over new lines (from 576febf). The
// passages are too unalike overall to pass as one edited, but nearly all of the
// shorter side is in the longer, so the addition is still lit, and the lines
// wholly added are lit whole.
func TestASmallEditBesideManyAddedLinesKeepsItsEmphasis(t *testing.T) {
	removed := []string{
		"run — but drift is now reported instead of silent.",
	}
	added := []string{
		`run — but drift is now reported instead of silent. The skill's "Requires dbn" line, the`,
		"oldest release it works with, is still set by hand. A release warns when the MCP schema changed",
		"since the last one but the line did not, or when the line names a release that is not out yet.",
	}

	removedRanges, addedRanges := intraline.Emphasis(removed, added)

	expectMarked(t, "removed", removed, removedRanges, removed)
	expectMarked(t, "added", added, addedRanges, []string{
		`run — but drift is now reported instead of silent. [The skill's "Requires dbn" line, the]`,
		"[oldest release it works with, is still set by hand. A release warns when the MCP schema changed]",
		"[since the last one but the line did not, or when the line names a release that is not out yet.]",
	})
}

// A rename gofmt realigned around (from 7fd0a76). The realigning whitespace is
// not lit, so the untouched Status line shows nothing. One unchanged word
// between two edits is too little to keep them apart, so each renamed line is
// lit as one span.
func TestRealignedCodeLightsTheRenameNotTheWhitespace(t *testing.T) {
	removed := []string{
		"\t\t\tChangeRequestID: d.ChangeRequestID,",
		"\t\t\tStatus:          review.DispositionStatus(d.Status),",
		"\t\t\tReasoning:       d.Reasoning,",
	}
	added := []string{
		"\t\t\tCommentID: d.CommentID,",
		"\t\t\tStatus:    review.DispositionStatus(d.Status),",
		"\t\t\tResponse:  d.Response,",
	}

	removedRanges, addedRanges := intraline.Emphasis(removed, added)

	expectMarked(t, "removed", removed, removedRanges, []string{
		"\t\t\t[ChangeRequestID: d.ChangeRequestID],",
		"\t\t\tStatus:          review.DispositionStatus(d.Status),",
		"\t\t\t[Reasoning:       d.Reasoning],",
	})
	expectMarked(t, "added", added, addedRanges, []string{
		"\t\t\t[CommentID: d.CommentID],",
		"\t\t\tStatus:    review.DispositionStatus(d.Status),",
		"\t\t\t[Response:  d.Response],",
	})
}

// A label realigned by padding inside a string (from c6f9174). Whitespace is
// never lit on its own, even inside a literal.
func TestAWhitespaceOnlyChangeLightsNothing(t *testing.T) {
	removed := []string{"\t\tfmt.Fprintf(&out, \"Asks:     %s\\n\", question.Text)"}
	added := []string{"\t\tfmt.Fprintf(&out, \"Asks:       %s\\n\", question.Text)"}

	removedRanges, addedRanges := intraline.Emphasis(removed, added)

	expectMarked(t, "removed", removed, removedRanges, removed)
	expectMarked(t, "added", added, addedRanges, added)
}

// A term lower-cased on screen (from 0d367aa). A case change is a real edit.
func TestACaseOnlyChangeIsLit(t *testing.T) {
	removed := []string{"\t\treturn []string{pluralize(answered, \"Agent Question\") + \" answered\"}"}
	added := []string{"\t\treturn []string{pluralize(answered, \"agent question\") + \" answered\"}"}

	removedRanges, addedRanges := intraline.Emphasis(removed, added)

	expectMarked(t, "removed", removed, removedRanges, []string{"\t\treturn []string{pluralize(answered, \"[Agent Question]\") + \" answered\"}"})
	expectMarked(t, "added", added, addedRanges, []string{"\t\treturn []string{pluralize(answered, \"[agent question]\") + \" answered\"}"})
}

// A marker is folded into the line break only when both sides carry it down
// consecutive lines, as a rewrapped comment does. Dropping it from lines is a
// real edit, and is lit.
func TestRemovingALeadingMarkerIsLit(t *testing.T) {
	cases := map[string]struct{ removed, added, wantRemoved []string }{
		"uncommenting code": {
			removed:     []string{"// alpha()", "// beta()"},
			added:       []string{"alpha()", "beta()"},
			wantRemoved: []string{"[// alpha()]", "[//] beta()"},
		},
		"dropping a dereference": {
			removed:     []string{"*p = compute(one, two)", "*q = compute(three, four)"},
			added:       []string{"*p = compute(one, two)", "q = compute(three, four)"},
			wantRemoved: []string{"*p = compute(one, two)", "[*]q = compute(three, four)"},
		},
		"demoting a heading": {
			removed:     []string{"# Title words", "# Other words here"},
			added:       []string{"# Title words", "Other words here"},
			wantRemoved: []string{"# Title words", "[#] Other words here"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			removedRanges, _ := intraline.Emphasis(c.removed, c.added)

			expectMarked(t, "removed", c.removed, removedRanges, c.wantRemoved)
		})
	}
}

// Each distinct token is diffed as one rune from Unicode's private-use planes,
// which hold 131,072. An edit with more distinct tokens than that can't be
// diffed faithfully, so it is lit whole, as a replacement is, rather than lit
// wrongly.
func TestAnEditWithMoreDistinctTokensThanCanBeEncodedIsLitWhole(t *testing.T) {
	var words []string
	for i := range 140_000 {
		words = append(words, fmt.Sprintf("w%d", i))
	}
	removed := []string{strings.Join(words, " ")}
	words[0] = "changed"
	added := []string{strings.Join(words, " ")}

	removedRanges, addedRanges := intraline.Emphasis(removed, added)

	wholeRemoved := []intraline.Range{{Start: 0, End: len([]rune(removed[0]))}}
	wholeAdded := []intraline.Range{{Start: 0, End: len([]rune(added[0]))}}
	if fmt.Sprint(removedRanges[0]) != fmt.Sprint(wholeRemoved) || fmt.Sprint(addedRanges[0]) != fmt.Sprint(wholeAdded) {
		t.Errorf("expected each side lit whole, got %d and %d ranges", len(removedRanges[0]), len(addedRanges[0]))
	}
}

// A sentence added to a comment, running on to a line of its own (from
// 1c67bb3). Every word of that line is new, so all of it is lit, and a lit row
// is never mistaken for one whose text only moved.
func TestAnEntirelyNewLineIsLitWhole(t *testing.T) {
	removed := []string{"// rather than as an id dbn never had (ADR-0015)."}
	added := []string{
		"// rather than as an id dbn never had (ADR-0015). The tombstone lives as long as",
		"// the daemon does: it is memory, not a promise.",
	}

	removedRanges, addedRanges := intraline.Emphasis(removed, added)

	expectMarked(t, "removed", removed, removedRanges, removed)
	expectMarked(t, "added", added, addedRanges, []string{
		"// rather than as an id dbn never had (ADR-0015). [The tombstone lives as long as]",
		"[// the daemon does: it is memory, not a promise.]",
	})
}

func TestAnEditWithNoLinesOnOneSideLightsTheOtherWhole(t *testing.T) {
	removedRanges, addedRanges := intraline.Emphasis(nil, []string{"", "  added()"})

	if len(removedRanges) != 0 {
		t.Errorf("expected nothing for the removed side, got %v", removedRanges)
	}
	if fmt.Sprint(addedRanges) != "[[] [{2 9}]]" {
		t.Errorf("expected the added line lit whole and the empty line with nothing, got %v", addedRanges)
	}
}

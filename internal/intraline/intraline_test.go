package intraline_test

import (
	"fmt"
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

func TestDissimilarLinesAreNotMatched(t *testing.T) {
	removed, added := intraline.Emphasis(
		[]string{"total := sum(values)"},
		[]string{"// the configuration file lives elsewhere now"},
	)

	if removed[0] != nil || added[0] != nil {
		t.Errorf("unrelated lines must get no emphasis, got %v and %v", removed, added)
	}
}

func TestMatchingRunsInOrder(t *testing.T) {
	// The first removed line matches the second added line; the second removed
	// line would match the first added line, but that lies behind the match
	// already made, so it stays unmatched.
	removed, added := intraline.Emphasis(
		[]string{"alpha := compute(one)", "beta := fetch(two)"},
		[]string{"beta := fetch(three)", "alpha := compute(four)"},
	)

	if removed[0] == nil || added[1] == nil {
		t.Errorf("expected the alpha lines matched, got %v and %v", removed, added)
	}
	if removed[1] != nil || added[0] != nil {
		t.Errorf("expected the beta lines left unmatched, got %v and %v", removed, added)
	}
}

func TestIdenticalMatchedLinesHaveNothingToEmphasise(t *testing.T) {
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

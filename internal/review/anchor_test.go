package review_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

func TestAnAnchorCarriesEverythingNeededToPasteIntoChat(t *testing.T) {
	session := newSession() // stubResolver fabricates "<file> line N"
	mustPost(t, session, validRound())
	mustAdvance(t, session) // Step 1: src/fetch.ts new 12-34

	anchor, err := session.Anchor(span(0, 20, 22))
	if err != nil {
		t.Fatalf("expected an Anchor, got %v", err)
	}

	if anchor.File != "src/fetch.ts" {
		t.Errorf("unexpected file: %s", anchor.File)
	}
	want := []review.AnchorSegment{{Side: review.NewSide, FirstLine: 20, LastLine: 22}}
	if len(anchor.Segments) != 1 || anchor.Segments[0] != want[0] {
		t.Errorf("expected the one-sided Anchor to be a single segment %+v, got %+v", want, anchor.Segments)
	}
	if anchor.StepName != "Add the retrier" {
		t.Errorf("expected the Step name, got %q", anchor.StepName)
	}
	if len(anchor.Lines) != 3 {
		t.Fatalf("expected 3 lines of code, got %d", len(anchor.Lines))
	}

	rendered := anchor.Render()
	// Self-contained: an agent reading only this must know repo, file, lines,
	// which Step, and see the code.
	for _, want := range []string{"argus-portal", "src/fetch.ts", "after 20-22", "Add the retrier", "src/fetch.ts line 21"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("expected the rendered Anchor to contain %q\n---\n%s", want, rendered)
		}
	}
}

func TestAnchoringAtTheBriefIsRejected(t *testing.T) {
	session := newSession()
	mustPost(t, session, validRound())

	_, err := session.Anchor(span(0, 20, 22))

	assertRejected(t, err, review.RejectedNoSuchStep)
}

func TestAnchorMustNameAnExcerptInTheStep(t *testing.T) {
	session := newSession()
	mustPost(t, session, validRound())
	mustAdvance(t, session)

	_, err := session.Anchor(span(9, 20, 22))

	assertRejected(t, err, review.RejectedBadSelection)
}

func TestAnchorEndpointsMustNameRowsTheExcerptShows(t *testing.T) {
	session := newSession()
	mustPost(t, session, validRound()) // excerpt is 12-34
	mustAdvance(t, session)

	cases := map[string]review.AnchorTarget{
		"before the excerpt": span(0, 5, 15),
		"after the excerpt":  span(0, 30, 40),
		"wrong side":         sideSpan(0, review.AnchorEndpoint{Side: review.OldSide, Line: 20}, review.AnchorEndpoint{Side: review.OldSide, Line: 22}),
	}
	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := session.Anchor(target)

			assertRejected(t, err, review.RejectedBadSelection)
		})
	}
}

func TestAnchorEndsAreRowsNotBounds(t *testing.T) {
	// The Reviewer can select upwards, so which end was reached first says nothing
	// about which line comes first.
	session := newSession()
	mustPost(t, session, validRound())
	mustAdvance(t, session)

	anchor, err := session.Anchor(span(0, 22, 20))

	if err != nil {
		t.Fatalf("expected an upward selection to anchor, got %v", err)
	}
	want := review.AnchorSegment{Side: review.NewSide, FirstLine: 20, LastLine: 22}
	if len(anchor.Segments) != 1 || anchor.Segments[0] != want {
		t.Errorf("expected the run to be ordered %+v, got %+v", want, anchor.Segments)
	}
}

func TestAnchorReportsWhenTheCodeCannotBeRead(t *testing.T) {
	session := review.NewSession(failingResolver{}, emptyDeriver{})
	walkthrough := validRound()
	walkthrough.Steps[0].Excerpts[0].Side = review.OldSide // post skips old-side resolution
	mustPost(t, session, walkthrough)
	mustAdvance(t, session)

	_, err := session.Anchor(span(0, 20, 22))

	if err == nil {
		t.Fatal("expected anchoring to fail when the code cannot be read")
	}
}

// In a Document's formatted view the Reviewer selects whole blocks, and the
// Anchor carries those blocks' source lines (#148). The selection names the
// blocks at each end by a row of each, and dbn takes every row of every block
// between them.

// blocks is a selection of the blocks holding the two new-side rows, and every
// block between them.
func blocks(excerpt, first, last int) review.AnchorTarget {
	return review.AnchorTarget{ExcerptIndex: excerpt, Blocks: true,
		Start: review.AnchorEndpoint{Line: first}, End: review.AnchorEndpoint{Line: last}}
}

func numbersOf(lines []review.Line) []int {
	var out []int
	for _, line := range lines {
		out = append(out, line.Number)
	}
	return out
}

func TestSelectingTwoParagraphsAnchorsTheirSourceLines(t *testing.T) {
	// A heading on line 1; paragraphs on 3-4, 6-7 and 9.
	files := adding("docs/retries.md", markdown("# Retries", "one two\nthree four", "five six\nseven eight", "nine"))
	session := openDocumentRound(t, files, showingWhole("Retries", files, review.NewSide, "docs/retries.md"))

	anchor, err := session.Anchor(blocks(0, 3, 6))

	if err != nil {
		t.Fatalf("expected the two paragraphs to anchor, got %v", err)
	}
	if got, want := numbersOf(anchor.Lines), []int{3, 4, 5, 6, 7}; !slices.Equal(got, want) {
		t.Errorf("expected both paragraphs whole, and the blank line between them, %v; got %v", want, got)
	}
	if want := []review.AnchorSegment{{Side: review.NewSide, FirstLine: 3, LastLine: 7}}; !slices.Equal(anchor.Segments, want) {
		t.Errorf("expected the Anchor to cover %v, got %v", want, anchor.Segments)
	}
	if rendered := anchor.Render(); !strings.Contains(rendered, "+     7 | seven eight") {
		t.Errorf("expected the source lines quoted with their signs and numbers:\n%s", rendered)
	}
}

func TestAnEditedParagraphsAnchorQuotesItsRemovedAndAddedLines(t *testing.T) {
	// The paragraph on line 3 is edited; a new one follows it on line 5.
	files := modifying("docs/retries.md",
		markdown("# Retries", "A request is retried when the transport fails."),
		markdown("# Retries", "A request is resent when the transport fails.", "It waits between attempts."),
	)
	session := openDocumentRound(t, files, showingLines("Resend", "docs/retries.md", review.NewSide, 3, 5))

	anchor, err := session.Anchor(blocks(0, 3, 3))

	if err != nil {
		t.Fatalf("expected the edited paragraph to anchor, got %v", err)
	}
	want := []review.AnchorSegment{
		{Side: review.OldSide, FirstLine: 3, LastLine: 3},
		{Side: review.NewSide, FirstLine: 3, LastLine: 3},
	}
	if !slices.Equal(anchor.Segments, want) {
		t.Errorf("expected the paragraph's removed and added lines, %v; got %v", want, anchor.Segments)
	}
	rendered := anchor.Render()
	for _, line := range []string{"-     3 | A request is retried", "+     3 | A request is resent"} {
		if !strings.Contains(rendered, line) {
			t.Errorf("expected %q quoted:\n%s", line, rendered)
		}
	}
}

// attemptsTable is a heading on line 1 and a table: its header row on line 3,
// its divider on 4 and its rows on 5-8.
const attemptsTable = "| Attempt | Delay |\n| ------- | ----- |\n| 1 | none |\n| 2 | 1s |\n| 3 | 2s |\n| 4 | 4s |"

func TestAnchoringATableRowQuotesTheHeaderRowForContext(t *testing.T) {
	// One Step shows the heading and the first row; the one under test shows
	// rows 7-8, so dbn adds the header row and divider to it as context.
	files := adding("docs/retries.md", markdown("# Retries", attemptsTable))
	session := openDocumentRound(t, files,
		showingLines("The last attempts", "docs/retries.md", review.NewSide, 7, 8),
		showingLines("The first attempt", "docs/retries.md", review.NewSide, 1, 6))

	anchor, err := session.Anchor(blocks(0, 8, 8))

	if err != nil {
		t.Fatalf("expected the table row to anchor, got %v", err)
	}
	if got, want := numbersOf(anchor.Lines), []int{3, 4, 8}; !slices.Equal(got, want) {
		t.Errorf("expected the header row and divider quoted above the row, %v; got %v", want, got)
	}
	// The header is there to read the row by; the Comment is on the row.
	if want := []review.AnchorSegment{{Side: review.NewSide, FirstLine: 8, LastLine: 8}}; !slices.Equal(anchor.Segments, want) {
		t.Errorf("expected the Anchor to point at the row alone, %v; got %v", want, anchor.Segments)
	}
	if rendered := anchor.Render(); !strings.Contains(rendered, "| Attempt | Delay |") || !strings.Contains(rendered, "after 8 ") {
		t.Errorf("expected the header quoted and the row named:\n%s", rendered)
	}
}

func TestAnchoringATablesHeaderRowPointsAtIt(t *testing.T) {
	files := adding("docs/retries.md", markdown("# Retries", attemptsTable))
	session := openDocumentRound(t, files, showingWhole("Retries", files, review.NewSide, "docs/retries.md"))

	anchor, err := session.Anchor(blocks(0, 3, 5))

	if err != nil {
		t.Fatalf("expected the header row and first row to anchor, got %v", err)
	}
	if want := []review.AnchorSegment{{Side: review.NewSide, FirstLine: 3, LastLine: 5}}; !slices.Equal(anchor.Segments, want) {
		t.Errorf("expected the Anchor to cover the header row it was raised on, %v; got %v", want, anchor.Segments)
	}
}

func TestASelectionOfBlocksMustEndInBlocks(t *testing.T) {
	files := adding("docs/retries.md", markdown("# Retries", "one two"))
	session := openDocumentRound(t, files, showingWhole("Retries", files, review.NewSide, "docs/retries.md"))

	_, err := session.Anchor(blocks(0, 2, 3)) // line 2 is the blank line

	assertRejected(t, err, review.RejectedBadSelection)
}

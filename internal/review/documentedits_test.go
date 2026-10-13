package review_test

import (
	"fmt"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// An edit to a Document is charged for what the Reviewer reads: the block as it
// stands after the edit, with the words the edit struck from it. These tests post
// Rounds over real Markdown that a Change Set modified, so the cost is what dbn
// makes of the edit itself.

// replacing is ws with the count words from index at replaced by the given ones.
func replacing(ws []string, at, count int, with []string) []string {
	out := append([]string{}, ws[:at]...)
	out = append(out, with...)
	return append(out, ws[at+count:]...)
}

// modifying is a Change Set that changes one Document from before to after.
func modifying(path, before, after string) workingFiles {
	return workingFiles{
		after:  map[string]string{path: after},
		before: map[string]string{path: before},
	}
}

func TestAnEditedParagraphIsChargedItsWordsAfterTheEditAndTheWordsItStruck(t *testing.T) {
	// A paragraph of 60 words has 4 replaced by 5, so the Reviewer reads 61 words
	// and the 4 struck: 65. A new paragraph of 940 words beside it puts the Step
	// over budget, so the refusal says what the edit was charged.
	original := words("retry", 60)
	edited := replacing(original, 20, 4, words("backoff", 5))
	files := modifying("docs/retries.md",
		markdown("# Retries", wrapped(10, original)),
		markdown("# Retries", wrapped(10, edited), wrapped(10, words("limit", 940))),
	)

	err := postingOver(files, showingWhole("How retries back off", files, review.NewSide, "docs/retries.md"))

	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, `Step 1 ("How retries back off") counts 1005 words of Document text`)
}

// rewrapped is a Document whose one paragraph of 1,100 words went from ten words
// a line to twelve: every line of it changed, and no word.
func rewrapped() workingFiles {
	paragraphWords := words("retry", 1100)
	return modifying("docs/retries.md",
		markdown("# Retries", wrapped(10, paragraphWords)),
		markdown("# Retries", wrapped(12, paragraphWords)),
	)
}

func TestARewrappedParagraphCostsNothing(t *testing.T) {
	files := rewrapped()

	err := postingOver(files, showingWhole("Rewrap the retries section", files, review.NewSide, "docs/retries.md"))

	if err != nil {
		t.Fatalf("expected a rewrap of 1,100 words to cost nothing, got %v", err)
	}
}

func TestARewrappedParagraphMustStillBeShownOrAcknowledged(t *testing.T) {
	files := rewrapped()

	// The rewrapped paragraph now runs over lines 3-94.
	err := postingOver(files, showingLines("Part of the rewrap", "docs/retries.md", review.NewSide, 3, 50))

	assertRejected(t, err, review.RejectedUncoveredChanges)
	assertDetailOmits(t, err, string(review.RejectedOversizedStep))
}

func TestAParagraphReadInThePreviousRoundCostsNothingToShowAgain(t *testing.T) {
	// A paragraph of 950 words on lines 3-97, read in Round 1, and one of 100 on
	// lines 99-108, rewritten since.
	files := revisedDocuments{
		workingFiles: adding("docs/retries.md",
			markdown("# Retries", wrapped(10, words("read", 950)), wrapped(10, words("fix", 100)))),
		touched: map[string]bool{},
	}
	for line := 99; line <= 108; line++ {
		files.touched[fmt.Sprintf("docs/retries.md:%d", line)] = true
	}
	session := review.NewSession(files, files)
	mustPost(t, session, documentRound(
		showingLines("Why retries exist", "docs/retries.md", review.NewSide, 1, 97),
		showingLines("When they stop", "docs/retries.md", review.NewSide, 99, 108),
	))
	handOffWithAComment(t, session)

	err := session.Revise(session.ReviewID(), revising(documentRound(
		showingWhole("Retries, with the fix in context", files.workingFiles, review.NewSide, "docs/retries.md"),
	)))

	if err != nil {
		t.Fatalf("expected only the 100 rewritten words to be charged, got %v", err)
	}
}

func TestAnExcerptCuttingIntoAnEditedParagraphIsChargedTheWholeEdit(t *testing.T) {
	// A six-line paragraph of 60 words on lines 3-8 has 4 words of its third line
	// replaced by 4 others: 64 words to read. A new paragraph of 940 words follows
	// on lines 10-103. Step 1 shows only the paragraph's first two lines, which
	// the edit did not touch, beside the new paragraph.
	original := words("retry", 60)
	files := modifying("docs/retries.md",
		markdown("# Retries", wrapped(10, original)),
		markdown("# Retries", wrapped(10, replacing(original, 20, 4, words("backoff", 4))), wrapped(10, words("limit", 940))),
	)
	step := showingLines("The opening and the limits", "docs/retries.md", review.NewSide, 3, 4)
	step.Excerpts = append(step.Excerpts, review.Excerpt{
		Repository: docsRepo, File: "docs/retries.md", Side: review.NewSide, FirstLine: 10, LastLine: 103,
	})

	err := postingOver(files, step, showingLines("The edit", "docs/retries.md", review.NewSide, 3, 8))

	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, `Step 1 ("The opening and the limits") counts 1004 words`)
	assertDetailOmits(t, err, "The edit")
}

func TestARemovedParagraphShownOnItsOwnCostsItsRemovedWords(t *testing.T) {
	// A paragraph of 1,050 words on lines 5-109 is removed from the end of the
	// Document, so it must be shown before-side.
	files := modifying("docs/retries.md",
		markdown("# Retries", wrapped(10, words("retry", 10)), wrapped(10, words("gone", 1050))),
		markdown("# Retries", wrapped(10, words("retry", 10))),
	)

	err := postingOver(files, showingLines("Drop the old limits", "docs/retries.md", review.OldSide, 5, 109))

	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, `Step 1 ("Drop the old limits") counts 1050 words`)
}

func TestAParagraphReplacedByAnotherCostsTheRemovedWordsDrawnWithIt(t *testing.T) {
	// A paragraph of 1,000 words is replaced by an unrelated one of 50. The Step
	// shows only the new paragraph, and the old one rides along with it.
	files := modifying("docs/retries.md",
		markdown("# Retries", wrapped(10, words("gone", 1000)), "## Limits"),
		markdown("# Retries", wrapped(10, words("retry", 50)), "## Limits"),
	)

	err := postingOver(files, showingLines("Rewrite the section", "docs/retries.md", review.NewSide, 3, 7))

	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, `Step 1 ("Rewrite the section") counts 1050 words`)
}

package review_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// A Step's view gives each Document Excerpt its blocks, so the Reviewer's
// surface can draw the Document formatted, block by block, without parsing
// Markdown itself (ADR-0005: dbn delegates the algorithm, not the rendering).
// These tests post Rounds over real Markdown and read what the Step view says.

// retriesDocument is a new Markdown Document with one block of every kind.
const retriesDocument = `---
title: Retries
---

# Retries

A request is retried when the
transport fails.

- the first attempt
- the second attempt

| Attempt | Delay |
| ------- | ----- |
| 1       | none  |

` + "```go" + `
retry()
` + "```" + `
`

// viewingStep posts a Round of the Steps over the files and opens the first.
func viewingStep(t *testing.T, files workingFiles, steps ...review.Step) *review.StepView {
	t.Helper()
	session := review.NewSession(files, files)
	mustPost(t, session, documentRound(steps...))
	mustAdvance(t, session)
	return session.View().Step
}

func TestANewDocumentsExcerptCarriesItsBlocksWithTheirKindsRangesAndTreatment(t *testing.T) {
	files := adding("docs/retries.md", retriesDocument)

	step := viewingStep(t, files, showingWhole("Retries", files, review.NewSide, "docs/retries.md"))

	newBlock := func(kind review.BlockKind, level, first, last int) review.Block {
		return review.Block{Kind: kind, Level: level, Side: review.NewSide,
			Lines: review.LineRange{First: first, Last: last}, Treatment: review.TreatmentNew}
	}
	want := []review.Block{
		newBlock(review.BlockFrontmatter, 0, 1, 3),
		newBlock(review.BlockHeading, 1, 5, 5),
		newBlock(review.BlockParagraph, 0, 7, 8),
		newBlock(review.BlockListItem, 0, 10, 10),
		newBlock(review.BlockListItem, 0, 11, 11),
		newBlock(review.BlockTableRow, 0, 13, 14),
		newBlock(review.BlockTableRow, 0, 15, 15),
		newBlock(review.BlockCode, 0, 17, 19),
	}
	if got := step.Excerpts[0].Blocks; !slices.Equal(got, want) {
		t.Errorf("expected the Document's blocks\n%+v\ngot\n%+v", want, got)
	}
}

func TestEachRowOfADocumentExcerptNamesTheBlockItBelongsTo(t *testing.T) {
	files := adding("docs/retries.md", retriesDocument)

	step := viewingStep(t, files, showingWhole("Retries", files, review.NewSide, "docs/retries.md"))

	// Blocks are numbered from 1 among the Excerpt's; a blank line between
	// blocks is in none.
	want := map[int]int{1: 1, 3: 1, 4: 0, 5: 2, 7: 3, 8: 3, 9: 0, 11: 5, 14: 6, 18: 8}
	for _, line := range step.Excerpts[0].Lines {
		if block, ok := want[line.Number]; ok && line.Block != block {
			t.Errorf("expected line %d in block %d, got %d", line.Number, block, line.Block)
		}
	}
}

func TestADocumentsUnchangedBlocksAreContextBesideItsNewOnes(t *testing.T) {
	// A paragraph inserted after an unchanged one; the Excerpt shows both.
	before := markdown("# Retries", "A request is retried\nwhen the transport fails.")
	after := markdown("# Retries", "A request is retried\nwhen the transport fails.", "It is retried three times.")
	files := modifying("docs/retries.md", before, after)

	step := viewingStep(t, files, showingLines("Retries", "docs/retries.md", review.NewSide, 3, 6))

	var treatments []review.Treatment
	for _, block := range step.Excerpts[0].Blocks {
		treatments = append(treatments, block.Treatment)
	}
	if want := []review.Treatment{review.TreatmentContext, review.TreatmentNew}; !slices.Equal(treatments, want) {
		t.Errorf("expected the unchanged paragraph as context and the new one new, got %v", treatments)
	}
}

func TestOnlyTheDocumentInAMixedStepHasBlocks(t *testing.T) {
	files := adding("docs/retries.md", retriesDocument, "retry.go", code(3))
	step := showingWhole("Retries and their code", files, review.NewSide, "docs/retries.md", "retry.go")

	view := viewingStep(t, files, step)

	if len(view.Excerpts[0].Blocks) == 0 {
		t.Error("expected the Document's Excerpt to carry its blocks")
	}
	if blocks := view.Excerpts[1].Blocks; len(blocks) != 0 {
		t.Errorf("expected code to carry no blocks, got %+v", blocks)
	}
	for _, line := range view.Excerpts[1].Lines {
		if line.Block != 0 {
			t.Errorf("expected no line of code in a block, got %+v", line)
		}
	}
}

func TestAnExpandedAcknowledgementOverANewDocumentCarriesItsBlocks(t *testing.T) {
	files := adding("docs/retries.md", retriesDocument, "retry.go", code(3))
	step := showingWhole("The code", files, review.NewSide, "retry.go")
	step.Acknowledgements = []review.Acknowledgement{{
		Repository: docsRepo, Files: []string{"docs/retries.md"}, Reason: "the agent's notes, read elsewhere",
	}}
	session := review.NewSession(files, files)
	mustPost(t, session, documentRound(step))

	views, err := session.ExpandAcknowledgement(1, 0)

	if err != nil {
		t.Fatalf("expected the Acknowledgement to expand, got %v", err)
	}
	var kinds []string
	for _, view := range views {
		for _, block := range view.Blocks {
			kinds = append(kinds, string(block.Kind))
		}
	}
	if !strings.Contains(strings.Join(kinds, " "), "heading paragraph") {
		t.Errorf("expected the expansion to carry the Document's blocks, got %v", kinds)
	}
}

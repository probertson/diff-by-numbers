package review_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// A Step's view gives each Document Excerpt its blocks, so the Reviewer's
// surface can draw the Document formatted, block by block, without working out
// its structure itself: only the inline styling within a block is left to it
// (ADR-0005, #166).
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

// openDocumentRound posts a Round of the Steps over the files and opens the
// first.
func openDocumentRound(t *testing.T, files workingFiles, steps ...review.Step) *review.Session {
	t.Helper()
	session := review.NewSession(files, files)
	mustPost(t, session, documentRound(steps...))
	mustAdvance(t, session)
	return session
}

// viewingStep is the view of the first Step of a Round of the Steps over the
// files.
func viewingStep(t *testing.T, files workingFiles, steps ...review.Step) *review.StepView {
	t.Helper()
	return openDocumentRound(t, files, steps...).View().Step
}

func TestANewDocumentsExcerptCarriesItsBlocksWithTheirKindsRangesAndTreatment(t *testing.T) {
	files := adding("docs/retries.md", retriesDocument)

	step := viewingStep(t, files, showingWhole("Retries", files, review.NewSide, "docs/retries.md"))

	newBlock := func(kind review.BlockKind, level, first, last int, path ...string) review.Block {
		return review.Block{Kind: kind, Level: level, Side: review.NewSide,
			Lines: review.LineRange{First: first, Last: last}, Treatment: review.TreatmentNew, Path: path}
	}
	want := []review.Block{
		newBlock(review.BlockFrontmatter, 0, 1, 3),
		newBlock(review.BlockHeading, 1, 5, 5),
		newBlock(review.BlockParagraph, 0, 7, 8, "Retries"),
		newBlock(review.BlockListItem, 0, 10, 10, "Retries"),
		newBlock(review.BlockListItem, 0, 11, 11, "Retries"),
		newBlock(review.BlockTableRow, 0, 13, 14, "Retries"),
		newBlock(review.BlockTableRow, 0, 15, 15, "Retries"),
		newBlock(review.BlockCode, 0, 17, 19, "Retries"),
	}
	if got := step.Excerpts[0].Blocks; !reflect.DeepEqual(got, want) {
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

	treatments := treatmentsOf(step.Excerpts[0])
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
	session := openDocumentRound(t, files, step)

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

func TestAnExcerptCuttingIntoAParagraphIsWidenedToItWithTheExtraLinesAsContext(t *testing.T) {
	// A heading on line 1, then a six-line paragraph on lines 3-8. One Step
	// shows the paragraph's middle two lines; another shows the rest.
	files := adding("docs/retries.md", markdown("# Retries", wrapped(2, words("w", 12))))
	middle := showingLines("The middle", "docs/retries.md", review.NewSide, 5, 6)
	rest := showingLines("The rest", "docs/retries.md", review.NewSide, 1, 4)
	rest.Excerpts = append(rest.Excerpts, review.Excerpt{
		Repository: docsRepo, File: "docs/retries.md", Side: review.NewSide, FirstLine: 7, LastLine: 8,
	})

	step := viewingStep(t, files, middle, rest)

	excerpt := step.Excerpts[0]
	// The whole paragraph, its extra lines widened in as context.
	assertRows(t, rowsOf(excerpt), []viewRow{
		{review.NewSide, 3, false, true}, {review.NewSide, 4, false, true},
		{review.NewSide, 5, true, false}, {review.NewSide, 6, true, false},
		{review.NewSide, 7, false, true}, {review.NewSide, 8, false, true},
	})
	for _, line := range excerpt.Lines {
		if line.Block != 1 {
			t.Errorf("expected line %d in the paragraph's block, got block %d", line.Number, line.Block)
		}
	}
	wantBlocks := []review.Block{{Kind: review.BlockParagraph, Side: review.NewSide,
		Lines: review.LineRange{First: 3, Last: 8}, Treatment: review.TreatmentNew, Path: []string{"Retries"}}}
	if !reflect.DeepEqual(excerpt.Blocks, wantBlocks) {
		t.Errorf("expected the paragraph new, as the lines the Excerpt names are\n%+v\ngot\n%+v", wantBlocks, excerpt.Blocks)
	}
}

func TestAWidenedParagraphCanBeAnchoredWhole(t *testing.T) {
	files := adding("docs/retries.md", markdown("# Retries", wrapped(2, words("w", 12))))
	rest := showingLines("The rest", "docs/retries.md", review.NewSide, 1, 4)
	rest.Excerpts = append(rest.Excerpts, review.Excerpt{
		Repository: docsRepo, File: "docs/retries.md", Side: review.NewSide, FirstLine: 7, LastLine: 8,
	})
	session := openDocumentRound(t, files, showingLines("The middle", "docs/retries.md", review.NewSide, 5, 6), rest)

	anchor, err := session.Anchor(review.AnchorTarget{
		Start: review.AnchorEndpoint{Line: 3}, End: review.AnchorEndpoint{Line: 8},
	})

	if err != nil {
		t.Fatalf("expected the widened rows to be anchorable, got %v", err)
	}
	if want := []review.AnchorSegment{{Side: review.NewSide, FirstLine: 3, LastLine: 8}}; !slices.Equal(anchor.Segments, want) {
		t.Errorf("expected the Anchor to carry the whole paragraph %v, got %v", want, anchor.Segments)
	}
}

func TestAnExcerptCuttingIntoATableKeepsItsRowsAndGainsTheHeaderRowAsContext(t *testing.T) {
	// A heading on line 1; a table with its header row on line 3, its divider
	// on 4 and rows on 5-8. One Step shows the heading and the first row; the
	// other shows rows 7-8, and is not widened to the rest of the table.
	files := adding("docs/retries.md", markdown("# Retries", attemptsTable))
	first := showingLines("The first attempt", "docs/retries.md", review.NewSide, 1, 6)
	last := showingLines("The last attempts", "docs/retries.md", review.NewSide, 7, 8)

	view := viewingStep(t, files, last, first)

	excerpt := view.Excerpts[0]
	// The rows shown, with the header row and divider as context.
	assertRows(t, rowsOf(excerpt), []viewRow{
		{review.NewSide, 3, false, true}, {review.NewSide, 4, false, true},
		{review.NewSide, 7, true, false}, {review.NewSide, 8, true, false},
	})
	row := func(first, last int, treatment review.Treatment) review.Block {
		return review.Block{Kind: review.BlockTableRow, Side: review.NewSide,
			Lines: review.LineRange{First: first, Last: last}, Treatment: treatment, Path: []string{"Retries"}}
	}
	wantBlocks := []review.Block{row(3, 4, review.TreatmentContext), row(7, 7, review.TreatmentNew), row(8, 8, review.TreatmentNew)}
	if !reflect.DeepEqual(excerpt.Blocks, wantBlocks) {
		t.Errorf("expected the header row as a block of context before the rows\n%+v\ngot\n%+v", wantBlocks, excerpt.Blocks)
	}
}

func TestAnExcerptShowingATablesHeaderRowGainsNothing(t *testing.T) {
	table := "| Attempt | Delay |\n| ------- | ----- |\n| 1 | none |"
	files := adding("docs/retries.md", markdown("# Retries", table))

	view := viewingStep(t, files, showingWhole("Retries", files, review.NewSide, "docs/retries.md"))

	for _, line := range view.Excerpts[0].Lines {
		if line.Widened {
			t.Errorf("expected nothing widened into an Excerpt showing the whole table, got line %d", line.Number)
		}
	}
}

func TestATextDocumentsBlocksAreItsParagraphsDrawnPlain(t *testing.T) {
	// "# Not a heading" is the first line of a paragraph, not a heading.
	files := adding("docs/spec.txt", plainSpec())
	round := documentRound(showingWhole("The spec", files, review.NewSide, "docs/spec.txt"))
	round.Overrulings = []review.Overruling{documentOverruling("docs/spec.txt", true)}
	session := review.NewSession(files, files)
	mustPost(t, session, round)
	mustAdvance(t, session)

	excerpt := session.View().Step.Excerpts[0]

	if !excerpt.Plain {
		t.Error("expected a text Document's Excerpt to say it is not Markdown")
	}
	var got []review.LineRange
	for _, block := range excerpt.Blocks {
		if block.Kind != review.BlockParagraph {
			t.Errorf("expected only paragraphs in a text Document, got a %s", block.Kind)
		}
		got = append(got, block.Lines)
	}
	if want := []review.LineRange{{First: 1, Last: 2}, {First: 4, Last: 4}}; !slices.Equal(got, want) {
		t.Errorf("expected the text's blank-line paragraphs %v, got %v", want, got)
	}
}

func TestAMarkdownDocumentsExcerptIsNotPlain(t *testing.T) {
	files := adding("docs/retries.md", retriesDocument)

	step := viewingStep(t, files, showingWhole("Retries", files, review.NewSide, "docs/retries.md"))

	if step.Excerpts[0].Plain {
		t.Error("expected a Markdown Document's Excerpt to be drawn as Markdown")
	}
}

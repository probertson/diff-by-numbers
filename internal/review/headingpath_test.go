package review_test

import (
	"slices"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// Each block of a Document Excerpt carries its heading path: the texts of the
// headings it sits under, outermost first, read from the whole Document on the
// block's own side. The Reviewer's surface puts it in the Excerpt's sticky file
// header, so the Reviewer always knows which section a change is in (#145).

// limitsDocument nests a section three headings deep, a long way below its
// outermost heading.
var limitsDocument = markdown(
	"# Retries",             // 1
	"A request is retried.", // 3
	"## Limits",             // 5
	"### Per host",          // 7
	"Each host has a cap.",  // 9
	"The cap is shared.",    // 11
)

// pathOfRow is the heading path of the block a row of an Excerpt is in.
func pathOfRow(t *testing.T, view review.ExcerptView, side review.Side, number int) []string {
	t.Helper()
	for _, line := range view.Lines {
		if line.Side == side && line.Number == number {
			if line.Block == 0 {
				t.Fatalf("expected %v line %d in a block", side, number)
			}
			return view.Blocks[line.Block-1].Path
		}
	}
	t.Fatalf("expected the Excerpt to show %v line %d", side, number)
	return nil
}

func TestABlockDeepInADocumentCarriesEveryHeadingAboveItEvenOutsideTheExcerpt(t *testing.T) {
	files := adding("docs/retries.md", limitsDocument)

	step := viewingStep(t, files,
		showingLines("The shared cap", "docs/retries.md", review.NewSide, 11, 11),
		showingLines("The rest", "docs/retries.md", review.NewSide, 1, 9))

	want := []string{"Retries", "Limits", "Per host"}
	if got := pathOfRow(t, step.Excerpts[0], review.NewSide, 11); !slices.Equal(got, want) {
		t.Errorf("expected the heading path %q, got %q", want, got)
	}
}

func TestAHeadingsOwnBlockCarriesThePathOfItsParents(t *testing.T) {
	files := adding("docs/retries.md", limitsDocument)

	step := viewingStep(t, files, showingWhole("Retries", files, review.NewSide, "docs/retries.md"))

	view := step.Excerpts[0]
	if got, want := pathOfRow(t, view, review.NewSide, 7), []string{"Retries", "Limits"}; !slices.Equal(got, want) {
		t.Errorf("expected the third-level heading under %q, got %q", want, got)
	}
	if got, want := pathOfRow(t, view, review.NewSide, 5), []string{"Retries"}; !slices.Equal(got, want) {
		t.Errorf("expected the second-level heading under %q, got %q", want, got)
	}
	if got := pathOfRow(t, view, review.NewSide, 1); len(got) != 0 {
		t.Errorf("expected the outermost heading under nothing, got %q", got)
	}
}

func TestAHeadingClosesTheSectionsAtItsLevelAndDeeper(t *testing.T) {
	files := adding("docs/retries.md", markdown(
		"# Retries",     // 1
		"## Limits",     // 3
		"### Per host",  // 5
		"## Backoff",    // 7
		"Waits double.", // 9
	))

	step := viewingStep(t, files, showingWhole("Retries", files, review.NewSide, "docs/retries.md"))

	if got, want := pathOfRow(t, step.Excerpts[0], review.NewSide, 9), []string{"Retries", "Backoff"}; !slices.Equal(got, want) {
		t.Errorf("expected the paragraph under %q, got %q", want, got)
	}
}

func TestAHeadingsMarkupIsLeftOutOfItsPath(t *testing.T) {
	files := adding("docs/retries.md", markdown(
		"# Calling `retry()` *safely*",    // 1
		"Setext [limits](limits.md)\n---", // 3-4
		"Each host has a cap.",            // 6
	))

	step := viewingStep(t, files, showingWhole("Retries", files, review.NewSide, "docs/retries.md"))

	want := []string{"Calling retry() safely", "Setext limits"}
	if got := pathOfRow(t, step.Excerpts[0], review.NewSide, 6); !slices.Equal(got, want) {
		t.Errorf("expected the headings' text %q, got %q", want, got)
	}
}

func TestAfterSideBlocksUseTheNewHeadingsAndRemovedOnesTheOld(t *testing.T) {
	// The section's heading is renamed and its paragraph replaced by one that
	// shares none of its words, so the old paragraph is drawn removed.
	files := modifying("docs/retries.md",
		markdown("# Retries", "## Limits", "Old caps applied once."),
		markdown("# Retries", "## Caps", "Brand new ceilings hold."),
	)

	step := viewingStep(t, files, showingLines("Caps", "docs/retries.md", review.NewSide, 3, 5))

	view := step.Excerpts[0]
	if got, want := pathOfRow(t, view, review.NewSide, 5), []string{"Retries", "Caps"}; !slices.Equal(got, want) {
		t.Errorf("expected the new paragraph under the new heading %q, got %q", want, got)
	}
	if got, want := pathOfRow(t, view, review.OldSide, 5), []string{"Retries", "Limits"}; !slices.Equal(got, want) {
		t.Errorf("expected the removed paragraph under the old heading %q, got %q", want, got)
	}
}

func TestADocumentWithoutHeadingsGivesEmptyPaths(t *testing.T) {
	files := adding("docs/retries.md", markdown("A request is retried.", "- once\n- twice"))

	step := viewingStep(t, files, showingWhole("Retries", files, review.NewSide, "docs/retries.md"))

	for _, block := range step.Excerpts[0].Blocks {
		if len(block.Path) != 0 {
			t.Errorf("expected no heading path, got %q on %+v", block.Path, block)
		}
	}
}

func TestADocumentThatIsNotMarkdownGivesEmptyPaths(t *testing.T) {
	files := adding("docs/spec.txt", "# Not a heading\nbut a paragraph.\n\nAnother.\n")
	round := documentRound(showingWhole("The spec", files, review.NewSide, "docs/spec.txt"))
	round.Overrulings = []review.Overruling{documentOverruling("docs/spec.txt", true)}
	session := review.NewSession(files, files)
	mustPost(t, session, round)
	mustAdvance(t, session)

	excerpt := session.View().Step.Excerpts[0]

	if len(excerpt.Blocks) == 0 {
		t.Fatal("expected the text Document drawn as blocks")
	}
	for _, block := range excerpt.Blocks {
		if len(block.Path) != 0 {
			t.Errorf("expected no heading path in prose that is not Markdown, got %q", block.Path)
		}
	}
}

func TestAnExpandedAcknowledgementOverADocumentCarriesHeadingPaths(t *testing.T) {
	files := adding("docs/retries.md", limitsDocument, "retry.go", code(3))
	step := showingWhole("The code", files, review.NewSide, "retry.go")
	step.Acknowledgements = []review.Acknowledgement{{
		Repository: docsRepo, Files: []string{"docs/retries.md"}, Reason: "the agent's notes, read elsewhere",
	}}
	session := openDocumentRound(t, files, step)

	views, err := session.ExpandAcknowledgement(1, 0)

	if err != nil {
		t.Fatalf("expected the Acknowledgement to expand, got %v", err)
	}
	document := expandedFile(views, "docs/retries.md")
	if len(document) == 0 {
		t.Fatal("expected the expansion to show the Document")
	}
	want := []string{"Retries", "Limits", "Per host"}
	if got := pathOfRow(t, document[len(document)-1], review.NewSide, 11); !slices.Equal(got, want) {
		t.Errorf("expected the expansion's heading path %q, got %q", want, got)
	}
}

// rowPathOf is the heading path a row of an Excerpt carries itself: that of its
// text on its own side, which for a row an edit replaced is not its block's.
func rowPathOf(t *testing.T, view review.ExcerptView, side review.Side, number int) []string {
	t.Helper()
	for _, line := range view.Lines {
		if line.Side == side && line.Number == number {
			return line.Path
		}
	}
	t.Fatalf("expected the Excerpt to show %v line %d", side, number)
	return nil
}

func TestTheRowsAnEditReplacedCarryTheOldHeadings(t *testing.T) {
	// The section's heading is renamed and its paragraph edited, so the old
	// paragraph's row is drawn in the new paragraph's block, as track changes.
	files := modifying("docs/retries.md",
		markdown("# Retries", "## Limits", "Each host has a cap of five."),
		markdown("# Retries", "## Caps", "Each host has a cap of six."),
	)

	step := viewingStep(t, files, showingLines("Caps", "docs/retries.md", review.NewSide, 3, 5))

	view := step.Excerpts[0]
	if got, want := rowPathOf(t, view, review.OldSide, 5), []string{"Retries", "Limits"}; !slices.Equal(got, want) {
		t.Errorf("expected the replaced row under the old heading %q, got %q", want, got)
	}
	if got, want := rowPathOf(t, view, review.NewSide, 5), []string{"Retries", "Caps"}; !slices.Equal(got, want) {
		t.Errorf("expected the row that replaced it under the new heading %q, got %q", want, got)
	}
}

func TestARowInNoBlockCarriesThePathOfTheTextAboveItOrElseBelowIt(t *testing.T) {
	// The section's heading is renamed, its first paragraph edited and its
	// second removed, so the old heading's rows, the blank line after it
	// among them, are drawn above the new heading. The Excerpt opens on a
	// blank line with nothing above it.
	files := modifying("docs/retries.md",
		markdown("# Retries", "## Limits", "Each host has a cap of five.", "Old words vanish here.", "Kept para."),
		markdown("# Retries", "## Caps", "Each host has a cap of six.", "Kept para."),
	)

	step := viewingStep(t, files, showingLines("Caps", "docs/retries.md", review.NewSide, 2, 7))

	view := step.Excerpts[0]
	cases := []struct {
		name   string
		side   review.Side
		number int
		want   []string
	}{
		{"the blank line the Excerpt opens on, as the old heading below it", review.NewSide, 2, []string{"Retries"}},
		{"the old blank line after the old heading, as that heading", review.OldSide, 4, []string{"Retries"}},
		{"the blank line after the edited paragraph, as that paragraph", review.NewSide, 6, []string{"Retries", "Caps"}},
	}
	for _, c := range cases {
		if got := rowPathOf(t, view, c.side, c.number); !slices.Equal(got, c.want) {
			t.Errorf("expected %s to carry %q, got %q", c.name, c.want, got)
		}
	}
}

func TestSinceThePreviousRoundReplacedRowsCarryThePreviousRoundsHeadings(t *testing.T) {
	// A Document added in Round 1 has its section renamed and its paragraph
	// edited for Round 2.
	files := newDocumentRounds(adding("docs/retries.md",
		markdown("# Retries", "## Limits", "Each host has a cap of five.")))
	session := review.NewSession(files, files)
	mustPost(t, session, documentRound(showingWhole("Retries", files.workingFiles, review.NewSide, "docs/retries.md")))
	handOffWithAComment(t, session)
	files.after["docs/retries.md"] = markdown("# Retries", "## Caps", "Each host has a cap of six.")
	if err := session.Revise(session.ReviewID(), revising(documentRound(
		showingWhole("Retries, capped", files.workingFiles, review.NewSide, "docs/retries.md")))); err != nil {
		t.Fatal(err)
	}

	mustAdvance(t, session)
	since := session.View().Step.Excerpts[0]

	if got, want := rowPathOf(t, since, review.PreviousSide, 5), []string{"Retries", "Limits"}; !slices.Equal(got, want) {
		t.Errorf("expected the row Round 1 had under its heading %q, got %q", want, got)
	}
	if got, want := rowPathOf(t, since, review.NewSide, 5), []string{"Retries", "Caps"}; !slices.Equal(got, want) {
		t.Errorf("expected the row that replaced it under the new heading %q, got %q", want, got)
	}
}

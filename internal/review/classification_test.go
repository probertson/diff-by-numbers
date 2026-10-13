package review_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// dbn classifies a file as a Document by its type, but only the Authoring Agent
// knows what a file is for: a prose spec in a .txt file is a Document, and a
// generated Markdown changelog arguably is not. These tests overrule dbn's
// classification on describe_changes and post_round, and check what each then
// reports, charges and refuses.

// plainSpec is a prose spec that is not Markdown: a paragraph whose first line
// would be a heading in Markdown, then a paragraph of 8 words.
//
//	1-2  "# Not a heading" and 10 words: 13 words
//	4    8 words
func plainSpec() string {
	return markdown("# Not a heading\n"+paragraph(10), paragraph(8))
}

func documentOverruling(path string, document bool) review.Classification {
	return review.Classification{File: path, Document: document}
}

func TestATextFileOverruledToADocumentIsDescribedWithItsBlankLineParagraphsCostedInWords(t *testing.T) {
	files := adding("docs/spec.txt", plainSpec())

	d, err := review.NewSession(files, files).DescribeChanges(documentRound().ChangeSet, documentOverruling("docs/spec.txt", true))

	if err != nil {
		t.Fatal(err)
	}
	file := fileNamed(t, d, "docs/spec.txt")
	if !file.Document {
		t.Error("expected the overruled file to be flagged a Document")
	}
	// Split at blank lines, not parsed as Markdown: the first line is no heading
	// of its own, so the first paragraph is one block of 13 words.
	want := &review.DocumentCost{Words: 21, NewBlocks: []review.BlockCost{
		{Lines: review.LineRange{First: 1, Last: 2}, Words: 13},
		{Lines: review.LineRange{First: 4, Last: 4}, Words: 8},
	}}
	if !reflect.DeepEqual(file.Cost, want) {
		t.Errorf("expected %+v, got %+v", want, file.Cost)
	}
}

func TestATextFileOverruledToADocumentIsBudgetedInWordsOnPost(t *testing.T) {
	// 400 words over 40 lines: far over the code budget, well within the
	// Document budget.
	files := adding("docs/spec.txt", markdown(paragraph(400)))
	round := documentRound(showingWhole("The spec", files, review.NewSide, "docs/spec.txt"))
	round.Classifications = []review.Classification{documentOverruling("docs/spec.txt", true)}

	err := review.NewSession(files, files).Post(round)

	if err != nil {
		t.Fatalf("expected the overruled file's 400 words to be within the Document budget, got %v", err)
	}
}

func TestAMarkdownFileOverruledToNotADocumentLosesItsFlagAndIsCountedInLines(t *testing.T) {
	// A generated changelog of 400 words over 40 lines.
	files := adding("CHANGELOG.md", markdown(paragraph(400)))
	session := review.NewSession(files, files)
	notADocument := documentOverruling("CHANGELOG.md", false)
	round := documentRound(showingWhole("The changelog", files, review.NewSide, "CHANGELOG.md"))
	round.Classifications = []review.Classification{notADocument}

	d, describeErr := session.DescribeChanges(documentRound().ChangeSet, notADocument)
	postErr := session.Post(round)

	if describeErr != nil {
		t.Fatal(describeErr)
	}
	file := fileNamed(t, d, "CHANGELOG.md")
	if file.Document || file.Cost != nil {
		t.Errorf("expected the changelog described as code, with no cost in words, got document %v, cost %+v", file.Document, file.Cost)
	}
	assertRejected(t, postErr, review.RejectedOversizedStep)
	assertDetailContains(t, postErr, "counts 40 changed lines toward the budget of 30")
}

// specRevised is a review whose first Round made docs/spec.txt, a paragraph of
// 10 words, a Document and was handed off with a Comment. The spec has since
// gained a paragraph of 400 words on lines 3-42, which the Revision Round has to
// show: within the Document budget, but 40 lines of code.
func specRevised(t *testing.T) *review.Session {
	t.Helper()
	files := revisedDocuments{workingFiles: adding("docs/spec.txt", markdown(paragraph(10))), touched: map[string]bool{}}
	session := review.NewSession(files, files)
	first := documentRound(showingWhole("The spec", files.workingFiles, review.NewSide, "docs/spec.txt"))
	first.Classifications = []review.Classification{documentOverruling("docs/spec.txt", true)}
	mustPost(t, session, first)
	handOffWithAComment(t, session)
	files.after["docs/spec.txt"] = markdown(paragraph(10), paragraph(400))
	for n := 2; n <= 42; n++ {
		files.touched[fmt.Sprintf("docs/spec.txt:%d", n)] = true
	}
	return session
}

// showingAddedParagraph is a Revision Round's Step showing the paragraph
// specRevised added.
func showingAddedParagraph() review.Step {
	return review.Step{
		Name: "What the spec now covers", Explanation: "why this Step exists, in the reviewer's terms",
		Excerpts: []review.Excerpt{{Repository: docsRepo, File: "docs/spec.txt", Side: review.NewSide, FirstLine: 3, LastLine: 42}},
	}
}

func TestAnOverrulingHoldsThroughARevisionRoundThatDoesNotRepeatIt(t *testing.T) {
	session := specRevised(t)

	d, describeErr := session.DescribeRevision(session.ReviewID(), documentRound().ChangeSet)
	reviseErr := session.Revise(session.ReviewID(), revising(documentRound(showingAddedParagraph())))

	if describeErr != nil {
		t.Fatal(describeErr)
	}
	if file := fileNamed(t, d, "docs/spec.txt"); !file.Document || file.Cost == nil || file.Cost.Words != 400 {
		t.Errorf("expected the spec still described as a Document costing 400 words, got document %v, cost %+v", file.Document, file.Cost)
	}
	if reviseErr != nil {
		t.Fatalf("expected the spec still budgeted in words, got %v", reviseErr)
	}
}

func TestALaterRoundCanReverseAnOverruling(t *testing.T) {
	session := specRevised(t)
	round := revising(documentRound(showingAddedParagraph()))
	round.Classifications = []review.Classification{documentOverruling("docs/spec.txt", false)}

	err := session.Revise(session.ReviewID(), round)

	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, "counts 40 changed lines toward the budget of 30")
}

func TestAReversedOverrulingHoldsForTheRoundsAfterIt(t *testing.T) {
	session := specRevised(t)
	round := revising(documentRound(showingAddedParagraph()))
	round.Steps[0].OversizeJustification = "the paragraph is one argument"
	round.Classifications = []review.Classification{documentOverruling("docs/spec.txt", false)}
	mustRevise(t, session, round)

	d, err := session.DescribeRevision(session.ReviewID(), documentRound().ChangeSet)

	if err != nil {
		t.Fatal(err)
	}
	if fileNamed(t, d, "docs/spec.txt").Document {
		t.Error("expected the spec described as code once a Round reversed its overruling")
	}
}

func TestDescribeChangesLaysItsOverrulingOverTheReviews(t *testing.T) {
	session := specRevised(t)

	d, err := session.DescribeRevision(session.ReviewID(), documentRound().ChangeSet, documentOverruling("docs/spec.txt", false))

	if err != nil {
		t.Fatal(err)
	}
	if file := fileNamed(t, d, "docs/spec.txt"); file.Document || file.Cost != nil {
		t.Errorf("expected the description's overruling to win over the Review's, got document %v, cost %+v", file.Document, file.Cost)
	}
}

func TestAnOverrulingGivenToDescribeChangesIsNotRecordedOnTheReview(t *testing.T) {
	session := specRevised(t)
	if _, err := session.DescribeRevision(session.ReviewID(), documentRound().ChangeSet, documentOverruling("docs/spec.txt", false)); err != nil {
		t.Fatal(err)
	}

	err := session.Revise(session.ReviewID(), revising(documentRound(showingAddedParagraph())))

	if err != nil {
		t.Fatalf("expected the Review's own overruling to stand after a description overruled it, got %v", err)
	}
}

func TestAFileNobodyOverrulesKeepsDbnsClassification(t *testing.T) {
	files := adding("docs/spec.txt", plainSpec(), "docs/retries.md", markdown(paragraph(10)))

	d, err := review.NewSession(files, files).DescribeChanges(documentRound().ChangeSet, documentOverruling("docs/spec.txt", true))

	if err != nil {
		t.Fatal(err)
	}
	if !fileNamed(t, d, "docs/retries.md").Document {
		t.Error("expected the Markdown file nobody overruled to stay a Document")
	}
}

// withBinary is a Change Set that also changes a binary file.
type withBinary struct {
	workingFiles
	binary string
}

func (w withBinary) Derive(repo review.Repository) (review.Derivation, error) {
	d, err := w.workingFiles.Derive(repo)
	d.Opaque = append(d.Opaque, review.OpaqueChange{Repository: repo.Root, File: w.binary, Kind: review.OpaqueBinary})
	return d, err
}

// classifiedRound is a first Round over a spec and a binary logo, both
// accounted for, with the given classifications.
func classifiedRound(files withBinary, classifications ...review.Classification) review.Round {
	step := showingWhole("The spec", files.workingFiles, review.NewSide, "docs/spec.txt")
	step.Acknowledgements = []review.Acknowledgement{{Repository: docsRepo, Files: []string{files.binary}, Reason: "regenerated image"}}
	round := documentRound(step)
	round.Classifications = classifications
	return round
}

func TestOverrulingAFileOutsideTheChangeSetIsAProblemOnBothTools(t *testing.T) {
	files := withBinary{workingFiles: adding("docs/spec.txt", plainSpec()), binary: "logo.png"}
	session := review.NewSession(files, files)
	typo := documentOverruling("docs/sepc.txt", true)

	_, describeErr := session.DescribeChanges(documentRound().ChangeSet, typo)
	postErr := session.Post(classifiedRound(files, typo))

	for tool, err := range map[string]error{"describe_changes": describeErr, "post_round": postErr} {
		t.Run(tool, func(t *testing.T) {
			assertRejected(t, err, review.RejectedMalformedClassification)
			assertDetailContains(t, err, `"docs/sepc.txt", which is not in the Change Set`)
		})
	}
}

func TestMakingABinaryFileADocumentIsAProblemOnBothTools(t *testing.T) {
	files := withBinary{workingFiles: adding("docs/spec.txt", plainSpec()), binary: "logo.png"}
	session := review.NewSession(files, files)
	binary := documentOverruling("logo.png", true)

	_, describeErr := session.DescribeChanges(documentRound().ChangeSet, binary)
	postErr := session.Post(classifiedRound(files, binary))

	for tool, err := range map[string]error{"describe_changes": describeErr, "post_round": postErr} {
		t.Run(tool, func(t *testing.T) {
			assertRejected(t, err, review.RejectedMalformedClassification)
			assertDetailContains(t, err, `makes "logo.png" a Document, but it is a binary file`)
		})
	}
}

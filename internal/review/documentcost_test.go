package review_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// describe_changes reports what each Document will cost, so an Authoring Agent
// can split its Steps by section without counting words itself. These tests
// describe Change Sets of real Documents and check the costs against what
// posting then charges.

func describingOver(t *testing.T, files workingFiles) review.ChangeDescription {
	t.Helper()
	d, err := review.NewSession(files, files).DescribeChanges(documentRound().ChangeSet)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestANewDocumentIsDescribedWithEachBlocksCostAndItsTotal(t *testing.T) {
	// A heading of 2 words on line 1, a paragraph of 25 on 3-5, and one of 30 on 7-9.
	files := adding("docs/retries.md", markdown("## Retry policy", paragraph(25), paragraph(30)))

	d := describingOver(t, files)

	want := &review.DocumentCost{Words: 57, NewBlocks: []review.BlockCost{
		{Lines: review.LineRange{First: 1, Last: 1}, Words: 2},
		{Lines: review.LineRange{First: 3, Last: 5}, Words: 25},
		{Lines: review.LineRange{First: 7, Last: 9}, Words: 30},
	}}
	if got := fileNamed(t, d, "docs/retries.md").Cost; !reflect.DeepEqual(got, want) {
		t.Errorf("expected %+v, got %+v", want, got)
	}
}

// editedDocument is a Document edited in place, with what git reports of the
// edit: the Changed Lines and the before-side each modification replaced.
type editedDocument struct {
	workingFiles
	lines           []review.ChangedLine
	whitespace      []review.ChangedLine
	correspondences []review.Correspondence
}

func (e editedDocument) Derive(repo review.Repository) (review.Derivation, error) {
	d := review.Derivation{}
	for _, line := range e.lines {
		line.Repository = repo.Root
		d.Lines = append(d.Lines, line)
	}
	for _, line := range e.whitespace {
		line.Repository = repo.Root
		d.Whitespace = append(d.Whitespace, line)
	}
	for _, c := range e.correspondences {
		c.Repository = repo.Root
		d.Correspondences = append(d.Correspondences, c)
	}
	return d, nil
}

func changedRun(path string, side review.Side, first, last int) []review.ChangedLine {
	var out []review.ChangedLine
	for n := first; n <= last; n++ {
		out = append(out, review.ChangedLine{File: path, Side: side, Line: n})
	}
	return out
}

// retriesEdited edits one line in the middle of a paragraph of the given
// number of words and deletes the paragraph after it outright, leaving the
// heading, the paragraph before and the one after alone:
//
//	before                          after
//	1      ## Retry policy          1      ## Retry policy
//	3-4    20 words                 3-4    20 words
//	6-     the edited paragraph     6-     the edited paragraph, its line 7 edited
//	...    20 words, deleted        ...    40 words
//	...    40 words
func retriesEdited(editedWords, deletedWords int) editedDocument {
	const path = "docs/retries.md"
	edited := paragraph(editedWords)
	before := markdown("## Retry policy", paragraph(20), edited, paragraph(deletedWords), paragraph(40))
	after := markdown("## Retry policy", paragraph(20),
		strings.Replace(edited, "\n"+strings.Repeat("prose ", 9)+"prose\n", "\nprose prose prose prose prose revised\n", 1),
		paragraph(40))
	editedLast := 6 + lineCount(edited+"\n") - 1
	deletedFirst := editedLast + 2
	deletedLast := deletedFirst + lineCount(paragraph(deletedWords)+"\n") - 1
	return editedDocument{
		workingFiles: workingFiles{after: map[string]string{path: after}, before: map[string]string{path: before}},
		lines: slices.Concat(
			changedRun(path, review.NewSide, 7, 7),
			changedRun(path, review.OldSide, 7, 7),
			changedRun(path, review.OldSide, deletedFirst, deletedLast+1),
		),
		whitespace:      changedRun(path, review.OldSide, deletedLast+1, deletedLast+1),
		correspondences: []review.Correspondence{{File: path, OldFirst: 7, OldLast: 7, NewFirst: 7, NewLast: 7}},
	}
}

func TestAnEditedDocumentListsOnlyTheBlocksThatCostAnything(t *testing.T) {
	files := retriesEdited(30, 20)

	d, err := review.NewSession(files, files).DescribeChanges(documentRound().ChangeSet)

	if err != nil {
		t.Fatal(err)
	}
	cost := fileNamed(t, d, "docs/retries.md").Cost
	// The edited paragraph is listed on its new side only: its before-side is
	// drawn beside it, not shown on its own. The deleted paragraph is listed on
	// the before-side, where it has to be shown. The heading and the paragraphs
	// nothing changed are left out.
	if got, want := rangesIn(cost.NewBlocks), []review.LineRange{{First: 6, Last: 8}}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected new-side blocks %+v, got %+v", want, got)
	}
	if got, want := rangesIn(cost.OldBlocks), []review.LineRange{{First: 10, Last: 11}}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected old-side blocks %+v, got %+v", want, got)
	}
}

func TestADocumentsDescribedCostIsWhatAStepShowingItsBlocksIsCharged(t *testing.T) {
	// 600 words edited in place and 500 deleted: well over budget together.
	files := retriesEdited(600, 500)
	session := review.NewSession(files, files)
	d, err := session.DescribeChanges(documentRound().ChangeSet)
	if err != nil {
		t.Fatal(err)
	}
	cost := fileNamed(t, d, "docs/retries.md").Cost
	step := review.Step{Name: "The retry policy", Explanation: "why this Step exists, in the reviewer's terms"}
	for _, b := range cost.NewBlocks {
		step.Excerpts = append(step.Excerpts, review.Excerpt{Repository: docsRepo, File: "docs/retries.md", Side: review.NewSide, FirstLine: b.Lines.First, LastLine: b.Lines.Last})
	}
	for _, b := range cost.OldBlocks {
		step.Excerpts = append(step.Excerpts, review.Excerpt{Repository: docsRepo, File: "docs/retries.md", Side: review.OldSide, FirstLine: b.Lines.First, LastLine: b.Lines.Last + 1})
	}

	err = session.Post(documentRound(step))

	if cost.Words <= review.DocumentBudget {
		t.Fatalf("expected the edit to be described as over budget, got %d words", cost.Words)
	}
	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, fmt.Sprintf(`counts %d words of Document text`, cost.Words))
	sum := 0
	for _, b := range slices.Concat(cost.NewBlocks, cost.OldBlocks) {
		sum += b.Words
	}
	if sum != cost.Words {
		t.Errorf("expected the blocks' costs to add up to the total of %d, got %d", cost.Words, sum)
	}
}

// revisedDocuments is a Change Set of Documents across Rounds: a test edits the
// files between them and names, in touched, the lines ("file:line") it moved.
// Everything else maps to where it was, as a snapshot diff would say.
type revisedDocuments struct {
	workingFiles
	touched map[string]bool
}

func (r revisedDocuments) Snapshot(string) (string, error) { return "snapshot", nil }

func (r revisedDocuments) MapBetween(_, _, _ string) (review.RoundMapping, error) {
	return fakeMapping{touched: r.touched}, nil
}

func TestARevisionRoundIsDescribedNetOfWhatWasReadInTheRoundBefore(t *testing.T) {
	// A heading on line 1 and paragraphs on 3-4, 6-7 and 9-10, all shown in the
	// first Round. Then the last paragraph is rewritten in place.
	files := revisedDocuments{
		workingFiles: adding("docs/retries.md", markdown("## Retry policy", paragraph(20), paragraph(15), paragraph(12))),
		touched:      map[string]bool{},
	}
	session := review.NewSession(files, files)
	mustPost(t, session, documentRound(showingWhole("The retry policy", files.workingFiles, review.NewSide, "docs/retries.md")))
	handOffWithAComment(t, session)
	files.after["docs/retries.md"] = markdown("## Retry policy", paragraph(20), paragraph(15), "The rewritten closing paragraph\nruns to ten words in all.")
	files.touched["docs/retries.md:9"], files.touched["docs/retries.md:10"] = true, true

	d, err := session.DescribeRevision(session.ReviewID(), documentRound().ChangeSet)

	if err != nil {
		t.Fatal(err)
	}
	want := &review.DocumentCost{Words: 10, NewBlocks: []review.BlockCost{
		{Lines: review.LineRange{First: 9, Last: 10}, Words: 10},
	}}
	if got := fileNamed(t, d, "docs/retries.md").Cost; !reflect.DeepEqual(got, want) {
		t.Errorf("expected only the rewritten paragraph to cost anything, %+v, got %+v", want, got)
	}
}

func rangesIn(blocks []review.BlockCost) []review.LineRange {
	var out []review.LineRange
	for _, b := range blocks {
		out = append(out, b.Lines)
	}
	return out
}

func TestACodeFileIsDescribedWithNoCost(t *testing.T) {
	files := adding("docs/retries.md", markdown("## Retry policy"), "retry.go", code(5))

	d := describingOver(t, files)

	if got := fileNamed(t, d, "retry.go").Cost; got != nil {
		t.Errorf("expected code to carry no cost in words, got %+v", got)
	}
}

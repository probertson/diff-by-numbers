package review_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// Document text is charged in words against a budget of its own, because a page
// of prose a Reviewer reads comfortably runs far past the 30 lines a Step of code
// may show. These tests post Rounds over real Markdown, so what dbn charges is
// what its parsing and counting make of an actual Document.

const docsRepo = "/repos/docs"

// workingFiles is a Change Set's files as a Round holds them: each file's lines
// after the change and before it. A file only in after was added; a file only in
// before was deleted.
type workingFiles struct {
	after, before map[string]string
}

func (w workingFiles) side(side review.Side) map[string]string {
	if side == review.OldSide {
		return w.before
	}
	return w.after
}

func (w workingFiles) Resolve(e review.Excerpt, round review.RoundSource) ([]review.Line, error) {
	all, err := w.ReadFile(e.Repository, e.File, e.Side, round)
	if err != nil {
		return nil, err
	}
	if e.LastLine > len(all) {
		return nil, fmt.Errorf("%s has %d lines", e.File, len(all))
	}
	var out []review.Line
	for n := e.FirstLine; n <= e.LastLine; n++ {
		out = append(out, review.Line{Number: n, Text: all[n-1], Side: e.Side})
	}
	return out, nil
}

func (w workingFiles) ReadFile(_, file string, side review.Side, _ review.RoundSource) ([]string, error) {
	content, ok := w.side(side)[file]
	if !ok {
		return nil, fmt.Errorf("no %s side of %s", side, file)
	}
	return strings.Split(strings.TrimSuffix(content, "\n"), "\n"), nil
}

// Derive reports every line of an added file as new and every line of a deleted
// one as removed, with the blank ones marked whitespace-only, as git would.
func (w workingFiles) Derive(repo review.Repository) (review.Derivation, error) {
	var d review.Derivation
	derive := func(files map[string]string, side review.Side) {
		for file, content := range files {
			for i, text := range strings.Split(strings.TrimSuffix(content, "\n"), "\n") {
				line := review.ChangedLine{Repository: repo.Root, File: file, Side: side, Line: i + 1}
				d.Lines = append(d.Lines, line)
				if strings.TrimSpace(text) == "" {
					d.Whitespace = append(d.Whitespace, line)
				}
			}
		}
	}
	added, deleted := map[string]string{}, map[string]string{}
	for file, content := range w.after {
		if _, ok := w.before[file]; !ok {
			added[file] = content
			d.Added = append(d.Added, file)
		}
	}
	for file, content := range w.before {
		if _, ok := w.after[file]; !ok {
			deleted[file] = content
			d.Deleted = append(d.Deleted, file)
		}
	}
	derive(added, review.NewSide)
	derive(deleted, review.OldSide)
	for file, after := range w.after {
		if before, ok := w.before[file]; ok && before != after {
			deriveEdits(&d, repo.Root, file, before, after)
		}
	}
	// A constant base, so a Revision Round finds old-side lines where it left
	// them.
	d.Base = "base"
	return d, nil
}

// deriveEdits diffs a modified file line by line, as git would, reporting each
// removed and added line as changed and each run of them as one edit.
func deriveEdits(d *review.Derivation, root, file, before, after string) {
	beforeLines := strings.Split(strings.TrimSuffix(before, "\n"), "\n")
	afterLines := strings.Split(strings.TrimSuffix(after, "\n"), "\n")
	encoded := map[string]rune{}
	encode := func(lines []string) []rune {
		out := make([]rune, len(lines))
		for i, line := range lines {
			if _, ok := encoded[line]; !ok {
				encoded[line] = rune(0xF0000 + len(encoded))
			}
			out[i] = encoded[line]
		}
		return out
	}
	diffs := diffmatchpatch.New().DiffMainRunes(encode(beforeLines), encode(afterLines), false)

	var edit review.Correspondence
	flush := func() {
		if edit.OldFirst > 0 || edit.NewFirst > 0 {
			d.Correspondences = append(d.Correspondences, edit)
		}
		edit = review.Correspondence{Repository: root, File: file}
	}
	flush()
	changed := func(side review.Side, n int, text string) {
		line := review.ChangedLine{Repository: root, File: file, Side: side, Line: n}
		d.Lines = append(d.Lines, line)
		if strings.TrimSpace(text) == "" {
			d.Whitespace = append(d.Whitespace, line)
		}
	}
	oldAt, newAt := 1, 1
	for _, diff := range diffs {
		n := len([]rune(diff.Text))
		switch diff.Type {
		case diffmatchpatch.DiffEqual:
			flush()
			oldAt += n
			newAt += n
		case diffmatchpatch.DiffDelete:
			if edit.OldFirst == 0 {
				edit.OldFirst = oldAt
			}
			for range n {
				changed(review.OldSide, oldAt, beforeLines[oldAt-1])
				edit.OldLast = oldAt
				oldAt++
			}
		case diffmatchpatch.DiffInsert:
			if edit.NewFirst == 0 {
				edit.NewFirst = newAt
			}
			for range n {
				changed(review.NewSide, newAt, afterLines[newAt-1])
				edit.NewLast = newAt
				newAt++
			}
		}
	}
	flush()
}

// adding is a Change Set that adds the given files, path then content.
func adding(pathsAndContents ...string) workingFiles {
	w := workingFiles{after: map[string]string{}, before: map[string]string{}}
	for i := 0; i < len(pathsAndContents); i += 2 {
		w.after[pathsAndContents[i]] = pathsAndContents[i+1]
	}
	return w
}

// paragraph is n words of prose hard-wrapped ten words to a line, as the
// Documents dbn was measured against are.
func paragraph(n int) string {
	var lines []string
	for n > 0 {
		width := min(n, 10)
		lines = append(lines, strings.TrimSpace(strings.Repeat("prose ", width)))
		n -= width
	}
	return strings.Join(lines, "\n")
}

// markdown joins blocks into a Document, a blank line between each.
func markdown(blocks ...string) string {
	return strings.Join(blocks, "\n\n") + "\n"
}

// lineCount is how many lines a file's content has.
func lineCount(content string) int {
	return strings.Count(content, "\n")
}

// showingWhole is a Step showing the whole of one side of each named file.
func showingWhole(name string, files workingFiles, side review.Side, paths ...string) review.Step {
	step := review.Step{Name: name, Explanation: "why this Step exists, in the reviewer's terms"}
	for _, path := range paths {
		step.Excerpts = append(step.Excerpts, review.Excerpt{
			Repository: docsRepo, File: path, Side: side,
			FirstLine: 1, LastLine: lineCount(files.side(side)[path]),
		})
	}
	return step
}

// documentRound is a first Round of the given Steps over the docs repository.
func documentRound(steps ...review.Step) review.Round {
	return review.Round{
		Label:     "document budget",
		Brief:     review.Brief{Goal: "Write down how retries work", Background: "Nobody could say when a request is retried"},
		ChangeSet: review.ChangeSet{Repositories: []review.Repository{{Root: docsRepo, Base: "main"}}},
		Steps:     steps,
	}
}

func postingOver(files workingFiles, steps ...review.Step) error {
	return review.NewSession(files, files).Post(documentRound(steps...))
}

func TestANewSectionOfAThousandWordsIsAcceptedHoweverManyLinesItSpans(t *testing.T) {
	// 1 word of heading and nine paragraphs of 111: 1,000 words over 118 lines.
	var blocks = []string{"## Background"}
	for range 9 {
		blocks = append(blocks, paragraph(111))
	}
	files := adding("docs/retries.md", markdown(blocks...))

	err := postingOver(files, showingWhole("Why retries exist", files, review.NewSide, "docs/retries.md"))

	if err != nil {
		t.Fatalf("expected a Step of 1,000 new words to be within the Document budget, got %v", err)
	}
}

// overBudgetSection is a new Document of 1,001 words: a heading of 1 and ten
// paragraphs of 100.
func overBudgetSection() workingFiles {
	blocks := []string{"## Background"}
	for range 10 {
		blocks = append(blocks, paragraph(100))
	}
	return adding("docs/retries.md", markdown(blocks...))
}

func TestAStepOfMoreThanAThousandNewWordsIsRefusedWithItsCostInWords(t *testing.T) {
	files := overBudgetSection()

	err := postingOver(files, showingWhole("Why retries exist", files, review.NewSide, "docs/retries.md"))

	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, `Step 1 ("Why retries exist") counts 1001 words of Document text toward the budget of 1000, and gives no justification`)
	assertDetailOmits(t, err, "changed lines toward")
}

// markedUp is a new Document of 116 words of markup — frontmatter, a heading, a
// table, a list, a block quote, a thematic break and a fenced code block — and a
// paragraph of the given number of words. Its bare markers (`---`, `#`, `|`,
// `-`, `>`, the fences) number over a hundred, so counting any kind of them would
// show.
func markedUp(paragraphWords int) workingFiles {
	table := []string{"| Term | Meaning |", "|---|---|"}
	for range 29 {
		table = append(table, "| term | meaning |")
	}
	return adding("docs/retries.md", markdown(
		"---\ntitle: Retries\n---", // 2 words
		"# Retry policy",           // 2
		strings.Join(table, "\n"),  // 60
		"- "+paragraph(10)+"\n- "+paragraph(10)+"\n- "+paragraph(10), // 30
		"> "+paragraph(10)+"\n> "+paragraph(10),                      // 20
		"---",                              // 0
		"```\nretry := policy.Next()\n```", // 2
		paragraph(paragraphWords),
	))
}

func TestBareMarkdownMarkersAreNotCountedAsWords(t *testing.T) {
	files := markedUp(884)

	err := postingOver(files, showingWhole("The retry policy", files, review.NewSide, "docs/retries.md"))

	if err != nil {
		t.Fatalf("expected 1,000 words with their markers left uncounted to be within budget, got %v", err)
	}
}

func TestEveryWordAroundTheMarkersIsCounted(t *testing.T) {
	files := markedUp(885)

	err := postingOver(files, showingWhole("The retry policy", files, review.NewSide, "docs/retries.md"))

	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, "counts 1001 words of Document text")
}

func TestAJustifiedStepOfMoreThanAThousandWordsIsAccepted(t *testing.T) {
	files := overBudgetSection()
	step := showingWhole("Why retries exist", files, review.NewSide, "docs/retries.md")
	step.OversizeJustification = "the argument only holds read whole"

	err := postingOver(files, step)

	if err != nil {
		t.Fatalf("expected a justified Step to be accepted, got %v", err)
	}
}

// code is a new file of n lines of code.
func code(n int) string {
	var lines []string
	for i := range n {
		lines = append(lines, fmt.Sprintf("\tattempt%d := retry()", i))
	}
	return strings.Join(lines, "\n") + "\n"
}

// section is a new Document of a 1-word heading and paragraphs of 100 words
// making up the given number, which must be one more than a multiple of 100.
func section(words int) string {
	blocks := []string{"## Background"}
	for range words / 100 {
		blocks = append(blocks, paragraph(100))
	}
	return markdown(blocks...)
}

func TestAMixedStepWithinBothBudgetsIsAccepted(t *testing.T) {
	// The Document's 1,000 words run to 118 lines, which would be far over the
	// code budget if they were counted as lines beside the 30 of code.
	files := adding("docs/retries.md", section(901), "retry.go", code(30))

	err := postingOver(files, showingWhole("Retries, described and built", files, review.NewSide, "docs/retries.md", "retry.go"))

	if err != nil {
		t.Fatalf("expected each part of a mixed Step to be judged by its own budget, got %v", err)
	}
}

func TestAMixedStepIsRefusedWhenItsCodeAloneIsOverBudget(t *testing.T) {
	files := adding("docs/retries.md", section(501), "retry.go", code(31))

	err := postingOver(files, showingWhole("Retries, described and built", files, review.NewSide, "docs/retries.md", "retry.go"))

	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, `Step 1 ("Retries, described and built") counts 31 changed lines toward the budget of 30, and gives no justification`)
	assertDetailOmits(t, err, "words of Document text")
}

func TestAMixedStepIsRefusedWhenItsDocumentTextAloneIsOverBudget(t *testing.T) {
	files := adding("docs/retries.md", section(1001), "retry.go", code(10))

	err := postingOver(files, showingWhole("Retries, described and built", files, review.NewSide, "docs/retries.md", "retry.go"))

	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, `Step 1 ("Retries, described and built") counts 1001 words of Document text toward the budget of 1000, and gives no justification`)
	assertDetailOmits(t, err, "changed lines toward")
}

func TestAMixedStepOverBothBudgetsIsRefusedForBoth(t *testing.T) {
	files := adding("docs/retries.md", section(1101), "retry.go", code(31))

	err := postingOver(files, showingWhole("Retries, described and built", files, review.NewSide, "docs/retries.md", "retry.go"))

	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, "counts 31 changed lines toward the budget of 30 and 1101 words of Document text toward the budget of 1000")
}

func TestOneRefusalListsEveryOversizedStepInWordsForDocumentsAndLinesForCode(t *testing.T) {
	files := adding(
		"docs/retries.md", section(1001),
		"retry.go", code(31),
		"docs/backoff.md", section(1201), "backoff.go", code(40),
		"docs/limits.md", section(301),
	)

	err := postingOver(files,
		showingWhole("Why retries exist", files, review.NewSide, "docs/retries.md"),
		showingWhole("The retrier", files, review.NewSide, "retry.go"),
		showingWhole("Backoff", files, review.NewSide, "docs/backoff.md", "backoff.go"),
		showingWhole("Limits", files, review.NewSide, "docs/limits.md"),
	)

	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, `3 Steps are over budget (30 changed lines of code, 1000 words of Document text) and give no justification:
  Step 1 ("Why retries exist") counts 1001 words
  Step 2 ("The retrier") counts 31 changed lines
  Step 3 ("Backoff") counts 40 changed lines and 1201 words
`)
	assertDetailContains(t, err, "Document text costs every word of each changed block a Step's Excerpts touch, "+
		"with the words struck from it; a block whose words did not change, as in a rewrap, costs nothing")
	assertDetailOmits(t, err, "Limits")
}

// showingLines is a Step showing one range of one side of a file.
func showingLines(name, path string, side review.Side, first, last int) review.Step {
	return review.Step{
		Name:        name,
		Explanation: "why this Step exists, in the reviewer's terms",
		Excerpts: []review.Excerpt{{
			Repository: docsRepo, File: path, Side: side, FirstLine: first, LastLine: last,
		}},
	}
}

func TestAnExcerptCuttingIntoAParagraphIsChargedTheWholeParagraph(t *testing.T) {
	// A 1-word heading on line 1, then one paragraph of 1,100 words on lines
	// 3-112. Each Step shows only part of the paragraph, but reading part of a
	// paragraph means reading it, so each is charged all of it.
	files := adding("docs/retries.md", markdown("## Background", paragraph(1100)))

	err := postingOver(files,
		showingLines("The opening", "docs/retries.md", review.NewSide, 1, 50),
		showingLines("The rest", "docs/retries.md", review.NewSide, 51, 112),
	)

	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, `Step 1 ("The opening") counts 1101 words`)
	assertDetailContains(t, err, `Step 2 ("The rest") counts 1100 words`)
}

func TestAListOrATableIsChargedByTheItemsAndRowsAStepShows(t *testing.T) {
	// Each Step shows half of a list or a table of 2,000 words or so. Were the
	// whole list or table one block, each half would be charged all of it.
	var items, rows []string
	for range 20 {
		item := strings.ReplaceAll(paragraph(100), "\n", "\n  ")
		items = append(items, "- "+item) // 100 words over 10 lines
	}
	rows = append(rows, "| Attempt | Note |", "|---|---|")
	for range 150 {
		rows = append(rows, "| one two three four five | six seven eight nine ten |") // 10 words
	}
	for _, tc := range []struct {
		name     string
		document string
		halfway  int
	}{
		{"list", markdown(strings.Join(items, "\n")), 100},    // items 1-10: 1,000 words
		{"table", markdown(strings.Join(rows, "\n")), 2 + 98}, // header and 98 rows: 982 words
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := adding("docs/retries.md", tc.document)

			err := postingOver(files,
				showingLines("The first half", "docs/retries.md", review.NewSide, 1, tc.halfway),
				showingLines("The second half", "docs/retries.md", review.NewSide, tc.halfway+1, lineCount(tc.document)),
			)

			if err != nil {
				t.Fatalf("expected each half to be charged only what it shows, got %v", err)
			}
		})
	}
}

func TestDeletedDocumentTextIsChargedInWordsToo(t *testing.T) {
	files := workingFiles{
		after:  map[string]string{},
		before: map[string]string{"docs/retired.md": section(1101)},
	}

	err := postingOver(files, showingWhole("Retire the old notes", files, review.OldSide, "docs/retired.md"))

	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, `Step 1 ("Retire the old notes") counts 1101 words of Document text`)
	assertDetailOmits(t, err, "changed lines toward")
}

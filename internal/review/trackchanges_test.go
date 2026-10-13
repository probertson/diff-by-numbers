package review_test

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// An edit to a Document is read in place, as track changes: dbn's Step view
// gives each changed block the treatment its run of changes earns (edited, new
// or removed), and gives an edited block's rows the merged flow of what the
// edit kept, struck and added, so the Reviewer's surface draws it without
// diffing anything. These tests post Rounds over real Markdown and read what
// the Step view says.

// trackedRuns is what an Excerpt's rows in one block say the edit did, in the
// order they are drawn.
func trackedRuns(view review.ExcerptView, block int) []review.WordRun {
	var out []review.WordRun
	for _, line := range view.Lines {
		if line.Block == block {
			out = append(out, line.Runs...)
		}
	}
	return out
}

func kept(text string) review.WordRun { return review.WordRun{Text: text, Change: review.WordsKept} }
func struck(text string) review.WordRun {
	return review.WordRun{Text: text, Change: review.WordsRemoved}
}
func inserted(text string) review.WordRun {
	return review.WordRun{Text: text, Change: review.WordsAdded}
}

func treatmentsOf(view review.ExcerptView) []review.Treatment {
	var out []review.Treatment
	for _, block := range view.Blocks {
		out = append(out, block.Treatment)
	}
	return out
}

func TestAnEditedParagraphCarriesTheMergedFlowOfItsKeptStruckAndAddedWords(t *testing.T) {
	files := modifying("docs/retries.md",
		markdown("# Retries", "A request is retried when the transport fails."),
		markdown("# Retries", "A request is resent when the transport fails."),
	)

	step := viewingStep(t, files, showingLines("Resend", "docs/retries.md", review.NewSide, 3, 3))

	view := step.Excerpts[0]
	want := []review.Block{{Kind: review.BlockParagraph, Side: review.NewSide,
		Lines: review.LineRange{First: 3, Last: 3}, Treatment: review.TreatmentEdited, Path: []string{"Retries"}}}
	if !reflect.DeepEqual(view.Blocks, want) {
		t.Fatalf("expected the paragraph edited\n%+v\ngot\n%+v", want, view.Blocks)
	}
	runs := []review.WordRun{kept("A request is "), struck("retried"), inserted("resent"), kept(" when the transport fails.")}
	if got := trackedRuns(view, 1); !slices.Equal(got, runs) {
		t.Errorf("expected the merged flow\n%+v\ngot\n%+v", runs, got)
	}
}

func TestAWordStruckAtTheEndOfALineStaysOnItAndTheBlocksOtherRowsReadAsKept(t *testing.T) {
	files := modifying("docs/retries.md",
		markdown("# Retries", "A request is retried\nwhen the transport fails badly", "## Limits"),
		markdown("# Retries", "A request is retried\nwhen the transport fails", "## Limits"),
	)

	step := viewingStep(t, files, showingLines("Soften", "docs/retries.md", review.NewSide, 3, 4))

	view := step.Excerpts[0]
	rows := map[int][]review.WordRun{}
	for _, line := range view.Lines {
		if line.Side == review.NewSide {
			rows[line.Number] = line.Runs
		}
	}
	if want := []review.WordRun{kept("A request is retried")}; !slices.Equal(rows[3], want) {
		t.Errorf("expected the untouched row read as kept, got %+v", rows[3])
	}
	if want := []review.WordRun{kept("when the transport fails"), struck("badly")}; !slices.Equal(rows[4], want) {
		t.Errorf("expected the struck word at the end of its row, got %+v", rows[4])
	}
}

// documentRounds is a Change Set of Documents a test edits between Rounds. Each
// Round's snapshot keeps the after-side as it was posted, and a Round is mapped
// to an earlier one by a line diff of the two, as a snapshot diff would.
type documentRounds struct {
	workingFiles
	snapshots map[string]map[string]string
}

func newDocumentRounds(files workingFiles) documentRounds {
	return documentRounds{workingFiles: files, snapshots: map[string]map[string]string{}}
}

func (r documentRounds) Snapshot(string) (string, error) {
	id := fmt.Sprintf("snapshot-%d", len(r.snapshots)+1)
	r.snapshots[id] = maps.Clone(r.after)
	return id, nil
}

func (r documentRounds) ReadFile(repo, file string, side review.Side, round review.RoundSource) ([]string, error) {
	snapshot, ok := r.snapshots[round.Snapshots[repo]]
	if side == review.OldSide || !ok {
		return r.workingFiles.ReadFile(repo, file, side, round)
	}
	content, ok := snapshot[file]
	if !ok {
		return nil, fmt.Errorf("no %s in %s", file, round.Snapshots[repo])
	}
	return strings.Split(strings.TrimSuffix(content, "\n"), "\n"), nil
}

func (r documentRounds) Resolve(e review.Excerpt, round review.RoundSource) ([]review.Line, error) {
	all, err := r.ReadFile(e.Repository, e.File, e.Side, round)
	if err != nil {
		return nil, err
	}
	var out []review.Line
	for n := e.FirstLine; n <= min(e.LastLine, len(all)); n++ {
		out = append(out, review.Line{Number: n, Text: all[n-1], Side: e.Side})
	}
	return out, nil
}

func (r documentRounds) MapBetween(_, from, to string) (review.RoundMapping, error) {
	mapping := fakeMapping{touched: map[string]bool{}, edits: map[string][]review.RoundEdit{}}
	for file, after := range r.snapshots[to] {
		var d review.Derivation
		deriveEdits(&d, docsRepo, file, r.snapshots[from][file], after)
		for _, c := range d.Correspondences {
			edit := review.RoundEdit{OldFirst: c.OldFirst, NewFirst: c.NewFirst}
			if c.OldFirst > 0 {
				edit.OldCount = c.OldLast - c.OldFirst + 1
			}
			if c.NewFirst > 0 {
				edit.NewCount = c.NewLast - c.NewFirst + 1
			}
			mapping.edits[file] = append(mapping.edits[file], edit)
			mapping.touched[file] = true
			for n := c.NewFirst; c.NewFirst > 0 && n <= c.NewLast; n++ {
				mapping.touched[fmt.Sprintf("%s:%d", file, n)] = true
			}
		}
	}
	return mapping, nil
}

func TestARevisionRoundsTrackChangesShowOnlyWhatChangedSinceThePreviousRound(t *testing.T) {
	// A Document added in Round 1, its paragraph edited for Round 2.
	files := newDocumentRounds(adding("docs/retries.md",
		markdown("# Retries", "A request is retried when the transport fails.")))
	session := review.NewSession(files, files)
	mustPost(t, session, documentRound(showingWhole("Retries", files.workingFiles, review.NewSide, "docs/retries.md")))
	handOffWithAComment(t, session)
	files.after["docs/retries.md"] = markdown("# Retries", "A request is resent when the transport fails.")
	if err := session.Revise(session.ReviewID(), revising(documentRound(
		showingWhole("Retries, resent", files.workingFiles, review.NewSide, "docs/retries.md")))); err != nil {
		t.Fatal(err)
	}

	mustAdvance(t, session)
	since := session.View().Step.Excerpts[0]
	if err := session.ToggleSincePreviousRound(); err != nil {
		t.Fatal(err)
	}
	whole := session.View().Step.Excerpts[0]

	if got, want := treatmentsOf(since), []review.Treatment{review.TreatmentContext, review.TreatmentEdited}; !slices.Equal(got, want) {
		t.Errorf("expected the untouched heading as context and the paragraph edited since Round 1, got %v", got)
	}
	runs := []review.WordRun{kept("A request is "), struck("retried"), inserted("resent"), kept(" when the transport fails.")}
	if got := trackedRuns(since, 2); !slices.Equal(got, runs) {
		t.Errorf("expected the edit since Round 1\n%+v\ngot\n%+v", runs, got)
	}
	if got, want := treatmentsOf(whole), []review.Treatment{review.TreatmentNew, review.TreatmentNew}; !slices.Equal(got, want) {
		t.Errorf("expected every change under review shown new against the merge-base, got %v", got)
	}
}

func TestADeletionShownOnItsOwnIsARemovedBlock(t *testing.T) {
	files := modifying("docs/retries.md",
		markdown("# Retries", "A request is retried.", "Old limits applied\nonce a day."),
		markdown("# Retries", "A request is retried."),
	)

	step := viewingStep(t, files, showingLines("Drop the old limits", "docs/retries.md", review.OldSide, 5, 6))

	view := step.Excerpts[0]
	want := []review.Block{{Kind: review.BlockParagraph, Side: review.OldSide,
		Lines: review.LineRange{First: 5, Last: 6}, Treatment: review.TreatmentRemoved, Path: []string{"Retries"}}}
	if !reflect.DeepEqual(view.Blocks, want) {
		t.Fatalf("expected the deleted paragraph removed\n%+v\ngot\n%+v", want, view.Blocks)
	}
	for _, line := range view.Lines {
		if line.Number >= 5 && line.Block != 1 {
			t.Errorf("expected every row of the paragraph in the removed block, got %+v", line)
		}
	}
}

func TestAnEditedParagraphWidenedToWholeKeepsTheReplacedLineAboveItsReplacement(t *testing.T) {
	files := modifying("docs/retries.md",
		markdown("# Retries", "A request\nis retried when\nthe transport fails."),
		markdown("# Retries", "A request\nis resent when\nthe transport fails."),
	)

	step := viewingStep(t, files, showingLines("Resend", "docs/retries.md", review.NewSide, 4, 4))

	type row struct {
		side    review.Side
		number  int
		widened bool
		runs    bool
	}
	var got []row
	for _, line := range step.Excerpts[0].Lines {
		got = append(got, row{line.Side, line.Number, line.Widened, len(line.Runs) > 0})
	}
	want := []row{
		{review.NewSide, 3, true, false},
		{review.OldSide, 4, false, false},
		{review.NewSide, 4, false, true},
		{review.NewSide, 5, true, false},
	}
	if !slices.Equal(got, want) {
		t.Errorf("expected the paragraph whole, its replaced line above its replacement\n%+v\ngot\n%+v", want, got)
	}
	if got := treatmentsOf(step.Excerpts[0]); !slices.Equal(got, []review.Treatment{review.TreatmentEdited}) {
		t.Errorf("expected one edited block, got %v", got)
	}
}

func TestAWhollyNewRunBesideAnEditIsNewAndARunWithNothingAfterItIsRemoved(t *testing.T) {
	// The first paragraph is edited, and the second is replaced by one that
	// shares none of its words.
	files := modifying("docs/retries.md",
		markdown("# Retries", "A request is retried when the transport fails.", "Old limits applied once."),
		markdown("# Retries", "A request is resent when the transport fails.", "Brand new caps hold."),
	)

	step := viewingStep(t, files, showingLines("Resend", "docs/retries.md", review.NewSide, 3, 5))

	view := step.Excerpts[0]
	want := []review.Block{
		{Kind: review.BlockParagraph, Side: review.NewSide, Lines: review.LineRange{First: 3, Last: 3}, Treatment: review.TreatmentEdited,
			Path: []string{"Retries"}},
		{Kind: review.BlockParagraph, Side: review.OldSide, Lines: review.LineRange{First: 5, Last: 5}, Treatment: review.TreatmentRemoved,
			Path: []string{"Retries"}},
		{Kind: review.BlockParagraph, Side: review.NewSide, Lines: review.LineRange{First: 5, Last: 5}, Treatment: review.TreatmentNew,
			Path: []string{"Retries"}},
	}
	if !reflect.DeepEqual(view.Blocks, want) {
		t.Fatalf("expected an edited, a removed and a new block\n%+v\ngot\n%+v", want, view.Blocks)
	}
	if runs := trackedRuns(view, 2); len(runs) != 0 {
		t.Errorf("expected the removed block to carry no runs, got %+v", runs)
	}
	if runs := trackedRuns(view, 3); len(runs) != 0 {
		t.Errorf("expected the new block to carry no runs, got %+v", runs)
	}
}

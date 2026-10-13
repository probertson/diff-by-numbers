package daemon

import (
	"slices"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/intraline"
	"github.com/probertson/diff-by-numbers/internal/review"
)

func TestToViewWireCarriesConcluded(t *testing.T) {
	if !toViewWire(review.ViewModel{Concluded: true}).Concluded {
		t.Error("toViewWire should carry a concluded review through to the wire")
	}
	if toViewWire(review.ViewModel{Concluded: false}).Concluded {
		t.Error("toViewWire should not invent a concluded state")
	}
}

func TestAStepsExcerptsCarryWhetherTheirFileChangedOnDisk(t *testing.T) {
	view := review.ViewModel{Step: &review.StepView{Excerpts: []review.ExcerptView{
		{Excerpt: review.Excerpt{File: "edited.ts"}, ChangedOnDisk: true},
		{Excerpt: review.Excerpt{File: "untouched.ts"}},
	}}}

	excerpts := toViewWire(view).Step.Excerpts

	if !excerpts[0].ChangedOnDisk || excerpts[1].ChangedOnDisk {
		t.Errorf("expected only edited.ts flagged, got %+v", excerpts)
	}
}

func TestAnExpansionCarriesWhetherItsFileChangedOnDisk(t *testing.T) {
	views := []review.ExcerptView{{Excerpt: review.Excerpt{File: "gen.ts"}, ChangedOnDisk: true}}

	wires := toExcerptWires(views)

	if !wires[0].ChangedOnDisk {
		t.Error("expected the expanded file flagged as changed on disk")
	}
}

func TestARowCarriesItsEmphasisAsRunePairs(t *testing.T) {
	line := review.Line{Number: 2, Text: "retry(transport, 5)", Side: review.NewSide, Changed: true,
		Emphasis: []intraline.Range{{Start: 17, End: 18}}}

	wire := toLineWire(line)

	if len(wire.Emphasis) != 1 || wire.Emphasis[0] != [2]int{17, 18} {
		t.Errorf("expected [[17 18]], got %v", wire.Emphasis)
	}
}

func TestADocumentExcerptCarriesItsBlocksAndEachRowsBlock(t *testing.T) {
	views := []review.ExcerptView{{
		Excerpt: review.Excerpt{File: "docs/retries.md", Side: review.NewSide, FirstLine: 1, LastLine: 3},
		Lines: []review.Line{
			{Number: 1, Text: "# Retries", Side: review.NewSide, Changed: true, Block: 1},
			{Number: 2, Text: "", Side: review.NewSide, Changed: true},
			{Number: 3, Text: "A request is retried.", Side: review.NewSide, Changed: true, Block: 2},
		},
		Blocks: []review.Block{
			{Kind: review.BlockHeading, Level: 1, Side: review.NewSide, Lines: review.LineRange{First: 1, Last: 1}, Treatment: review.TreatmentNew},
			{Kind: review.BlockParagraph, Side: review.NewSide, Lines: review.LineRange{First: 3, Last: 3}, Treatment: review.TreatmentContext},
		},
	}}

	wire := toExcerptWires(views)[0]

	want := []BlockWire{
		{Kind: "heading", Level: 1, Side: "new", FirstLine: 1, LastLine: 1, Treatment: "new"},
		{Kind: "paragraph", Side: "new", FirstLine: 3, LastLine: 3, Treatment: "context"},
	}
	if !slices.Equal(wire.Blocks, want) {
		t.Errorf("expected the blocks %+v, got %+v", want, wire.Blocks)
	}
	if got := []int{wire.Lines[0].Block, wire.Lines[1].Block, wire.Lines[2].Block}; !slices.Equal(got, []int{1, 0, 2}) {
		t.Errorf("expected each row to name its block, got %v", got)
	}
}

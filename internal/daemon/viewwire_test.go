package daemon

import (
	"testing"

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

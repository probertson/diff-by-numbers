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

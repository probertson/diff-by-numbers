package review_test

import (
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// A Document's Mermaid diagrams are marked in the Step view, so the Browser
// Renderer can draw them as pictures (#191). dbn only marks them: it never
// parses Mermaid.

// architectureDocument is a new Markdown Document with a Mermaid diagram and a
// fence of code beside it.
const architectureDocument = "# Architecture\n" +
	"\n" +
	"```mermaid\n" +
	"flowchart LR\n" +
	"  tui --> daemon\n" +
	"```\n" +
	"\n" +
	"```go\n" +
	"serve()\n" +
	"```\n"

func TestAMermaidFenceIsMarkedAsADiagramAndOtherFencesAreNot(t *testing.T) {
	files := adding("docs/architecture.md", architectureDocument)

	step := viewingStep(t, files, showingWhole("Architecture", files, review.NewSide, "docs/architecture.md"))

	blocks := step.Excerpts[0].Blocks
	if len(blocks) != 3 {
		t.Fatalf("expected a heading and two fences, got %+v", blocks)
	}
	diagram, code := blocks[1], blocks[2]
	if diagram.Kind != review.BlockCode || !diagram.Diagram {
		t.Errorf("expected the mermaid fence to be code marked as a diagram, got %+v", diagram)
	}
	if code.Kind != review.BlockCode || code.Diagram {
		t.Errorf("expected the go fence to be code and no diagram, got %+v", code)
	}
}

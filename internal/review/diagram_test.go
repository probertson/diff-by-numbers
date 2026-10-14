package review_test

import (
	"fmt"
	"reflect"
	"strings"
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

// flowDiagram is a Mermaid diagram file's content: Mermaid alone, no fence.
const flowDiagram = "flowchart LR\n" +
	"  tui --> daemon\n" +
	"  daemon --> git\n"

// A file ending .mmd or .mermaid is a diagram file (#194). It is not a
// Document, so it has no blocks and stays code; its Excerpts carry the whole
// file as one diagram block, which the Browser Renderer draws.
func TestADiagramFileIsMarkedAsADiagramAndStaysCode(t *testing.T) {
	for _, file := range []string{"docs/flow.mmd", "docs/flow.mermaid"} {
		t.Run(file, func(t *testing.T) {
			files := adding(file, flowDiagram, "cmd/serve.go", "serve()\n")

			step := viewingStep(t, files, showingWhole("The flow", files, review.NewSide, file, "cmd/serve.go"))

			diagram, code := step.Excerpts[0], step.Excerpts[1]
			want := review.Block{Kind: review.BlockCode, Diagram: true, Side: review.NewSide,
				Lines: review.LineRange{First: 1, Last: 3}, Treatment: review.TreatmentNew}
			if diagram.DiagramFile == nil || !reflect.DeepEqual(*diagram.DiagramFile, want) {
				t.Errorf("expected the diagram file's Excerpt to carry it as one new diagram block %+v, got %+v", want, diagram.DiagramFile)
			}
			if len(diagram.Blocks) != 0 || diagram.Lines[0].Block != 0 {
				t.Errorf("expected the diagram file to stay code, with no Document blocks, got %+v", diagram.Blocks)
			}
			if code.DiagramFile != nil {
				t.Errorf("expected a Go file to be no diagram, got %+v", code.DiagramFile)
			}
		})
	}
}

// A diagram file is not a Document, so it is charged in lines against the code
// budget, however few words it has (#194).
func TestADiagramFileCountsAgainstTheLineBudgetNotTheWordBudget(t *testing.T) {
	lines := []string{"flowchart TD"}
	for i := range 30 {
		lines = append(lines, fmt.Sprintf("  n%d --> n%d", i, i+1))
	}
	files := adding("docs/flow.mmd", strings.Join(lines, "\n")+"\n")

	err := postingOver(files, showingWhole("The flow", files, review.NewSide, "docs/flow.mmd"))

	assertRejected(t, err, review.RejectedOversizedStep)
	assertDetailContains(t, err, `Step 1 ("The flow") counts 31 changed lines`)
	assertDetailOmits(t, err, "words")
}

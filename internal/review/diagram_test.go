package review_test

import (
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

// A changed diagram is drawn twice, before and after, each as authored (#193),
// so the Step view carries the Mermaid source of both.

// diagramOf is a Document whose one section holds a Mermaid diagram of these
// lines.
func diagramOf(lines ...string) string {
	return markdown("# Architecture", "```mermaid\n"+strings.Join(lines, "\n")+"\n```")
}

// diagramBlock is the one diagram block an Excerpt shows.
func diagramBlock(t *testing.T, view review.ExcerptView) review.Block {
	t.Helper()
	for _, block := range view.Blocks {
		if block.Diagram {
			return block
		}
	}
	t.Fatalf("expected a diagram block, got %+v", view.Blocks)
	return review.Block{}
}

func TestAChangedDiagramCarriesItsSourceBeforeAndAfterTheChange(t *testing.T) {
	files := modifying("docs/architecture.md",
		diagramOf("flowchart LR", "  tui --> daemon"),
		diagramOf("flowchart LR", "  tui --> daemon", "  daemon --> store"),
	)

	step := viewingStep(t, files, showingWhole("Architecture", files, review.NewSide, "docs/architecture.md"))

	diagram := diagramBlock(t, step.Excerpts[0])
	if diagram.Treatment != review.TreatmentEdited {
		t.Fatalf("expected the diagram edited, got %+v", diagram)
	}
	if want := "flowchart LR\n  tui --> daemon\n"; diagram.BeforeSource != want {
		t.Errorf("expected the diagram's source before the change %q, got %q", want, diagram.BeforeSource)
	}
	if want := "flowchart LR\n  tui --> daemon\n  daemon --> store\n"; diagram.AfterSource != want {
		t.Errorf("expected the diagram's source after the change %q, got %q", want, diagram.AfterSource)
	}
}

func TestARemovedDiagramCarriesOnlyItsSourceBefore(t *testing.T) {
	files := modifying("docs/architecture.md",
		markdown("# Architecture", "The parts.", "```mermaid\nflowchart LR\n  tui --> daemon\n```", "The end."),
		markdown("# Architecture", "The parts.", "The end."),
	)

	// The diagram ran over lines 5-8, a blank line after it.
	step := viewingStep(t, files, showingLines("No diagram", "docs/architecture.md", review.OldSide, 5, 9))

	diagram := diagramBlock(t, step.Excerpts[0])
	if diagram.Treatment != review.TreatmentRemoved {
		t.Fatalf("expected the diagram removed, got %+v", diagram)
	}
	if want := "flowchart LR\n  tui --> daemon\n"; diagram.BeforeSource != want {
		t.Errorf("expected the removed diagram's source %q, got %q", want, diagram.BeforeSource)
	}
	if diagram.AfterSource != "" {
		t.Errorf("expected a removed diagram to have no source after, got %q", diagram.AfterSource)
	}
}

func TestInARevisionRoundAChangedDiagramsBeforeIsThePreviousRounds(t *testing.T) {
	// A Document added in Round 1, its diagram changed for Round 2: against the
	// merge-base the diagram is new, but since Round 1 it changed.
	files := newDocumentRounds(adding("docs/architecture.md", diagramOf("flowchart LR", "  tui --> daemon")))
	session := review.NewSession(files, files)
	mustPost(t, session, documentRound(showingWhole("Architecture", files.workingFiles, review.NewSide, "docs/architecture.md")))
	handOffWithAComment(t, session)
	files.after["docs/architecture.md"] = diagramOf("flowchart LR", "  tui --> daemon", "  daemon --> store")
	if err := session.Revise(session.ReviewID(), revising(documentRound(
		showingWhole("Architecture, with a store", files.workingFiles, review.NewSide, "docs/architecture.md")))); err != nil {
		t.Fatal(err)
	}

	mustAdvance(t, session)
	diagram := diagramBlock(t, session.View().Step.Excerpts[0])

	if diagram.Treatment != review.TreatmentEdited {
		t.Fatalf("expected the diagram edited since Round 1, got %+v", diagram)
	}
	if want := "flowchart LR\n  tui --> daemon\n"; diagram.BeforeSource != want {
		t.Errorf("expected before to be Round 1's diagram %q, got %q", want, diagram.BeforeSource)
	}
	if want := "flowchart LR\n  tui --> daemon\n  daemon --> store\n"; diagram.AfterSource != want {
		t.Errorf("expected after to be this Round's diagram %q, got %q", want, diagram.AfterSource)
	}
}

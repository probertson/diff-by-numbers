package review_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// A Document's Mermaid diagrams are marked in the Step view, so the Browser
// Renderer can draw them (#191). dbn only marks them: it never
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
	if diagram.Kind != review.BlockCode || diagram.Diagram == nil {
		t.Errorf("expected the mermaid fence to be code marked as a diagram, got %+v", diagram)
	}
	if code.Kind != review.BlockCode || code.Diagram != nil {
		t.Errorf("expected the go fence to be code and no diagram, got %+v", code)
	}
}

// flowDiagram is a Mermaid diagram file's content: Mermaid alone, no fence.
const flowDiagram = "flowchart LR\n" +
	"  tui --> daemon\n" +
	"  daemon --> git\n"

// A file ending .mmd or .mermaid is a diagram file (#194). It is not a
// Document, so it has no blocks and stays code; its Excerpts carry the whole
// file as one diagram, which the Browser Renderer draws.
func TestADiagramFileIsMarkedAsADiagramAndStaysCode(t *testing.T) {
	for _, file := range []string{"docs/flow.mmd", "docs/flow.mermaid"} {
		t.Run(file, func(t *testing.T) {
			files := adding(file, flowDiagram, "cmd/serve.go", "serve()\n")

			step := viewingStep(t, files, showingWhole("The flow", files, review.NewSide, file, "cmd/serve.go"))

			diagram, code := step.Excerpts[0], step.Excerpts[1]
			want := review.DiagramFile{Treatment: review.TreatmentNew, Sources: review.DiagramSources{After: flowDiagram},
				Read: true, Lines: review.LineRange{First: 1, Last: 3}}
			if diagram.DiagramFile == nil || !reflect.DeepEqual(*diagram.DiagramFile, want) {
				t.Errorf("expected the diagram file's Excerpt to carry it as one new diagram %+v, got %+v", want, diagram.DiagramFile)
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
		if block.Diagram != nil {
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
	if want := "flowchart LR\n  tui --> daemon\n"; diagram.Diagram.Before != want {
		t.Errorf("expected the diagram's source before the change %q, got %q", want, diagram.Diagram.Before)
	}
	if want := "flowchart LR\n  tui --> daemon\n  daemon --> store\n"; diagram.Diagram.After != want {
		t.Errorf("expected the diagram's source after the change %q, got %q", want, diagram.Diagram.After)
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
	if want := "flowchart LR\n  tui --> daemon\n"; diagram.Diagram.Before != want {
		t.Errorf("expected the removed diagram's source %q, got %q", want, diagram.Diagram.Before)
	}
	if diagram.Diagram.After != "" {
		t.Errorf("expected a removed diagram to have no source after, got %q", diagram.Diagram.After)
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
	if want := "flowchart LR\n  tui --> daemon\n"; diagram.Diagram.Before != want {
		t.Errorf("expected before to be Round 1's diagram %q, got %q", want, diagram.Diagram.Before)
	}
	if want := "flowchart LR\n  tui --> daemon\n  daemon --> store\n"; diagram.Diagram.After != want {
		t.Errorf("expected after to be this Round's diagram %q, got %q", want, diagram.Diagram.After)
	}
}

// A changed diagram file is drawn before and after as a fenced diagram is
// (#194), so its Excerpt carries the whole file's source on both sides.
func TestAChangedDiagramFileCarriesItsSourceBeforeAndAfterTheChange(t *testing.T) {
	files := modifying("docs/flow.mmd", "flowchart LR\n  tui --> daemon\n", flowDiagram)

	step := viewingStep(t, files, showingWhole("The flow", files, review.NewSide, "docs/flow.mmd"))

	diagram := step.Excerpts[0].DiagramFile
	if diagram == nil || diagram.Treatment != review.TreatmentEdited {
		t.Fatalf("expected the diagram file edited, got %+v", diagram)
	}
	if want := "flowchart LR\n  tui --> daemon\n"; diagram.Sources.Before != want {
		t.Errorf("expected the file's source before the change %q, got %q", want, diagram.Sources.Before)
	}
	if diagram.Sources.After != flowDiagram {
		t.Errorf("expected the file's source after the change %q, got %q", flowDiagram, diagram.Sources.After)
	}
}

func TestARemovedDiagramFileCarriesOnlyItsSourceBefore(t *testing.T) {
	files := workingFiles{after: map[string]string{}, before: map[string]string{"docs/flow.mmd": flowDiagram}}

	step := viewingStep(t, files, showingWhole("No flow", files, review.OldSide, "docs/flow.mmd"))

	diagram := step.Excerpts[0].DiagramFile
	if diagram == nil || diagram.Treatment != review.TreatmentRemoved {
		t.Fatalf("expected the diagram file removed, got %+v", diagram)
	}
	if diagram.Sources.Before != flowDiagram || diagram.Sources.After != "" {
		t.Errorf("expected only the removed file's source before, %q, got before %q and after %q",
			flowDiagram, diagram.Sources.Before, diagram.Sources.After)
	}
}

func TestInARevisionRoundAChangedDiagramFilesBeforeIsThePreviousRounds(t *testing.T) {
	// A diagram file added in Round 1 and changed for Round 2: against the
	// merge-base it is new, but since Round 1 it changed.
	files := newDocumentRounds(adding("docs/flow.mmd", "flowchart LR\n  tui --> daemon\n"))
	session := review.NewSession(files, files)
	mustPost(t, session, documentRound(showingWhole("The flow", files.workingFiles, review.NewSide, "docs/flow.mmd")))
	handOffWithAComment(t, session)
	files.after["docs/flow.mmd"] = flowDiagram
	if err := session.Revise(session.ReviewID(), revising(documentRound(
		showingWhole("The flow, to git", files.workingFiles, review.NewSide, "docs/flow.mmd")))); err != nil {
		t.Fatal(err)
	}

	mustAdvance(t, session)
	diagram := session.View().Step.Excerpts[0].DiagramFile

	if diagram == nil || diagram.Treatment != review.TreatmentEdited {
		t.Fatalf("expected the diagram file edited since Round 1, got %+v", diagram)
	}
	if want := "flowchart LR\n  tui --> daemon\n"; diagram.Sources.Before != want {
		t.Errorf("expected before to be Round 1's diagram %q, got %q", want, diagram.Sources.Before)
	}
	if diagram.Sources.After != flowDiagram {
		t.Errorf("expected after to be this Round's diagram %q, got %q", flowDiagram, diagram.Sources.After)
	}
}

package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// A Mermaid diagram's picture is in the browser, a keypress away. The TUI says
// so where the Reviewer meets one: a header-like row above a diagram block's
// source, and above each Excerpt of a diagram file (#195). The row is no place
// in the code, so the cursor passes it by, and reaching a diagram opens nothing.

const theHint = "Mermaid diagram · p to open in browser"

// flowDocument is a new Document of a paragraph and a Mermaid diagram: lines 1
// and 3-6.
func flowDocument() daemon.ExcerptWire {
	excerpt := documentExcerpt("docs/flow.md",
		para("The flow:"),
		docBlock{kind: "code", text: []string{"```mermaid", "graph TD", "  A --> B", "```"}},
	)
	excerpt.Blocks[1].Diagram = true
	return excerpt
}

// rowAbove is the drawn row just above the one holding text, as the Reviewer
// reads it.
func rowAbove(t *testing.T, out, text string) string {
	t.Helper()
	rows := strings.Split(ansi.Strip(out), "\n")
	for i, row := range rows {
		if strings.Contains(row, text) {
			if i == 0 {
				t.Fatalf("expected a row above %q, it is the first:\n%s", text, ansi.Strip(out))
			}
			return strings.TrimSpace(rows[i-1])
		}
	}
	t.Fatalf("expected a row holding %q in:\n%s", text, ansi.Strip(out))
	return ""
}

func TestTheHintStandsAboveADiagramBlockInTheFormattedView(t *testing.T) {
	m := stepModel(t, documentStep(flowDocument()))

	out := drawnStep(m)

	if got := rowAbove(t, out, "```mermaid"); got != theHint {
		t.Errorf("expected %q just above the diagram's fence, got %q:\n%s", theHint, got, ansi.Strip(out))
	}
}

// flowFile is two Excerpts of a new diagram file, docs/flow.mmd, as dbn's Step
// view gives them: code, each marked as part of the diagram the whole file is.
func flowFile() *daemon.StepWire {
	file := &daemon.BlockWire{Kind: "code", Diagram: true, Side: "new", FirstLine: 1, LastLine: 6, Treatment: "new"}
	excerpt := func(first int, texts ...string) daemon.ExcerptWire {
		e := daemon.ExcerptWire{File: "docs/flow.mmd", Side: "new", FirstLine: first, LastLine: first + len(texts) - 1, DiagramFile: file}
		for n, text := range texts {
			e.Lines = append(e.Lines, daemon.LineWire{Number: first + n, Text: text, Side: "new", Changed: true})
		}
		return e
	}
	return documentStep(excerpt(1, "graph TD", "  A --> B"), excerpt(5, "  C --> D", "  D --> A"))
}

func TestTheHintStandsAboveEachExcerptOfADiagramFile(t *testing.T) {
	m := stepModel(t, flowFile())

	out := drawnStep(m)

	for _, first := range []string{"graph TD", "C --> D"} {
		if got := rowAbove(t, out, first); got != theHint {
			t.Errorf("expected %q just above %q, got %q:\n%s", theHint, first, got, ansi.Strip(out))
		}
	}
}

func TestTheHintStandsAboveADiagramBlockInTheSourceView(t *testing.T) {
	m := press(stepModel(t, documentStep(flowDocument())), "s")

	out := drawnStep(m)

	if got := rowAbove(t, out, "```mermaid"); got != theHint {
		t.Errorf("expected %q just above the diagram's fence, got %q:\n%s", theHint, got, ansi.Strip(out))
	}
}

// The hint is one row, like the file header: a pane too narrow for it cuts it
// rather than wrapping it.
func TestANarrowPaneCutsTheHint(t *testing.T) {
	m := stepModel(t, documentStep(flowDocument()))
	m.width = 25

	out := drawnStep(m)

	if got, want := rowAbove(t, out, "```mermaid"), "Mermaid diagram · p to…"; got != want {
		t.Errorf("expected the hint cut to %q, got %q:\n%s", want, got, ansi.Strip(out))
	}
}

func TestTheCursorPassesTheHintByInBothViews(t *testing.T) {
	for _, tc := range []struct {
		view  string
		keys  []string
		above string
	}{
		{"formatted", nil, "The flow:"},
		{"source", []string{"s", "down"}, "2 │"},
	} {
		t.Run(tc.view, func(t *testing.T) {
			m := stepModel(t, documentStep(flowDocument()))
			for _, key := range tc.keys {
				m = press(m, key)
			}

			down := press(m, "down")
			back := press(down, "up")

			if got := cursorText(t, down); !strings.Contains(got, "```mermaid") {
				t.Errorf("expected ↓ to go from %q straight to the diagram, got %q", tc.above, got)
			}
			if got := cursorText(t, back); !strings.Contains(got, tc.above) {
				t.Errorf("expected ↑ from the diagram to go straight back to %q, got %q", tc.above, got)
			}
			if got := rowAbove(t, drawnStep(down), "```mermaid"); got != theHint {
				t.Errorf("expected the hint, without the caret, above the diagram, got %q", got)
			}
		})
	}
}

func TestASelectionReachingADiagramTakesNoHintRow(t *testing.T) {
	m := press(press(stepModel(t, documentStep(flowDocument())), "s"), "down")

	selecting := press(press(m, "v"), "down")

	run, ok := selecting.cursor.selection()
	if !ok || run.rows != 2 {
		t.Errorf("expected the blank line and the fence selected, 2 lines, got %d (selected: %v)", run.rows, ok)
	}
}

func TestReachingADiagramOpensNothing(t *testing.T) {
	view := overviewView()
	view.Position, view.Step = 1, documentStep(flowDocument())
	m, opened := openingModel(modeReview, view)

	reached := press(m, "down")

	if got := cursorText(t, reached); !strings.Contains(got, "```mermaid") {
		t.Fatalf("expected the cursor on the diagram, got %q", got)
	}
	if len(*opened) != 0 {
		t.Errorf("expected reaching a diagram to open nothing, opened %v", *opened)
	}
}

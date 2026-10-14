package daemon

import (
	"html/template"
	"strconv"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/present"
	"github.com/probertson/diff-by-numbers/internal/review"
)

// A changed Mermaid diagram is drawn twice, "before" and "after", each as
// authored, with the edit to its source beneath as track changes; a removed
// one is drawn "before" only, dimmed (#193). dbn draws no change mark on
// either drawing, so its colours are never mistaken for the author's: what
// changed is read by comparing the two, and from the source beneath. A diagram
// file is drawn the same way (#194).

// changedDiagram is an edited or removed diagram as the page holds it: a
// fenced diagram in a Document, or a whole diagram file.
type changedDiagram struct {
	sources   DiagramSourcesWire
	treatment string
	// block is the block the diagram is, numbered from 1 among its Excerpt's;
	// 0 for a diagram file, which is in none.
	block int
	// place is where the diagram is: its block's place, or the place of every
	// row of a diagram file.
	place             string
	commented, cursor bool
	// edit is the edit to the diagram's source, its rows as track changes;
	// none is drawn for a removed diagram.
	edit []documentRow
}

// renderDiagramChange draws an edited or removed diagram: a container holding
// its data attributes and classes, as renderTracked's does, with a diagram
// container for each side it has, and, for an edited one, its edit beneath as
// track changes in code style. The page's script draws each diagram container
// (page.js), and lays a pair side by side when both fit.
func renderDiagramChange(c changedDiagram) template.HTML {
	removed := c.treatment == string(review.TreatmentRemoved)
	var out strings.Builder
	out.WriteString(`<div class="diagram-change`)
	if removed {
		out.WriteString(" removed")
	}
	if c.commented {
		out.WriteString(" commented")
	}
	if c.cursor {
		out.WriteString(" cursor")
	}
	out.WriteString(`"`)
	if c.block > 0 {
		out.WriteString(` data-block="` + strconv.Itoa(c.block) + `"`)
	}
	out.WriteString(` data-place="` + template.HTMLEscapeString(c.place) + `" data-treatment="` +
		template.HTMLEscapeString(c.treatment) + `">`)
	out.WriteString(diagramPair(c.sources, removed))
	if !removed && len(c.edit) > 0 {
		out.WriteString(`<div class="tracked code diagram-edit">`)
		out.WriteString(string(trackedLines(c.edit, false)))
		out.WriteString("</div>")
	}
	out.WriteString("</div>\n")
	return template.HTML(out.String())
}

// diagramPair is a changed diagram's "before" and "after" diagram containers,
// which the page's script lays side by side when both fit: "before" alone,
// dimmed, for a removed diagram. An edited diagram with no "before" (made from
// something that was no diagram, or whose "before" dbn could not read) is
// drawn as a new one is, once and unlabelled.
func diagramPair(sources DiagramSourcesWire, removed bool) string {
	var out strings.Builder
	if !removed && sources.Before == "" {
		writeDiagram(&out, "", func() {}, sources.After)
		return out.String()
	}
	out.WriteString(`<div class="diagram-pair">`)
	class := "diagram before"
	if removed {
		class += " removed"
	}
	out.WriteString(diagramFigure(class, "before", sources.Before))
	if !removed {
		out.WriteString(diagramFigure("diagram after", "after", sources.After))
	}
	out.WriteString("</div>")
	return out.String()
}

// diagramFigure is a labelled diagram container holding a diagram's source as
// authored. The label is outside the source, which is all the page's script
// reads.
func diagramFigure(class, label, source string) string {
	return `<figure class="` + class + `"><figcaption class="diagram-label">` + label +
		`</figcaption><pre class="diagram-source"><code>` + template.HTMLEscapeString(source) + "</code></pre></figure>"
}

// diagramFileEditRow is a row of an edited diagram file as track changes in
// code style, as a fenced diagram's edit is drawn: a line the change removed
// struck whole, one it added inserted whole, and one it kept as it stands. A
// signpost, standing in for lines drawn elsewhere, is dimmed.
func diagramFileEditRow(line LineWire, excerpt ExcerptWire) documentRow {
	change := review.WordsKept
	switch {
	case line.Signpost:
	case line.Changed && present.RemovedSide(sideOf(line, excerpt)):
		change = review.WordsRemoved
	case line.Changed:
		change = review.WordsAdded
	}
	return documentRow{
		text:    line.Text,
		widened: line.Signpost,
		tracked: &trackedLine{kind: string(review.BlockCode), runs: []RunWire{{Text: line.Text, Change: string(change)}}},
	}
}

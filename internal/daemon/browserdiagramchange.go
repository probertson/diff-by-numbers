package daemon

import (
	"html/template"
	"strconv"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// A changed Mermaid diagram is drawn twice, "before" and "after", each as
// authored, with the edit to its source beneath as track changes; a removed
// one is drawn "before" only, dimmed (#193). dbn draws no change mark on
// either picture, so its colours are never mistaken for the author's: what
// changed is read by comparing the two, and from the source beneath.

// diagramChange is an edited or removed diagram's Mermaid source before and
// after: "" for a side it has no diagram on.
type diagramChange struct {
	before, after string
}

// renderDiagramChange draws an edited or removed diagram, its rows given in
// order: a container holding the block's data attributes and classes, as
// renderTracked's does, with a diagram container for each side it has, and,
// for an edited one, its rows beneath as track changes in code style. The
// page's script draws each diagram container (page.js), and lays a pair side
// by side when both fit.
func renderDiagramChange(rows []documentRow) template.HTML {
	first := rows[0]
	removed := first.treatment == string(review.TreatmentRemoved)
	var out strings.Builder
	out.WriteString(`<div class="diagram-change`)
	if removed {
		out.WriteString(" removed")
	}
	if first.commented {
		out.WriteString(" commented")
	}
	if first.cursor {
		out.WriteString(" cursor")
	}
	out.WriteString(`" data-block="` + strconv.Itoa(first.block) + `" data-place="` +
		template.HTMLEscapeString(first.place) + `" data-treatment="` + template.HTMLEscapeString(first.treatment) + `">`)
	out.WriteString(diagramPair(*first.change, removed))
	if !removed {
		out.WriteString(`<div class="tracked code diagram-edit">`)
		out.WriteString(string(trackedLines(rows, false)))
		out.WriteString("</div>")
	}
	out.WriteString("</div>\n")
	return template.HTML(out.String())
}

// diagramPair is a changed diagram's "before" and "after" diagram containers,
// which the page's script lays side by side when both fit: "before" alone,
// dimmed, for a removed diagram, and no "before" for one that had none.
func diagramPair(change diagramChange, removed bool) string {
	var out strings.Builder
	out.WriteString(`<div class="diagram-pair">`)
	if change.before != "" {
		class := "diagram before"
		if removed {
			class += " removed"
		}
		out.WriteString(diagramFigure(class, "before", change.before))
	}
	if !removed {
		out.WriteString(diagramFigure("diagram after", "after", change.after))
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

package review

import (
	"path/filepath"
	"strings"
)

// isDiagramFile reports whether a file is a Mermaid diagram by its type: a
// file ending .mmd or .mermaid, which holds Mermaid alone, with no fence
// around it (#194). dbn never parses Mermaid.
func isDiagramFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mmd", ".mermaid":
		return true
	}
	return false
}

// withDiagramFile marks an Excerpt of a diagram file with the whole file as
// one diagram block (ExcerptView.DiagramFile). A diagram file is code: one the
// agent has made a Document is read as one and left as it is, as is an
// Excerpt dbn could not read.
//
// The block spans the file on the Excerpt's side, or no lines when dbn cannot
// read the whole file, so that nothing takes the Excerpt for all of it. Its
// treatment is what the Excerpt's rows show: new when every row is one the
// change added, removed when every row is one it removed, context when none
// changed, and edited otherwise.
func (s *Session) withDiagramFile(view ExcerptView) ExcerptView {
	excerpt := view.Excerpt
	if view.Problem != "" || !isDiagramFile(excerpt.File) || s.ledger.isDocumentAfterOverruling(excerpt.Repository, excerpt.File) {
		return view
	}
	block := Block{Kind: BlockCode, Diagram: true, Side: excerpt.Side, Treatment: diagramTreatment(view)}
	if reader, ok := s.resolver.(FileReader); ok {
		if source, err := reader.ReadFile(excerpt.Repository, excerpt.File, excerpt.Side, s.latest.RoundSource); err == nil && len(source) > 0 {
			block.Lines = LineRange{First: 1, Last: len(source)}
		}
	}
	view.DiagramFile = &block
	return view
}

// diagramTreatment is how the change treated what a diagram file's Excerpt
// shows, read from its rows.
func diagramTreatment(view ExcerptView) Treatment {
	changed, unchanged := 0, 0
	for _, row := range view.Lines {
		switch {
		case !row.Content() || row.Side != view.Excerpt.Side:
		case row.Changed:
			changed++
			continue
		default:
			unchanged++
			continue
		}
		// A row of the other side, or one standing in for it, is an edit's.
		return TreatmentEdited
	}
	switch {
	case changed == 0:
		return TreatmentContext
	case unchanged > 0:
		return TreatmentEdited
	case view.Excerpt.Side == OldSide:
		return TreatmentRemoved
	}
	return TreatmentNew
}

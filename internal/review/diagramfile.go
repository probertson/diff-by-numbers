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
// changed, and edited otherwise. A changed diagram file carries its source as
// a changed fenced diagram does (#193): an edited one, the whole file before
// and after the change, a removed one only before. Before is what the changes
// are read against: the merge-base, or the previous Round's Snapshot in a
// Revision Round.
func (s *Session) withDiagramFile(view ExcerptView) ExcerptView {
	excerpt := view.Excerpt
	if view.Problem != "" || !isDiagramFile(excerpt.File) || s.ledger.isDocumentAfterOverruling(excerpt.Repository, excerpt.File) {
		return view
	}
	block := Block{Kind: BlockCode, Diagram: true, Side: excerpt.Side, Treatment: diagramTreatment(view)}
	if source, ok := s.readDiagram(excerpt.Repository, excerpt.File, excerpt.Side, s.latest.RoundSource); ok {
		block.Lines = LineRange{First: 1, Last: strings.Count(source, "\n")}
		switch block.Treatment {
		case TreatmentRemoved:
			block.BeforeSource = source
		case TreatmentEdited:
			block.BeforeSource, block.AfterSource = s.diagramSourceBefore(excerpt.Repository, excerpt.File), source
		}
	}
	view.DiagramFile = &block
	return view
}

// diagramSourceBefore is a diagram file as it was before the changes the view
// shades: at the merge-base, or in the previous Round's Snapshot, where it may
// have had another path. A repository whose Rounds dbn could not map is
// shaded against the merge-base (sinceRows), so it is read there. It is ""
// when dbn cannot read it.
func (s *Session) diagramSourceBefore(repository, file string) string {
	if s.sincePrevious() {
		if mapping, ok := s.previous.mappings[repository]; ok {
			source, _ := s.readDiagram(repository, mapping.PathIn(file), NewSide, s.previous.round)
			return source
		}
	}
	source, _ := s.readDiagram(repository, file, OldSide, s.latest.RoundSource)
	return source
}

// readDiagram is one side of a diagram file as a round holds it, each line
// ending in a newline, and whether dbn could read any of it.
func (s *Session) readDiagram(repository, file string, side Side, round RoundSource) (string, bool) {
	reader, ok := s.resolver.(FileReader)
	if !ok {
		return "", false
	}
	lines, err := reader.ReadFile(repository, file, side, round)
	if err != nil || len(lines) == 0 {
		return "", false
	}
	return strings.Join(lines, "\n") + "\n", true
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

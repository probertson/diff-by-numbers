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

// DiagramSources is a Mermaid diagram's source before and after the change,
// each line ending in a newline: for a fenced diagram what lies between its
// fences, for a diagram file the whole file. "" stands for a side it is no
// diagram on: a new diagram has only After, a removed one only Before, and an
// unchanged one both, the same. An edited one has both, or only After when it
// was made from something that was no diagram, or dbn could not read what it
// was. Before is what the changes are read against: the merge-base, or the
// previous Round's Snapshot in a Revision Round.
type DiagramSources struct {
	Before, After string
}

// diagramSourcesOf is the source of a diagram treated so, as one side holds
// it: an edited one's Before is read elsewhere.
func diagramSourcesOf(treatment Treatment, source string) DiagramSources {
	switch treatment {
	case TreatmentRemoved:
		return DiagramSources{Before: source}
	case TreatmentContext:
		return DiagramSources{Before: source, After: source}
	}
	return DiagramSources{After: source}
}

// DiagramFile is an Excerpt of a Mermaid diagram file seen as the one diagram
// the whole file is: how the change treated what the Excerpt shows, and the
// file's source, so the Browser Renderer draws it as a fenced diagram is drawn.
type DiagramFile struct {
	Treatment Treatment
	Sources   DiagramSources
	// Read reports whether dbn could read the file on the Excerpt's side.
	// Lines then spans the whole file there; otherwise Lines and Sources are
	// empty, and nothing takes the Excerpt for all of the file.
	Read  bool
	Lines LineRange
}

// withDiagramFile marks an Excerpt of a diagram file as one
// (ExcerptView.DiagramFile). A diagram file is code: one the agent has made a
// Document is read as one and left as it is, as is an Excerpt dbn could not
// read.
//
// Its treatment is what the Excerpt's rows show: new when every row is one the
// change added, removed when every row is one it removed, context when none
// changed, and edited otherwise.
func (s *Session) withDiagramFile(view ExcerptView) ExcerptView {
	excerpt := view.Excerpt
	if view.Problem != "" || !isDiagramFile(excerpt.File) || s.ledger.isDocumentAfterOverruling(excerpt.Repository, excerpt.File) {
		return view
	}
	file := DiagramFile{Treatment: diagramTreatment(view)}
	if source, ok := s.readDiagram(excerpt.Repository, excerpt.File, excerpt.Side, s.latest.RoundSource); ok {
		file.Read, file.Lines = true, LineRange{First: 1, Last: strings.Count(source, "\n")}
		file.Sources = diagramSourcesOf(file.Treatment, source)
		if file.Treatment == TreatmentEdited {
			file.Sources.Before = s.diagramSourceBefore(excerpt.Repository, excerpt.File)
		}
	}
	view.DiagramFile = &file
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

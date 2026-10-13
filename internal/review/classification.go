package review

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Classification is the Authoring Agent overruling dbn's classification of one
// file. dbn judges a Document by file type alone, but only the agent knows what
// a file is for: a prose spec in a .txt file is a Document, and a generated
// Markdown changelog arguably is not. Either way can be overruled.
type Classification struct {
	// Repository is the root of the repository the file is in, optional when the
	// Change Set has only one.
	Repository string
	// File is the path, relative to the repository root, the file is described
	// under.
	File string
	// Document says whether the file is a Document.
	Document bool
}

// overrulings are the classifications in force, keyed by file: whether each
// file the agent has overruled is a Document. A file not in it keeps dbn's
// classification by type.
type overrulings map[fileRef]bool

// with lays classifications over these, a later one for a file replacing an
// earlier, and leaves the receiver as it was. A classification naming no
// repository it can be placed in is left out; validateClassifications refuses
// it.
func (o overrulings) with(set ChangeSet, given []Classification) overrulings {
	out := overrulings{}
	for ref, document := range o {
		out[ref] = document
	}
	for _, c := range given {
		repository := set.repositoryOf(c.Repository)
		if repository == "" || !set.contains(repository) {
			continue
		}
		out[fileRef{repository, filepath.Clean(c.File)}] = c.Document
	}
	return out
}

// withOverrulings returns the ledger classifying files as the agent has
// overruled them.
func (l ledger) withOverrulings(o overrulings) ledger {
	l.overruled = o
	return l
}

// isDocumentFile is how the round classifies a file: as the agent overruled it,
// or else by dbn's judgement of its type. A binary file is never a Document: it
// has no text to read.
func (l ledger) isDocumentFile(repository, file string) bool {
	if opaque, ok := l.opaqueFor(repository, file); ok && opaque.Kind == OpaqueBinary {
		return false
	}
	if document, ok := l.overruled[fileRef{repository, filepath.Clean(file)}]; ok {
		return document
	}
	return isDocument(file)
}

// inChangeSet reports whether a file is part of the Change Set: it has an atom,
// or git reported it new or gone, as an empty file can be with no atoms at all.
func (l ledger) inChangeSet(repository, file string) bool {
	if l.fileHasChange(repository, file) {
		return true
	}
	_, ok := l.fileStatus[fileRef{repository, filepath.Clean(file)}]
	return ok
}

// validateClassifications refuses a classification that would silently do
// nothing or make no sense: one naming no repository it can be placed in, no
// file, a file outside the Change Set (a typo, most often) or a file classified
// twice, and one making a binary file a Document. Every fault is listed at once.
func (l ledger) validateClassifications(given []Classification, set ChangeSet) *Rejection {
	var faults []string
	seen := map[fileRef]bool{}
	for i, c := range given {
		where := fmt.Sprintf("Classification %d", i+1)
		repository := set.repositoryOf(c.Repository)
		switch {
		case c.Repository == "" && repository == "":
			faults = append(faults, where+" names no repository: repository is required when the Change Set has more than one repository")
			continue
		case !set.contains(repository):
			faults = append(faults, fmt.Sprintf("%s names repository %q, which the Change Set does not include", where, repository))
			continue
		case c.File == "":
			faults = append(faults, where+" names no file")
			continue
		case !l.inChangeSet(repository, c.File):
			faults = append(faults, fmt.Sprintf("%s names %q, which is not in the Change Set; name a file by the path describe_changes lists it under", where, c.File))
			continue
		}
		ref := fileRef{repository, filepath.Clean(c.File)}
		if seen[ref] {
			faults = append(faults, fmt.Sprintf("%s classifies %q, which an earlier one already classifies; a file is classified once", where, c.File))
		}
		seen[ref] = true
		if opaque, ok := l.opaqueFor(repository, c.File); ok && opaque.Kind == OpaqueBinary && c.Document {
			faults = append(faults, fmt.Sprintf("%s makes %q a Document, but it is a binary file, with no text to read", where, c.File))
		}
	}
	if len(faults) == 0 {
		return nil
	}
	return reject(RejectedMalformedClassification, "%s", strings.Join(faults, "\n"))
}

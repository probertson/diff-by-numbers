package review

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Overruling is the Authoring Agent overruling dbn's classification of one
// file. dbn judges a Document by file type alone, but only the agent knows what
// a file is for: a prose spec in a .txt file is a Document, and a generated
// Markdown changelog arguably is not. Either way can be overruled. On the wire
// an overruling is a classification, since that is what the agent gives.
type Overruling struct {
	// Repository is the root of the repository the file is in, optional when the
	// Change Set has only one.
	Repository string
	// File is the path, relative to the repository root, the file is described
	// under.
	File string
	// Document says whether the file is a Document.
	Document bool
}

// overrulings are the overrulings in force, keyed by file: whether each file
// the agent has overruled is a Document. A file not in it keeps dbn's
// classification by type.
type overrulings map[fileRef]bool

// with lays the given overrulings over these, a later one for a file replacing
// an earlier, and leaves the receiver as it was. An overruling naming no
// repository it can be placed in is left out; validateOverrulings refuses it.
func (o overrulings) with(set ChangeSet, given []Overruling) overrulings {
	out := overrulings{}
	for ref, document := range o {
		out[ref] = document
	}
	for _, g := range given {
		repository := set.repositoryOf(g.Repository)
		if repository == "" || !set.contains(repository) {
			continue
		}
		out[fileRef{repository, filepath.Clean(g.File)}] = g.Document
	}
	return out
}

// withOverrulings returns the ledger classifying files as the agent has
// overruled them.
func (l ledger) withOverrulings(o overrulings) ledger {
	l.overruled = o
	return l
}

// isDocumentAfterOverruling is how the round classifies a file: as the agent
// overruled it, or else by dbn's judgement of its type (isMarkdown). A binary
// file is never a Document: it has no text to read.
func (l ledger) isDocumentAfterOverruling(repository, file string) bool {
	if opaque, ok := l.opaqueFor(repository, file); ok && opaque.Kind == OpaqueBinary {
		return false
	}
	if document, ok := l.overruled[fileRef{repository, filepath.Clean(file)}]; ok {
		return document
	}
	return isMarkdown(file)
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

// validateOverrulings refuses an overruling that would silently do nothing or
// make no sense: one naming no repository it can be placed in, no file, a file
// outside the Change Set (a typo, most often) or a file overruled twice, and one
// making a binary file a Document. Every fault is listed at once. The faults
// call each one a Classification, as the agent sent it.
func (l ledger) validateOverrulings(given []Overruling, set ChangeSet) *Rejection {
	var faults []string
	seen := map[fileRef]bool{}
	for i, g := range given {
		where := fmt.Sprintf("Classification %d", i+1)
		repository := set.repositoryOf(g.Repository)
		switch {
		case g.Repository == "" && repository == "":
			faults = append(faults, where+" names no repository: repository is required when the Change Set has more than one repository")
			continue
		case !set.contains(repository):
			faults = append(faults, fmt.Sprintf("%s names repository %q, which the Change Set does not include", where, repository))
			continue
		case g.File == "":
			faults = append(faults, where+" names no file")
			continue
		case !l.inChangeSet(repository, g.File):
			faults = append(faults, fmt.Sprintf("%s names %q, which is not in the Change Set; name a file by the path describe_changes lists it under", where, g.File))
			continue
		}
		ref := fileRef{repository, filepath.Clean(g.File)}
		if seen[ref] {
			faults = append(faults, fmt.Sprintf("%s classifies %q, which an earlier one already classifies; a file is classified once", where, g.File))
		}
		seen[ref] = true
		if opaque, ok := l.opaqueFor(repository, g.File); ok && opaque.Kind == OpaqueBinary && g.Document {
			faults = append(faults, fmt.Sprintf("%s makes %q a Document, but it is a binary file, with no text to read", where, g.File))
		}
	}
	if len(faults) == 0 {
		return nil
	}
	return reject(RejectedMalformedClassification, "%s", strings.Join(faults, "\n"))
}

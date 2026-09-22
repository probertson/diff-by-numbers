package review

import (
	"path/filepath"
	"sort"
)

// FileStatus is what happened to a file in a Change Set, as git reports it.
type FileStatus string

const (
	FileAdded    FileStatus = "added"
	FileModified FileStatus = "modified"
	FileDeleted  FileStatus = "deleted"
	FileRenamed  FileStatus = "renamed"
	FileBinary   FileStatus = "binary"
	FileMode     FileStatus = "mode"
)

// LineRange is an inclusive run of line numbers on one side of a file.
type LineRange struct{ First, Last int }

// Modification is one edit that replaced lines: the before-side it removed and
// the after-side that took its place. It is a Correspondence with both sides,
// which is what decides where a before-side rides along with its after-side.
type Modification struct{ Old, New LineRange }

// FileDescription is one file's part of a Change Set.
type FileDescription struct {
	Path   string
	Status FileStatus
	// From is where a renamed file came from.
	From          string
	NewRanges     []LineRange
	OldRanges     []LineRange
	Modifications []Modification
	// PreMarkedNew and PreMarkedOld are the ranges a Revision Round has already
	// shown, which the next post need not cover again.
	PreMarkedNew []LineRange
	PreMarkedOld []LineRange
	// PreMarked is set on an Opaque Change a Revision Round has already shown,
	// which the next post need not acknowledge again.
	PreMarked bool
}

// RepositoryDescription is one repository's part of a Change Set.
type RepositoryDescription struct {
	Root string
	// MergeBase is the commit the Change Set is derived from.
	MergeBase string
	Files     []FileDescription
}

// ChangeDescription is dbn's account of a Change Set, for the Authoring Agent to
// plan a Round from.
type ChangeDescription struct {
	Repositories []RepositoryDescription
	// RevisionRound reports that the next accepted post would be scoped against
	// an earlier round, so pre-marked ranges and StillToCover are filled in.
	RevisionRound bool
	// StillToCover counts the Changed Lines the next post must still account
	// for, once what is pre-marked is set aside.
	StillToCover int
}

// DescribeChanges derives a Change Set the way a post over it would be checked,
// and says what it found, without posting anything or changing the review.
//
// Agents used to run git diff themselves to find the ranges to excerpt, which
// duplicated dbn's derivation and got it subtly wrong — renames, untracked files,
// pre-marking. This answers from the same ledger a post is validated against,
// scoped the same way the next post would be, so the two cannot disagree.
// It names no review, so it plans a first round: there is nothing to pre-mark
// against. DescribeRevision is how a Revision Round is planned.
func (s *Session) DescribeChanges(set ChangeSet) (ChangeDescription, error) {
	return s.describe(set, nil)
}

// DescribeRevision describes the changes as the next Revision Round of the
// review named by id would be checked, pre-marking what that review's latest
// round already showed. What it pre-marks depends on which review it describes,
// so the id is required rather than inferred (ADR-0015).
// The round it pre-marks against is the one Revise would answer: the round just
// handed off, or — while a round is still under review, where the next post is a
// Replacement — whatever that round answers.
func (s *Session) DescribeRevision(id string, set ChangeSet) (ChangeDescription, error) {
	if rejection := s.named(id, "describe against", "describe without a review_id to plan a new review"); rejection != nil {
		return ChangeDescription{}, rejection
	}
	return s.describe(set, s.nextAnswering())
}

func (s *Session) describe(set ChangeSet, earlier *earlierRound) (ChangeDescription, error) {
	if rejection := validateChangeSet(set); rejection != nil {
		return ChangeDescription{}, rejection
	}
	// A snapshot is only for pre-marking here, so a first round writes none.
	l, _, rejection := s.scopedLedger(set, earlier, earlier != nil)
	if rejection != nil {
		return ChangeDescription{}, rejection
	}
	return l.describe(set, earlier != nil), nil
}

// fileAtoms gathers one file's part of the ledger while a description is built.
type fileAtoms struct {
	newLines, oldLines []int
	preNew, preOld     []int
	opaque             *OpaqueChange
	modifications      []Modification
}

// describe lays the ledger out file by file, repositories in Change Set order
// and files by path.
func (l ledger) describe(set ChangeSet, revision bool) ChangeDescription {
	files := l.atomsByFile()
	out := ChangeDescription{RevisionRound: revision}
	if revision {
		for _, line := range l.lines {
			if !l.preShown[line] {
				out.StillToCover++
			}
		}
	}
	for _, repository := range set.Repositories {
		described := RepositoryDescription{Root: repository.Root, MergeBase: l.bases[repository.Root]}
		var paths []string
		for ref := range files {
			if ref.repository == repository.Root {
				paths = append(paths, ref.file)
			}
		}
		sort.Strings(paths)
		for _, path := range paths {
			described.Files = append(described.Files, l.describeFile(repository.Root, path, files[fileRef{repository.Root, path}]))
		}
		out.Repositories = append(out.Repositories, described)
	}
	return out
}

// atomsByFile gathers every atom of the ledger under the file it belongs to. A
// file git reports as new or gone is listed even with no atoms at all — an empty
// file has no Changed Lines to name it, but it did still come or go.
func (l ledger) atomsByFile() map[fileRef]*fileAtoms {
	files := map[fileRef]*fileAtoms{}
	atomsOf := func(repository, file string) *fileAtoms {
		ref := fileRef{repository, file}
		if files[ref] == nil {
			files[ref] = &fileAtoms{}
		}
		return files[ref]
	}
	for ref := range l.fileStatus {
		atomsOf(ref.repository, ref.file)
	}
	for _, line := range l.lines {
		atoms := atomsOf(line.Repository, line.File)
		if line.Side == NewSide {
			atoms.newLines = append(atoms.newLines, line.Line)
			if l.preShown[line] {
				atoms.preNew = append(atoms.preNew, line.Line)
			}
			continue
		}
		atoms.oldLines = append(atoms.oldLines, line.Line)
		if l.preShown[line] {
			atoms.preOld = append(atoms.preOld, line.Line)
		}
	}
	for i := range l.opaque {
		atomsOf(l.opaque[i].Repository, l.opaque[i].File).opaque = &l.opaque[i]
	}
	for _, c := range l.correspondences {
		if c.isModification() {
			atoms := atomsOf(c.Repository, c.File)
			atoms.modifications = append(atoms.modifications, Modification{
				Old: LineRange{c.OldFirst, c.OldLast},
				New: LineRange{c.NewFirst, c.NewLast},
			})
		}
	}
	return files
}

// describeFile is one file's description.
func (l ledger) describeFile(repository, path string, atoms *fileAtoms) FileDescription {
	file := FileDescription{
		Path:          path,
		NewRanges:     rangesOf(atoms.newLines),
		OldRanges:     rangesOf(atoms.oldLines),
		Modifications: atoms.modifications,
		PreMarkedNew:  rangesOf(atoms.preNew),
		PreMarkedOld:  rangesOf(atoms.preOld),
	}
	sort.Slice(file.Modifications, func(i, j int) bool {
		return file.Modifications[i].New.First < file.Modifications[j].New.First
	})
	file.From, _ = l.renamedFrom(repository, path)
	file.Status = l.statusOf(repository, path, atoms.opaque, file.From != "")
	if atoms.opaque != nil {
		file.PreMarked = l.preShownOpaque[fileRef{repository, path}]
	}
	return file
}

// opaqueStatus is the status each kind of Opaque Change reads as.
var opaqueStatus = map[OpaqueKind]FileStatus{
	OpaqueBinary: FileBinary,
	OpaqueMode:   FileMode,
	OpaqueRename: FileRenamed,
}

// statusOf classifies what happened to a file: an Opaque Change by its kind, a
// renamed file as renamed, and otherwise by whether git reported the file new,
// gone, or neither.
func (l ledger) statusOf(repository, file string, opaque *OpaqueChange, renamed bool) FileStatus {
	if opaque != nil {
		return opaqueStatus[opaque.Kind]
	}
	if renamed {
		return FileRenamed
	}
	if status, ok := l.fileStatus[fileRef{repository, filepath.Clean(file)}]; ok {
		return status
	}
	return FileModified
}

// renamedFrom is the path a file was renamed from, if git detected a rename.
func (l ledger) renamedFrom(repository, file string) (string, bool) {
	for source, destination := range l.renames {
		if source.repository == repository && destination == filepath.Clean(file) {
			return source.file, true
		}
	}
	return "", false
}

// rangesOf collapses line numbers into the runs they form, in order.
func rangesOf(numbers []int) []LineRange {
	sorted := append([]int(nil), numbers...)
	sort.Ints(sorted)
	var out []LineRange
	for _, run := range runsOf(sorted) {
		out = append(out, LineRange{run.first, run.last})
	}
	return out
}

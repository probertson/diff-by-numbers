package review

import (
	"fmt"
	"path/filepath"
	"strings"
)

// validate checks that a Walkthrough is structurally well-formed. It deliberately
// says nothing about whether Excerpt ranges resolve against real files or whether
// coverage is complete: those need the Change Set derived from git, which arrives
// with plan validation.
func validate(w Walkthrough) *Rejection {
	if rejection := validateChangeSet(w.ChangeSet); rejection != nil {
		return rejection
	}
	if rejection := validateBrief(w.Brief); rejection != nil {
		return rejection
	}
	return validateSteps(w.Steps, w.ChangeSet)
}

func validateSteps(steps []Step, changeSet ChangeSet) *Rejection {
	if len(steps) == 0 {
		return reject(RejectedMalformedStep, "the Walkthrough contains no Steps")
	}
	for i, step := range steps {
		position := i + 1
		if step.Name == "" {
			return reject(RejectedMalformedStep,
				"Step %d has no name; a Step names the single idea it contains", position)
		}
		if step.Explanation == "" {
			return reject(RejectedMalformedStep,
				"Step %d has no explanation, which is the whole reason the Reviewer is not reading a bare diff", position)
		}
		if len(step.Excerpts) == 0 {
			return reject(RejectedMalformedStep, "Step %d shows no Excerpts", position)
		}
		for j, excerpt := range step.Excerpts {
			if rejection := validateExcerpt(excerpt, position, j+1, changeSet); rejection != nil {
				return rejection
			}
		}
	}
	return nil
}

func validateExcerpt(e Excerpt, step, index int, changeSet ChangeSet) *Rejection {
	where := fmt.Sprintf("Excerpt %d of Step %d", index, step)
	if e.File == "" {
		return reject(RejectedMalformedStep, "%s names no file", where)
	}
	if rejection := validateFilePath(e.File, where); rejection != nil {
		return rejection
	}
	if e.Side != OldSide && e.Side != NewSide {
		return reject(RejectedMalformedStep,
			"%s must qualify its side as %q or %q, since deleted lines exist only on the old side and added lines only on the new",
			where, OldSide, NewSide)
	}
	if e.FirstLine < 1 {
		return reject(RejectedMalformedStep, "%s starts at line %d; line numbers begin at 1", where, e.FirstLine)
	}
	if e.LastLine < e.FirstLine {
		return reject(RejectedMalformedStep, "%s ends at line %d, before it starts at line %d", where, e.LastLine, e.FirstLine)
	}
	if !changeSet.contains(e.Repository) {
		return reject(RejectedMalformedStep,
			"%s names repository %q, which the Change Set does not include", where, e.Repository)
	}
	return nil
}

func validateChangeSet(c ChangeSet) *Rejection {
	if len(c.Repositories) == 0 {
		return reject(RejectedEmptyChangeSet,
			"the Change Set names no repositories, so there is nothing to review")
	}
	for i, repository := range c.Repositories {
		if repository.Root == "" {
			return reject(RejectedEmptyChangeSet, "repository %d names no root", i+1)
		}
		// An absolute root is required because dbn never assumes the session's
		// working directory is a repository — a relative root would silently
		// resolve against the daemon's own directory instead.
		if !filepath.IsAbs(repository.Root) {
			return reject(RejectedEmptyChangeSet,
				"repository root %q must be absolute; dbn does not resolve paths relative to its own working directory",
				repository.Root)
		}
		if repository.Range == "" {
			return reject(RejectedEmptyChangeSet,
				"repository %q names no range, so the changes under review are undefined", repository.Root)
		}
	}
	return nil
}

func validateBrief(b Brief) *Rejection {
	if b.Ask == "" {
		return reject(RejectedMalformedBrief,
			"the Brief does not say what was asked for, which is the context the Reviewer cannot reconstruct from the diff")
	}
	if b.Approach == "" {
		return reject(RejectedMalformedBrief,
			"the Brief does not state the approach taken, so the Reviewer cannot judge the approach separately from the code")
	}
	switch b.Provenance.Kind {
	case ProvenanceInferred:
		return nil
	case ProvenanceStated:
		if b.Provenance.Citation == "" {
			return reject(RejectedMalformedBrief,
				"stated Provenance must cite its source, otherwise the claim to know the intent is only an assertion")
		}
		return nil
	default:
		return reject(RejectedMalformedBrief,
			"the Brief must declare Provenance as %q or %q", ProvenanceStated, ProvenanceInferred)
	}
}

// validateFilePath keeps an Excerpt inside the repository that contains it. dbn
// reads these paths off disk to render them, so an unchecked path is an
// arbitrary file read driven by whatever posted the Walkthrough.
func validateFilePath(file, where string) *Rejection {
	if filepath.IsAbs(file) {
		return reject(RejectedMalformedStep,
			"%s names the absolute path %q; Excerpt files are relative to their repository root", where, file)
	}
	cleaned := filepath.Clean(file)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return reject(RejectedMalformedStep,
			"%s names %q, which escapes its repository", where, file)
	}
	return nil
}

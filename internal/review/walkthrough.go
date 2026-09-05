// Package review is the review core: it owns a Walkthrough's whole life and
// knows nothing about MCP, terminals or git.
package review

// ProvenanceKind records where a Brief's account of intent came from. A guessed
// intent reads exactly as confidently as a known one, so the distinction is
// carried explicitly rather than left to the Reviewer to infer.
type ProvenanceKind string

const (
	// ProvenanceStated means the Authoring Agent was there, or read the session
	// transcript, and can cite it.
	ProvenanceStated ProvenanceKind = "stated"
	// ProvenanceInferred means the account was reverse-engineered from the
	// changes themselves.
	ProvenanceInferred ProvenanceKind = "inferred"
)

// Provenance is a Brief's declaration of where its account of intent came from.
type Provenance struct {
	Kind     ProvenanceKind
	Citation string
}

// Brief is the opening screen of a Walkthrough, shown before any code.
type Brief struct {
	Ask        string
	Approach   string
	Provenance Provenance
}

// Side qualifies a line range. Deleted lines exist only on the old side, added
// lines only on the new.
type Side string

const (
	OldSide Side = "old"
	NewSide Side = "new"
)

// Excerpt is a contiguous range of lines in one file of one repository that the
// Authoring Agent chose to show, sized for comprehension rather than by any
// tool's output format. It may include unchanged lines for reference.
type Excerpt struct {
	Repository string
	File       string
	Side       Side
	FirstLine  int
	LastLine   int
}

// Acknowledgement is the Coverage Ledger's escape valve (ADR-0010): a claim that
// a set of files changed mechanically and need not be read line by line. It
// satisfies coverage in place of an Excerpt, and is the only way to account for
// an Opaque Change, which has no lines to excerpt. It is a claim, not a
// dismissal: it renders as a manifest and the Reviewer may expand it on demand.
type Acknowledgement struct {
	Repository string
	// Files are the paths, relative to the repository root, whose entire change
	// this Acknowledgement claims. Every change in one of these files — Changed
	// Lines and Opaque Changes alike — is thereby accounted for.
	Files []string
	// Reason is the one-line account of why these changes are mechanical.
	Reason string
}

// Step is one numbered stop in a Walkthrough: a single self-contained idea. It
// carries Excerpts, Acknowledgements, or both; a Step with neither shows nothing.
type Step struct {
	Name        string
	Explanation string
	Excerpts    []Excerpt
	// Acknowledgements cover mechanical changes the Step does not excerpt.
	Acknowledgements []Acknowledgement
	// OversizeJustification is required only when a Step exceeds the size
	// budget. dbn never forbids a large Step, it only demands a reason.
	OversizeJustification string
}

// Repository is one repository's contribution to a Change Set, carrying its own
// range because default branches and conventions differ between repositories.
type Repository struct {
	Root  string
	Range string
}

// ChangeSet is the complete set of changes under review. It is a list because a
// single Walkthrough may span several repositories, and because a session root
// is frequently not a repository at all.
type ChangeSet struct {
	Repositories []Repository
}

// Walkthrough is one complete review over one Change Set: a Brief followed by an
// ordered sequence of Steps.
type Walkthrough struct {
	Brief     Brief
	ChangeSet ChangeSet
	Steps     []Step
}

// contains reports whether the Change Set includes the given repository root.
func (c ChangeSet) contains(root string) bool {
	for _, repository := range c.Repositories {
		if repository.Root == root {
			return true
		}
	}
	return false
}

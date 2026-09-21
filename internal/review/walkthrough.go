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
	// PreviousSide qualifies a row read from the previous round's snapshot,
	// drawn when a Revision Round is shaded by what moved since then (#44): a
	// line that round had and this one replaced or withdrew. dbn draws these;
	// an Excerpt never names one, and validation refuses it.
	PreviousSide Side = "previous"
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
// base ref because default branches and conventions differ between repositories.
type Repository struct {
	Root string
	Base string
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
	// Dispositions accounts for the previous round's Comments when this
	// Walkthrough is a Revision Round. It is empty for a first Walkthrough.
	Dispositions []Disposition
	// Label is an optional human-readable name the Authoring Agent may attach so a
	// Reviewer juggling several reviews can tell them apart. It is not the review's
	// identity — that is minted by dbn — only a display aid. It survives a Revision
	// Round that omits it.
	Label string
}

// sole returns the one repository under review, when there is only one. It is
// what makes leaving `repository` off an Excerpt or an Acknowledgement
// unambiguous, and the single place that rule is stated: validation, filling in
// and the uniqueness check must all agree on it, or one accepts what another
// cannot make sense of.
func (c ChangeSet) sole() (string, bool) {
	if len(c.Repositories) != 1 {
		return "", false
	}
	return c.Repositories[0].Root, true
}

// repositoryOf resolves the repository an Excerpt or Acknowledgement sits in,
// standing in the sole repository where the field was left out. It answers ""
// when the field is empty and there is no sole repository, which validation
// refuses separately.
func (c ChangeSet) repositoryOf(named string) string {
	if named != "" {
		return named
	}
	only, _ := c.sole()
	return only
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

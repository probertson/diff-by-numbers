// Package review is the review core: it owns a Review's whole life and
// knows nothing about MCP, terminals or git.
package review

import "time"

// Brief is the opening screen of a Round, shown before any code.
type Brief struct {
	Goal     string
	Approach string
}

// OpenReview is a review the daemon is still holding: enough for the Authoring
// Agent to recognise its own among several, which is what it is listed for when
// a call names no review (ADR-0015).
type OpenReview struct {
	ID    string
	Label string
	// HandedOff is the Reviewer having handed the round back, so the review is
	// waiting on the Authoring Agent rather than on them.
	HandedOff bool
	// Opened is the Reviewer having looked at the review at all, which is what
	// tells one waiting to be picked up from one left part-way through.
	Opened bool
	// Repositories are the repositories under review and the branch each is on,
	// which is what says whose work a row is.
	Repositories []OpenRepository
	// Round counts the rounds: 1 for the first, one more for each Revision Round.
	Round int
	// Position is where the Reviewer is: 0 for the Overview, 1..StepCount for a
	// Step.
	Position int
	// StepCount is how many Steps this round has.
	StepCount int
	// CommentCount is how many the Reviewer has raised and not withdrawn.
	CommentCount int
	// Posted is when the round on screen was accepted, which is what a row's age
	// is measured from.
	Posted time.Time
}

// OpenRepository is one repository under review, as a row names it: the
// repository's own name and the branch its work is on.
type OpenRepository struct {
	// Name is the repository directory's basename — what the Reviewer calls it,
	// rather than the absolute path they already know.
	Name string
	// Branch is what the work is on, empty where git could not say.
	Branch string
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

// Step is one numbered stop in a Round: a single self-contained idea. It
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
	// Questions are the Agent Questions the agent asks about this Step's code,
	// shown at the top of the Step apart from its Explanation (ADR-0017).
	Questions []Question
}

// Repository is one repository's contribution to a Change Set, carrying its own
// base ref because default branches and conventions differ between repositories.
type Repository struct {
	Root string
	Base string
}

// ChangeSet is the complete set of changes under review. It is a list because a
// single Round may span several repositories, and because a session root
// is frequently not a repository at all.
type ChangeSet struct {
	Repositories []Repository
}

// Round is one pass of a Review over one Change Set: a Brief followed by an
// ordered sequence of Steps.
type Round struct {
	Brief     Brief
	ChangeSet ChangeSet
	Steps     []Step
	// Dispositions accounts for the previous round's Comments when this
	// Round is a Revision Round. It is empty for a first Round.
	Dispositions []Disposition
	// Questions are the Agent Questions about the approach rather than any
	// Step's code, shown with the Brief (ADR-0017).
	Questions []Question
	// QuestionStatuses accounts for the previous round's Agent Questions when
	// this Round is a Revision Round, as Dispositions does for its Comments.
	QuestionStatuses []QuestionAccount
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

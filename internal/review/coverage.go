package review

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// StepBudget is the soft ceiling on the Changed Lines a Step may show before it
// must carry a justification. Reference lines do not count. It is a fixed number
// rather than terminal-relative, so a plan validated on one terminal cannot fail
// on another.
const StepBudget = 30

// ChangedLine identifies one line that differs between the two sides of the
// changes under review — the atom of the Coverage Ledger. Derived from git, never
// from the Authoring Agent, so the completeness guarantee does not rest on the
// account of the code being reviewed.
type ChangedLine struct {
	Repository string
	File       string
	Side       Side
	Line       int
}

// OpaqueKind names a change with no line-level representation. Each is a whole
// change that no Excerpt could ever show.
type OpaqueKind string

const (
	// OpaqueBinary is a modified, added or deleted binary file.
	OpaqueBinary OpaqueKind = "binary"
	// OpaqueMode is a file whose mode changed with no content change.
	OpaqueMode OpaqueKind = "mode"
	// OpaqueRename is a pure rename with no content delta.
	OpaqueRename OpaqueKind = "rename"
)

// OpaqueChange is a Change Set atom that has no lines — a binary file, a mode
// change, a pure rename. It can be accounted for only by an Acknowledgement, so
// the Coverage Ledger's guarantee holds for changes an Excerpt cannot reach
// (ADR-0010).
type OpaqueChange struct {
	Repository string
	File       string
	Kind       OpaqueKind
	// Detail is a short human hint for the manifest, e.g. "100644 -> 100755" or
	// "renamed from old/path".
	Detail string
}

func (o OpaqueChange) String() string {
	return fmt.Sprintf("%s (%s)", o.File, o.Kind)
}

// Derivation is everything git says changed in a repository's Change Set: the Changed
// Lines, the Opaque Changes that have no lines, and the cross-side Correspondences
// that pair each edit's removed lines with the lines that replaced them.
type Derivation struct {
	Lines           []ChangedLine
	Opaque          []OpaqueChange
	Correspondences []Correspondence
	// Base is the resolved merge-base the Change Set was derived from. A
	// Revision Round compares it with the previous round's: when it has moved,
	// the branch was rebased under the review and old-side lines no longer sit
	// where they did.
	Base string
}

// Deriver produces the Changed Lines and Opaque Changes of a repository's Change Set.
// It is the half of the git adapter that answers "what actually changed", kept
// behind an interface so the core performs no git of its own.
type Deriver interface {
	Derive(Repository) (Derivation, error)
}

func (c ChangedLine) covered(e Excerpt) bool {
	return c.Repository == e.Repository &&
		c.File == e.File &&
		c.Side == e.Side &&
		c.Line >= e.FirstLine &&
		c.Line <= e.LastLine
}

func (c ChangedLine) String() string {
	return fmt.Sprintf("%s:%d (%s)", c.File, c.Line, c.Side)
}

// ledger is the derived set of Change Set atoms for a posted Walkthrough: its
// Changed Lines, its Opaque Changes, and the Correspondences that pair each
// edit's before-side with the after-side that replaced it.
type ledger struct {
	lines           []ChangedLine
	opaque          []OpaqueChange
	correspondences []Correspondence
	// bases is each repository's resolved merge-base, keyed by root. A Revision
	// Round compares it with the previous round's to tell whether the branch was
	// rebased underneath the review, which moves every old-side line.
	bases map[string]string
}

func buildLedger(changeSet ChangeSet, deriver Deriver) (ledger, error) {
	l := ledger{bases: map[string]string{}}
	for _, repository := range changeSet.Repositories {
		derivation, err := deriver.Derive(repository)
		if err != nil {
			return ledger{}, err
		}
		l.lines = append(l.lines, derivation.Lines...)
		l.opaque = append(l.opaque, derivation.Opaque...)
		l.correspondences = append(l.correspondences, derivation.Correspondences...)
		if derivation.Base != "" {
			l.bases[repository.Root] = derivation.Base
		}
	}
	return l, nil
}

// covers reports whether an Acknowledgement claims a given file. Paths are
// cleaned on both sides so "./x" and "x" match.
func (a Acknowledgement) covers(repository, file string) bool {
	if a.Repository != repository {
		return false
	}
	target := filepath.Clean(file)
	for _, f := range a.Files {
		if filepath.Clean(f) == target {
			return true
		}
	}
	return false
}

// stepCoversLine reports whether a Step accounts for a Changed Line, whether by
// showing it in an Excerpt or by acknowledging its file.
func stepCoversLine(step Step, line ChangedLine) bool {
	for _, excerpt := range step.Excerpts {
		if line.covered(excerpt) {
			return true
		}
	}
	for _, ack := range step.Acknowledgements {
		if ack.covers(line.Repository, line.File) {
			return true
		}
	}
	return false
}

// stepCoversOpaque reports whether a Step accounts for an Opaque Change. Only an
// Acknowledgement can: an Opaque Change has no lines to excerpt.
func stepCoversOpaque(step Step, opaque OpaqueChange) bool {
	for _, ack := range step.Acknowledgements {
		if ack.covers(opaque.Repository, opaque.File) {
			return true
		}
	}
	return false
}

// validateCoverage refuses a plan that leaves any Changed Line or Opaque Change
// unaccounted for. The guarantee the agent cannot opt out of. Lines pre-marked as
// shown by a Revision Round are already accounted for and are not re-demanded.
func (l ledger) validateCoverage(steps []Step, preShown map[ChangedLine]bool, preShownOpaque map[fileRef]bool) *Rejection {
	var uncovered []string
	for _, line := range l.lines {
		if preShown[line] {
			continue
		}
		// A before-side line rides along when a Step shows the after-side of the
		// edit that removed it: pointing once at a change accounts for the lines it
		// replaced, without the agent naming the old side.
		if l.riddenAlong(line, steps) {
			continue
		}
		if !anyStep(steps, func(s Step) bool { return stepCoversLine(s, line) }) {
			uncovered = append(uncovered, line.String())
		}
	}
	for _, opaque := range l.opaque {
		if preShownOpaque[fileRef{opaque.Repository, opaque.File}] {
			continue
		}
		if !anyStep(steps, func(s Step) bool { return stepCoversOpaque(s, opaque) }) {
			uncovered = append(uncovered, opaque.String())
		}
	}
	if len(uncovered) == 0 {
		return nil
	}
	return reject(RejectedUncoveredChanges,
		"no Excerpt or Acknowledgement accounts for %s: %s", pluralize(len(uncovered), "change"), summarize(uncovered))
}

// validateAcknowledgements refuses an Acknowledgement that claims a file with no
// changes: a claim that accounts for nothing is a confusion, and left unchecked
// would let one gesture at coverage it does not provide.
func (l ledger) validateAcknowledgements(steps []Step) *Rejection {
	for i, step := range steps {
		for j, ack := range step.Acknowledgements {
			for _, file := range ack.Files {
				if !l.fileHasChange(ack.Repository, file) {
					return reject(RejectedEmptyAcknowledgement,
						"Acknowledgement %d of Step %d claims %q in %q, which has no changes to account for",
						j+1, i+1, file, ack.Repository)
				}
			}
		}
	}
	return nil
}

// fileHasChange reports whether a file has any atom in the ledger — a Changed
// Line or an Opaque Change.
func (l ledger) fileHasChange(repository, file string) bool {
	target := filepath.Clean(file)
	for _, line := range l.lines {
		if line.Repository == repository && filepath.Clean(line.File) == target {
			return true
		}
	}
	for _, opaque := range l.opaque {
		if opaque.Repository == repository && filepath.Clean(opaque.File) == target {
			return true
		}
	}
	return false
}

func anyStep(steps []Step, pred func(Step) bool) bool {
	for _, step := range steps {
		if pred(step) {
			return true
		}
	}
	return false
}

// validateBudget refuses an oversized Step that carries no justification. It
// never refuses a justified one: no mechanism in dbn forces an arbitrary cut.
func (l ledger) validateBudget(steps []Step) *Rejection {
	// Every offender, not the first. An agent that learns about them one
	// rejected post at a time re-sends the whole Walkthrough — Brief, every
	// Step, every Excerpt — to find out about the next, and they were all
	// present on the first attempt. The coverage check has always aggregated;
	// the budget was the outlier.
	var over []oversizedStep
	for i, step := range steps {
		count := l.changedLinesIn(step)
		if count > StepBudget && step.OversizeJustification == "" {
			over = append(over, oversizedStep{position: i + 1, name: step.Name, count: count})
		}
	}

	switch len(over) {
	case 0:
		return nil
	case 1:
		return reject(RejectedOversizedStep, "%s", over[0].sentence())
	default:
		// Uncapped: the point is to learn about all of them in one go, and a
		// Walkthrough has few enough Steps that the list stays readable.
		var listed strings.Builder
		fmt.Fprintf(&listed, "%d Steps are over the budget of %d changed lines and give no justification:",
			len(over), StepBudget)
		for _, step := range over {
			fmt.Fprintf(&listed, "\n  %s", step.entry())
		}
		return reject(RejectedOversizedStep, "%s", listed.String())
	}
}

// oversizedStep is one Step over the budget, named as well as numbered: with
// twenty Steps, "Step 6" alone means counting positions in a JSON array to find
// the one to fix.
type oversizedStep struct {
	position int
	name     string
	count    int
}

// String identifies the Step. validateSteps has already refused a Step with no
// name by the time the budget is checked, so the name is always there to quote.
func (o oversizedStep) String() string {
	return fmt.Sprintf("Step %d (%q)", o.position, o.name)
}

// sentence is how one offender reads on its own.
func (o oversizedStep) sentence() string {
	return fmt.Sprintf("%s shows %d changed lines, over the budget of %d, and gives no justification",
		o, o.count, StepBudget)
}

// entry is how one offender reads in a list, where the header has already said
// what the budget is and what is wrong with them.
func (o oversizedStep) entry() string {
	return fmt.Sprintf("%s shows %d", o, o.count)
}

// changedLinesIn counts the distinct Changed Lines a Step shows. A line shown by
// two of the Step's Excerpts counts once.
func (l ledger) changedLinesIn(step Step) int {
	seen := map[ChangedLine]bool{}
	for _, line := range l.lines {
		for _, excerpt := range step.Excerpts {
			if line.covered(excerpt) {
				seen[line] = true
				break
			}
		}
	}
	// Before-side lines this Step rides along render as `-` rows beside their
	// replacements, so they cost against the budget too: a rewrite is not cheaper
	// to read than an addition of the same size (status quo — both sides count).
	for _, line := range l.lines {
		if line.Side == OldSide && !seen[line] && l.riddenAlong(line, []Step{step}) {
			seen[line] = true
		}
	}
	return len(seen)
}

// seenBy counts the distinct atoms accounted for by Steps 1..position — Changed
// Lines shown or acknowledged, and Opaque Changes acknowledged — for the live
// coverage the Reviewer sees as they move. Pre-marked lines count as seen from
// the start, since a Revision Round has already reviewed them.
func (l ledger) seenBy(steps []Step, position int, preShown map[ChangedLine]bool) int {
	seenLines := map[ChangedLine]bool{}
	for line := range preShown {
		seenLines[line] = true
	}
	seenOpaque := map[OpaqueChange]bool{}
	bound := position
	if bound > len(steps) {
		bound = len(steps)
	}
	visible := steps[:bound]
	for _, step := range visible {
		for _, line := range l.lines {
			if stepCoversLine(step, line) {
				seenLines[line] = true
			}
		}
		for _, opaque := range l.opaque {
			if stepCoversOpaque(step, opaque) {
				seenOpaque[opaque] = true
			}
		}
	}
	// A before-side line the Reviewer has passed rides along with the after-side
	// that replaced it, so it counts as seen once its Step is behind them.
	for _, line := range l.lines {
		if line.Side == OldSide && !seenLines[line] && l.riddenAlong(line, visible) {
			seenLines[line] = true
		}
	}
	return len(seenLines) + len(seenOpaque)
}

func (l ledger) total() int { return len(l.lines) + len(l.opaque) }

// changedLinesFor returns the Changed Lines of a single file, so an
// Acknowledgement can report how many it claims and expand into them on demand.
func (l ledger) changedLinesFor(repository, file string) []ChangedLine {
	target := filepath.Clean(file)
	var out []ChangedLine
	for _, line := range l.lines {
		if line.Repository == repository && filepath.Clean(line.File) == target {
			out = append(out, line)
		}
	}
	return out
}

// opaqueFor returns the Opaque Change of a file, if it has one.
func (l ledger) opaqueFor(repository, file string) (OpaqueChange, bool) {
	target := filepath.Clean(file)
	for _, opaque := range l.opaque {
		if opaque.Repository == repository && filepath.Clean(opaque.File) == target {
			return opaque, true
		}
	}
	return OpaqueChange{}, false
}

// expansionExcerpts is what an acknowledged file expands into: one Excerpt per
// edit git found, in file order, shaped so each renders as a unified diff. An
// edit with an after-side becomes a new-side Excerpt, whose rendering injects the
// before-side it replaced; a pure removal has no after-side to sit beside and
// becomes a before-only Excerpt. Any Changed Line no edit accounts for (a ledger
// derived without Correspondences) falls back to plain side-by-side runs, so
// nothing the Acknowledgement claims is left out.
func (l ledger) expansionExcerpts(repository, file string) []Excerpt {
	type row struct {
		side Side
		line int
	}
	target := filepath.Clean(file)
	shown := map[row]bool{}
	var out []Excerpt
	for _, c := range l.correspondences {
		if c.Repository != repository || filepath.Clean(c.File) != target {
			continue
		}
		switch {
		case c.hasNew():
			out = append(out, Excerpt{Repository: repository, File: c.File, Side: NewSide, FirstLine: c.NewFirst, LastLine: c.NewLast})
			for n := c.NewFirst; n <= c.NewLast; n++ {
				shown[row{NewSide, n}] = true
			}
			// Only a modification's before-side is injected beside its replacement.
			if c.isModification() {
				for n := c.OldFirst; n <= c.OldLast; n++ {
					shown[row{OldSide, n}] = true
				}
			}
		case c.hasOld():
			out = append(out, Excerpt{Repository: repository, File: c.File, Side: OldSide, FirstLine: c.OldFirst, LastLine: c.OldLast})
			for n := c.OldFirst; n <= c.OldLast; n++ {
				shown[row{OldSide, n}] = true
			}
		}
	}
	var rest []ChangedLine
	for _, line := range l.changedLinesFor(repository, file) {
		if !shown[row{line.Side, line.Line}] {
			rest = append(rest, line)
		}
	}
	return append(out, excerptsForChangedLines(repository, file, rest)...)
}

// excerptsForChangedLines groups a file's Changed Lines into the smallest set of
// contiguous Excerpts that show them all, one side at a time.
func excerptsForChangedLines(repository, file string, lines []ChangedLine) []Excerpt {
	bySide := map[Side][]int{}
	for _, line := range lines {
		bySide[line.Side] = append(bySide[line.Side], line.Line)
	}
	var excerpts []Excerpt
	for _, side := range []Side{OldSide, NewSide} {
		nums := bySide[side]
		if len(nums) == 0 {
			continue
		}
		sort.Ints(nums)
		start, prev := nums[0], nums[0]
		for _, n := range nums[1:] {
			if n == prev+1 {
				prev = n
				continue
			}
			excerpts = append(excerpts, Excerpt{Repository: repository, File: file, Side: side, FirstLine: start, LastLine: prev})
			start, prev = n, n
		}
		excerpts = append(excerpts, Excerpt{Repository: repository, File: file, Side: side, FirstLine: start, LastLine: prev})
	}
	return excerpts
}

// isChanged reports whether a specific line is a Changed Line, so a resolved
// Excerpt can distinguish real changes from the reference context around them.
func (l ledger) isChanged(repo, file string, side Side, line int) bool {
	for _, c := range l.lines {
		if c.Repository == repo && c.File == file && c.Side == side && c.Line == line {
			return true
		}
	}
	return false
}

// summarize names a handful of atoms for a rejection, so the message is
// actionable without dumping thousands of them.
func summarize(labels []string) string {
	const limit = 5
	sort.Strings(labels)
	if len(labels) > limit {
		return strings.Join(labels[:limit], ", ") + fmt.Sprintf(", and %d more", len(labels)-limit)
	}
	return strings.Join(labels, ", ")
}

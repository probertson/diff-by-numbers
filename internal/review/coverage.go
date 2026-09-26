package review

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// fileRef identifies a file within a repository — the unit a rename, an Opaque
// Change and a pre-marked Opaque Change are all recorded against.
type fileRef struct {
	repository string
	file       string
}

// StepBudget is the soft ceiling on the new reading a Step may ask for before it
// must carry a justification. What counts toward it is not everything the Step
// shows: reference lines never did, and neither do whitespace-only lines or ones
// a Revision Round has already shown. It is a fixed number rather than
// terminal-relative, so a plan validated on one terminal cannot fail on another.
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
	// Branch is the branch the repository is on, for a surface naming the work
	// rather than the comparison — the Inbox row, which says what is under
	// review, not what it is measured against. Empty where git cannot say, such
	// as a detached HEAD.
	Branch          string
	Lines           []ChangedLine
	Opaque          []OpaqueChange
	Correspondences []Correspondence
	// Whitespace names the Changed Lines whose content is whitespace only. It is
	// a subset of Lines, not a separate kind of atom: these still have to be
	// accounted for, but an Excerpt beside one absorbs it rather than demanding
	// the agent name a line nobody needs to be told to read, and they cost
	// nothing against a Step's budget.
	Whitespace []ChangedLine
	// Renames maps each rename git found, source path to destination, including
	// renames that also carry edits — which are not Opaque Changes, so nothing
	// else in the derivation records where they came from. Every atom of a
	// renamed file is attributed to the destination, so this is the only thing
	// that can make the source path mean anything.
	Renames map[string]string
	// Added and Deleted name the files git reports as new or as gone. Which
	// sides of a file carry Changed Lines cannot say this — a file that only
	// gained lines is still one that was modified — so it is recorded where git
	// states it.
	Added   []string
	Deleted []string
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

// ledger is the derived set of Change Set atoms for a posted Round: its
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
	// branches is the branch each repository was on when the round was derived,
	// keyed by root: what the Inbox names a review's work by.
	branches map[string]string
	// whitespace is the subset of lines holding nothing but whitespace, as a set
	// because both the absorption pass and the budget ask about one line at a time.
	whitespace map[ChangedLine]bool
	// renames maps each rename's source path to its destination, keyed by
	// repository, so normalisation can read the source as an alias for the
	// destination every atom of the file was derived under.
	renames map[fileRef]string
	// fileStatus records the files git reported as new or as gone. Every other
	// file with atoms was modified, renamed, or is an Opaque Change.
	fileStatus map[fileRef]FileStatus
	// preShown and preShownOpaque are the atoms a Revision Round has already
	// shown. They live on the ledger because that is what ADR-0007 pre-marks:
	// every question the ledger answers — is this accounted for, how much new
	// reading does this Step ask for, how far has the Reviewer got — is asked of
	// the round's own scope, not of the raw Change Set.
	preShown       map[ChangedLine]bool
	preShownOpaque map[fileRef]bool
}

// newSinceEarlier reports whether a Changed Line is new to this round: not one
// the earlier round already had, which pre-marking records. For an old-side line
// that means a deletion the earlier round did not have yet.
func (l ledger) newSinceEarlier(line ChangedLine) bool {
	return l.isChanged(line.Repository, line.File, line.Side, line.Line) && !l.preShown[line]
}

// deletedSinceEarlier reports whether an old-side Excerpt shows any deletion new
// to this round.
func (l ledger) deletedSinceEarlier(excerpt Excerpt) bool {
	for n := excerpt.FirstLine; n <= excerpt.LastLine; n++ {
		if l.newSinceEarlier(ChangedLine{Repository: excerpt.Repository, File: excerpt.File, Side: OldSide, Line: n}) {
			return true
		}
	}
	return false
}

// withPreMarking returns the ledger scoped to a Revision Round. The atoms are
// unchanged; what the round has already shown is marked, so coverage does not
// re-demand it and the budget does not count it.
func (l ledger) withPreMarking(lines map[ChangedLine]bool, opaque map[fileRef]bool) ledger {
	l.preShown = lines
	l.preShownOpaque = opaque
	return l
}

func buildLedger(changeSet ChangeSet, deriver Deriver) (ledger, error) {
	l := ledger{
		bases:      map[string]string{},
		branches:   map[string]string{},
		whitespace: map[ChangedLine]bool{},
		renames:    map[fileRef]string{},
		fileStatus: map[fileRef]FileStatus{},
	}
	for _, repository := range changeSet.Repositories {
		derivation, err := deriver.Derive(repository)
		if err != nil {
			return ledger{}, err
		}
		l.branches[repository.Root] = derivation.Branch
		l.lines = append(l.lines, derivation.Lines...)
		l.opaque = append(l.opaque, derivation.Opaque...)
		l.correspondences = append(l.correspondences, derivation.Correspondences...)
		for _, line := range derivation.Whitespace {
			l.whitespace[line] = true
		}
		for source, destination := range derivation.Renames {
			l.renames[fileRef{repository.Root, filepath.Clean(source)}] = filepath.Clean(destination)
		}
		for _, file := range derivation.Added {
			l.fileStatus[fileRef{repository.Root, filepath.Clean(file)}] = FileAdded
		}
		for _, file := range derivation.Deleted {
			l.fileStatus[fileRef{repository.Root, filepath.Clean(file)}] = FileDeleted
		}
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
	for _, f := range a.Files {
		if samePath(f, file) {
			return true
		}
	}
	return false
}

// samePath reports whether two paths name the same file. Paths arrive from git
// and from the Authoring Agent, which writes them as it would in a shell, so
// "./x" and "x" have to read as one file.
func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
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
func (l ledger) validateCoverage(steps []Step) *Rejection {
	var uncoveredLines []ChangedLine
	var uncoveredOpaque []OpaqueChange
	for _, line := range l.lines {
		if !l.accountedFor(line, steps) {
			uncoveredLines = append(uncoveredLines, line)
		}
	}
	for _, opaque := range l.opaque {
		if l.preShownOpaque[fileRef{opaque.Repository, opaque.File}] {
			continue
		}
		if !anyStep(steps, func(s Step) bool { return stepCoversOpaque(s, opaque) }) {
			uncoveredOpaque = append(uncoveredOpaque, opaque)
		}
	}
	if len(uncoveredLines) == 0 && len(uncoveredOpaque) == 0 {
		return nil
	}
	return reject(RejectedUncoveredChanges, "%s", summarizeUncovered(uncoveredLines, uncoveredOpaque))
}

// accountedFor reports whether a Changed Line is already covered, by any of the
// ways a line can be: shown by an Excerpt, claimed by an Acknowledgement, ridden
// along by the after-side that replaced it, or pre-marked as already read by a
// Revision Round. It is what the coverage guarantee is made of, and also what
// absorption asks before widening a range — dbn widens only over lines that
// would otherwise be left unaccounted for.
func (l ledger) accountedFor(line ChangedLine, steps []Step) bool {
	if l.preShown[line] {
		return true
	}
	// A before-side line rides along when a Step shows the after-side of the edit
	// that removed it: pointing once at a change accounts for the lines it
	// replaced, without the agent naming the old side.
	if l.riddenAlong(line, steps, steps) {
		return true
	}
	return anyStep(steps, func(s Step) bool { return stepCoversLine(s, line) })
}

// renamedTo gives the destination path of a rename, when the name given is its
// source and means nothing else. A freed-up path can be reused by a new file the
// same branch created; when it is, it carries atoms of its own and the name means
// that file, so no alias applies and both have to be accounted for separately.
func (l ledger) renamedTo(repository, file string) (string, bool) {
	destination, ok := l.renames[fileRef{repository, filepath.Clean(file)}]
	if !ok || l.fileHasChange(repository, file) {
		return "", false
	}
	return destination, true
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
	for _, line := range l.lines {
		if line.Repository == repository && samePath(line.File, file) {
			return true
		}
	}
	for _, opaque := range l.opaque {
		if opaque.Repository == repository && samePath(opaque.File, file) {
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
	// rejected post at a time re-sends the whole Round — Brief, every
	// Step, every Excerpt — to find out about the next, and they were all
	// present on the first attempt. The coverage check has always aggregated;
	// the budget was the outlier.
	var over []oversizedStep
	for i, step := range steps {
		count := l.changedLinesIn(step, steps)
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
		// Round has few enough Steps that the list stays readable.
		var listed strings.Builder
		fmt.Fprintf(&listed, "%d Steps are over the budget of %d changed lines and give no justification:",
			len(over), StepBudget)
		for _, step := range over {
			fmt.Fprintf(&listed, "\n  %s", step.entry())
		}
		fmt.Fprintf(&listed, "\n%s", splitFirst)
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

// splitFirst closes every oversized refusal. The refusal is where the agent
// chooses between splitting and justifying, so it says which comes first; and it
// says why the count can be lower than the lines a Step shows, so an agent
// tallying its own Excerpts is not left with a number it cannot reproduce.
const splitFirst = "Split by idea into smaller Steps, and justify a Step only if it cannot be split; " +
	"whitespace-only lines and lines already shown last round are not counted"

// sentence is how one offender reads on its own. It says "counts", not "shows":
// the number is what the Step costs against the budget (see budgetedLines).
func (o oversizedStep) sentence() string {
	return fmt.Sprintf("%s counts %d changed lines toward the budget of %d, and gives no justification. %s",
		o, o.count, StepBudget, splitFirst)
}

// entry is how one offender reads in a list, where the header has already said
// what the budget is and what is wrong with them.
func (o oversizedStep) entry() string {
	return fmt.Sprintf("%s counts %d", o, o.count)
}

// changedLinesIn counts the distinct Changed Lines a Step asks the Reviewer to
// read. A line shown by two of the Step's Excerpts counts once.
func (l ledger) changedLinesIn(step Step, all []Step) int {
	seen := map[ChangedLine]bool{}
	code := l.budgetedLines()
	for _, line := range code {
		for _, excerpt := range step.Excerpts {
			if line.covered(excerpt) {
				seen[line] = true
				break
			}
		}
	}
	// Before-side lines this Step draws render as `-` rows beside their
	// replacements, so they cost against the budget too: a rewrite is not cheaper
	// to read than an addition of the same size (status quo — both sides count).
	// A Step draws only the before-side assigned to it, so a split rewrite is not
	// billed to one Step and drawn in another.
	for _, line := range code {
		if line.Side == OldSide && !seen[line] && l.riddenAlong(line, []Step{step}, all) {
			seen[line] = true
		}
	}
	return len(seen)
}

// budgetedLines are the Changed Lines the budget counts: the ones that cost the
// Reviewer something to read.
//
// A whitespace-only line costs nothing, so it never pushes a Step over the
// budget — least of all a blank separator dbn itself absorbed into the Step's
// ranges. Neither does a line a Revision Round pre-marked as shown: it was read
// last round, and the budget is about how much new reading a Step asks for. A
// Step re-showing a stretch of already-reviewed context around a fix counts
// zero, rather than being refused as oversized with no justification to give.
func (l ledger) budgetedLines() []ChangedLine {
	out := make([]ChangedLine, 0, len(l.lines))
	for _, line := range l.lines {
		if l.whitespace[line] || l.preShown[line] {
			continue
		}
		out = append(out, line)
	}
	return out
}

// seenBy counts the distinct atoms accounted for by Steps 1..position — Changed
// Lines shown or acknowledged, and Opaque Changes acknowledged — for the live
// coverage the Reviewer sees as they move. Pre-marked lines count as seen from
// the start, since a Revision Round has already reviewed them.
func (l ledger) seenBy(steps []Step, position int) int {
	seenLines := map[ChangedLine]bool{}
	for line := range l.preShown {
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
		if line.Side == OldSide && !seenLines[line] && l.riddenAlong(line, visible, steps) {
			seenLines[line] = true
		}
	}
	return len(seenLines) + len(seenOpaque)
}

func (l ledger) total() int { return len(l.lines) + len(l.opaque) }

// changedLinesFor returns the Changed Lines of a single file, so an
// Acknowledgement can report how many it claims and expand into them on demand.
func (l ledger) changedLinesFor(repository, file string) []ChangedLine {
	var out []ChangedLine
	for _, line := range l.lines {
		if line.Repository == repository && samePath(line.File, file) {
			out = append(out, line)
		}
	}
	return out
}

// opaqueFor returns the Opaque Change of a file, if it has one.
func (l ledger) opaqueFor(repository, file string) (OpaqueChange, bool) {
	for _, opaque := range l.opaque {
		if opaque.Repository == repository && samePath(opaque.File, file) {
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

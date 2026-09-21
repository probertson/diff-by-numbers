package review

import (
	"fmt"
	"strings"
)

// Line is one row of resolved file content, tagged with the Side it belongs to
// and whether git considers it a Changed Line (versus reference context the
// Excerpt included for readability). A new-side Excerpt's rendered lines are a
// unified diff: mostly new-side rows, with the before-side rows of each edit it
// shows injected as `-` lines, so Side is per line, not per Excerpt.
type Line struct {
	Number  int
	Text    string
	Side    Side
	Changed bool
	// Unreadable marks a row that stands in for code dbn could not read, rather
	// than code itself. It is drawn — a line that rode along must not vanish
	// silently — but it is not content, so nothing may quote it as if it were.
	Unreadable bool
	// Signpost marks a row standing where a before-side would go, when other
	// Steps draw it instead. Like Unreadable it is drawn but is not content: it
	// cannot be selected or anchored, and it counts toward nothing.
	Signpost bool
}

// Content reports whether a row is code, rather than a stand-in drawn in its
// place — an unreadable before-side, or a signpost to one drawn elsewhere. Only
// content may be selected, quoted or anchored.
func (l Line) Content() bool { return !l.Unreadable && !l.Signpost }

// Round is where one accepted Walkthrough's code is read from, per repository
// root: the Round Snapshot taken when it was posted, which holds the after-side,
// and the merge-base its Change Set was derived from, which holds the before-side.
// Both are git objects and immutable, so anything read from a Round can be cached
// for good.
type Round struct {
	// Snapshots has no entry for a repository git could not snapshot, whose code
	// is then read from the working tree instead — the same conservative fallback
	// pre-marking makes.
	Snapshots map[string]string
	// Bases is the merge-base the ledger was derived from, so the before-side the
	// Reviewer reads is the one coverage was counted against.
	Bases map[string]string
}

// Resolver turns an Excerpt into the lines it names, as they stand in a Round:
// the after-side from the round's snapshot, the before-side from its merge-base.
// The core performs no I/O — whoever constructs the Session supplies the thing
// that reads bytes — and it names the Round on every call, so a post being
// checked and the round on screen can never be confused for one another.
type Resolver interface {
	Resolve(Excerpt, Round) ([]Line, error)
}

// ExcerptView is an Excerpt with its content resolved, or the reason it could
// not be. The two are mutually exclusive: dbn never renders code it could not
// actually read.
type ExcerptView struct {
	Excerpt Excerpt
	Lines   []Line
	Problem string
	// ChangedOnDisk is set when the file has been edited since the round was
	// posted. The lines are still the posted ones; this only says the Reviewer
	// is no longer looking at what is on disk.
	ChangedOnDisk bool
}

// AcknowledgedFile is one line of an Acknowledgement's manifest: a file the
// Acknowledgement claims, and the size of what it stands in for. A file is
// either line-represented (ChangedLines > 0) or an Opaque Change (Opaque set),
// never both.
type AcknowledgedFile struct {
	Repository   string
	File         string
	ChangedLines int
	Opaque       OpaqueKind
	OpaqueDetail string
	// Change classifies what happened to the file, so a manifest can group by it:
	// "added", "removed", "modified", or an OpaqueKind ("binary", "mode", "rename").
	Change string
}

// Change classifications for a line-represented acknowledged file.
const (
	ChangeAdded    = "added"
	ChangeRemoved  = "removed"
	ChangeModified = "modified"
)

// AcknowledgementView is an Acknowledgement ready to draw: its reason and the
// manifest of what it covers. It renders as a claim the Reviewer can weigh and
// expand, never as code hidden from them.
type AcknowledgementView struct {
	Reason  string
	Entries []AcknowledgedFile
}

// StepView is one Step ready to draw.
type StepView struct {
	Number                int
	Name                  string
	Explanation           string
	OversizeJustification string
	Excerpts              []ExcerptView
	Acknowledgements      []AcknowledgementView
}

// Coverage is the live progress the Reviewer sees: how many Changed Lines the
// Steps up to their current position have shown, out of the total git derived.
// Because a plan cannot be posted unless it covers everything, Total is always
// reachable — Seen climbs to it as the Reviewer walks.
type Coverage struct {
	Seen  int
	Total int
}

// ViewModel is everything needed to draw the current screen. Position 0 is the
// Brief; positions 1..StepCount are Steps.
type ViewModel struct {
	Posted bool
	// Posting identifies the Walkthrough on screen among those this Session has
	// accepted: it changes exactly when a new one, or a Revision Round, replaces it.
	Posting int
	// ReviewID is the review on screen, which a restarted daemon mints afresh —
	// so together with Posting it tells one Walkthrough from any other.
	ReviewID     string
	Brief        Brief
	StepNames    []string
	StepCount    int
	Position     int
	Step         *StepView
	Coverage     Coverage
	Repositories []Repository
	// Seen[i] reports whether Step i+1 has been visited.
	Seen []bool
	// StepStatuses[i] is the derived disposition of Step i+1.
	StepStatuses []StepStatus
	Comments     []Comment
	Finished     bool
	// Concluded reports whether the review loop is over — finished having raised
	// nothing, or ended explicitly. It lets the finished screen tell "a Revision
	// Round is coming" from "the review is complete".
	Concluded bool
	// Dispositions accounts for the previous round's Comments in a Revision
	// Round — shown before any code, so a decline is seen before the fix.
	Dispositions []ResolvedDisposition
	// Replaced reports that the Walkthrough on screen replaced another in place,
	// so a surface can say why the review changed under the Reviewer.
	Replaced bool
}

// View reports what should be on screen right now.
func (s *Session) View() ViewModel {
	if s.walkthrough == nil {
		return ViewModel{Posted: false}
	}

	w := s.walkthrough
	names := make([]string, 0, len(w.Steps))
	for _, step := range w.Steps {
		names = append(names, step.Name)
	}

	view := ViewModel{
		Posted:       true,
		Posting:      s.postings,
		ReviewID:     s.id,
		Brief:        w.Brief,
		StepNames:    names,
		StepCount:    len(w.Steps),
		Position:     s.position,
		Repositories: w.ChangeSet.Repositories,
		Coverage: Coverage{
			Seen:  s.ledger.seenBy(w.Steps, s.position),
			Total: s.ledger.total(),
		},
		Seen:         s.seenFlags(),
		StepStatuses: s.stepStatuses(),
		Comments:     s.Comments(),
		Finished:     s.finished,
		Concluded:    s.isConcluded(),
		Dispositions: s.Dispositions(),
		Replaced:     s.replaced,
	}
	if s.position > 0 {
		view.Step = s.stepView(s.position)
	}
	return view
}

func (s *Session) stepView(position int) *StepView {
	step := s.walkthrough.Steps[position-1]
	excerpts := make([]ExcerptView, 0, len(step.Excerpts))
	for i, excerpt := range step.Excerpts {
		excerpts = append(excerpts, s.resolveExcerpt(excerpt, drawnIn{step: step, at: i, all: s.walkthrough.Steps}))
	}
	acknowledgements := make([]AcknowledgementView, 0, len(step.Acknowledgements))
	for _, ack := range step.Acknowledgements {
		acknowledgements = append(acknowledgements, s.acknowledgementView(ack))
	}
	return &StepView{
		Number:                position,
		Name:                  step.Name,
		Explanation:           step.Explanation,
		OversizeJustification: step.OversizeJustification,
		Excerpts:              excerpts,
		Acknowledgements:      acknowledgements,
	}
}

// drawnIn is where an Excerpt is being rendered: which Step it belongs to, its
// position among that Step's Excerpts, and the Steps the Walkthrough is made of.
// Together those decide which of a modification's before-side lines it draws.
type drawnIn struct {
	step Step
	at   int
	all  []Step
}

// resolveExcerpt reads an Excerpt's lines and marks the Changed ones, or records
// why it could not be read. dbn never renders code it could not actually read.
//
// A new-side Excerpt renders as a unified diff: its after-side lines, with the
// before-side of each edit it shows injected as removed lines just above their
// replacement. An old-side Excerpt is a deliberately shown deletion and renders
// before-only.
func (s *Session) resolveExcerpt(excerpt Excerpt, in drawnIn) ExcerptView {
	view := ExcerptView{Excerpt: excerpt, ChangedOnDisk: s.changedOnDisk(excerpt)}
	lines, err := s.resolver.Resolve(excerpt, s.latest.Round)
	if err != nil {
		view.Problem = err.Error()
		return view
	}
	if excerpt.Side == NewSide {
		view.Lines = s.interleaveBefore(excerpt, in, lines)
		return view
	}
	// An old-side Excerpt that assigns a rewrite's before-side has already been
	// drawn, interleaved above the after-side it replaced. Drawing it again as a
	// standalone block would show the same removal twice in one Step; what falls
	// outside any modification the Step shows — a deliberate deletion — still
	// renders here, which is what an old-side Excerpt was for.
	interleaved := s.interleavedOld(in)
	kept := make([]Line, 0, len(lines))
	for _, line := range lines {
		if interleaved[fileLine{excerpt.Repository, excerpt.File, line.Number}] {
			continue
		}
		line.Side = excerpt.Side
		line.Changed = s.ledger.isChanged(excerpt.Repository, excerpt.File, excerpt.Side, line.Number)
		kept = append(kept, line)
	}
	view.Lines = kept
	return view
}

// fileLine identifies one before-side line of one file, so a Step can be asked
// which of them it has already drawn by interleaving.
type fileLine struct {
	repository string
	file       string
	line       int
}

// interleavedOld is every before-side line this Step draws beside an after-side.
func (s *Session) interleavedOld(in drawnIn) map[fileLine]bool {
	out := map[fileLine]bool{}
	for _, excerpt := range in.step.Excerpts {
		if excerpt.Side != NewSide {
			continue
		}
		for _, c := range s.ledger.modificationsShownBy(excerpt) {
			for _, n := range assignedBefore(c, in.step, in.all) {
				out[fileLine{c.Repository, c.File, n}] = true
			}
		}
	}
	return out
}

// interleaveBefore turns a new-side Excerpt's after-side lines into a unified
// diff: for each edit whose replacement begins inside the Excerpt, the before-side
// lines it removed are read and placed immediately above their replacement, so the
// Reviewer reads "these lines became these" as one thought. Reference lines stay
// unmarked; a before-side that cannot be read is left out rather than faked.
//
// The order this produces is load-bearing beyond display: Session.Anchor resolves
// a Reviewer's selection against these rows, so it is what decides which lines sit
// between two selected endpoints (#57). Changing the interleaving changes what a
// selection spanning a removal and its replacement contains.
func (s *Session) interleaveBefore(excerpt Excerpt, in drawnIn, after []Line) []Line {
	afterText := map[int]string{}
	for _, line := range after {
		afterText[line.Number] = line.Text
	}
	newLine := func(n int) Line {
		return Line{
			Number:  n,
			Text:    afterText[n],
			Side:    NewSide,
			Changed: s.ledger.isChanged(excerpt.Repository, excerpt.File, NewSide, n),
		}
	}

	var out []Line
	cursor := excerpt.FirstLine
	for _, c := range s.ledger.modificationsShownBy(excerpt) {
		// Where this Excerpt first shows the replacement, which is where its
		// before-side belongs — the modification may have begun before the range.
		start := c.NewFirst
		if start < excerpt.FirstLine {
			start = excerpt.FirstLine
		}
		for n := cursor; n < start && n <= excerpt.LastLine; n++ {
			out = append(out, newLine(n))
		}
		// The before-side belongs to the Step, not to each of its ranges: a Step
		// showing one rewrite through several Excerpts draws the removal once,
		// above the first of them, rather than repeating it beside each.
		if firstShowing(in.step, c) == in.at {
			out = append(out, s.beforeRows(c, in)...)
		}
		last := c.NewLast
		if last > excerpt.LastLine {
			last = excerpt.LastLine
		}
		for n := start; n <= last; n++ {
			out = append(out, newLine(n))
		}
		cursor = last + 1
	}
	for n := cursor; n <= excerpt.LastLine; n++ {
		out = append(out, newLine(n))
	}
	return out
}

// beforeRows are the removed lines drawn above a modification's replacement in
// one Step: the before-side assigned to it, or — when another Step has all of it
// — a signpost saying so, rather than an after-side with nothing to read it
// against.
func (s *Session) beforeRows(c Correspondence, in drawnIn) []Line {
	assigned := assignedBefore(c, in.step, in.all)
	if len(assigned) == 0 {
		return []Line{{
			Number: c.OldFirst, Side: OldSide, Signpost: true,
			Text: signpostText(c, in),
		}}
	}

	var out []Line
	for _, run := range runsOf(assigned) {
		before, err := s.resolver.Resolve(Excerpt{
			Repository: c.Repository, File: c.File, Side: OldSide,
			FirstLine: run.first, LastLine: run.last,
		}, s.latest.Round)
		if err != nil {
			// The before-side is accounted for by being drawn here, so it must not
			// vanish silently, or a covered line would go unshown. Mark the gap.
			out = append(out, Line{Number: run.first, Side: OldSide, Unreadable: true,
				Text: fmt.Sprintf("(the before-side could not be read: %v)", err)})
			continue
		}
		for _, line := range before {
			line.Side = OldSide
			line.Changed = true
			out = append(out, line)
		}
	}
	return out
}

// signpostText says where a modification's before-side went, so a Step showing
// only part of a rewrite does not read as an addition out of nowhere. It names
// every Step holding part of it: a rewrite split three ways has its before-side
// split too, and naming only the first would send the Reviewer to a Step that
// has half of what they are looking for.
func signpostText(c Correspondence, in drawnIn) string {
	var holders []string
	for i, other := range in.all {
		if len(assignedBefore(c, other, in.all)) > 0 {
			holders = append(holders, fmt.Sprintf("%d", i+1))
		}
	}
	return fmt.Sprintf("⋯ replaces old %d-%d%s", c.OldFirst, c.OldLast, shownIn(holders))
}

// shownIn reads a list of Step numbers as a phrase, or says nothing when no Step
// holds the before-side at all.
func shownIn(holders []string) string {
	switch len(holders) {
	case 0:
		return ""
	case 1:
		return ", shown in Step " + holders[0]
	default:
		return ", shown in Steps " +
			strings.Join(holders[:len(holders)-1], ", ") + " and " + holders[len(holders)-1]
	}
}

// acknowledgementView builds the manifest for one Acknowledgement from the
// ledger: how many lines it stands in for, or which Opaque Change it accounts for.
func (s *Session) acknowledgementView(ack Acknowledgement) AcknowledgementView {
	view := AcknowledgementView{Reason: ack.Reason}
	for _, file := range ack.Files {
		entry := AcknowledgedFile{Repository: ack.Repository, File: file}
		if opaque, ok := s.ledger.opaqueFor(ack.Repository, file); ok {
			entry.Opaque = opaque.Kind
			entry.OpaqueDetail = opaque.Detail
			entry.Change = string(opaque.Kind)
		} else {
			lines := s.ledger.changedLinesFor(ack.Repository, file)
			entry.ChangedLines = len(lines)
			entry.Change = classifyChange(lines)
		}
		view.Entries = append(view.Entries, entry)
	}
	return view
}

// classifyChange reads a file's Changed Lines as added (new side only), removed
// (old side only), or modified (both) — what git saw happen to it.
func classifyChange(lines []ChangedLine) string {
	hasOld, hasNew := false, false
	for _, line := range lines {
		if line.Side == OldSide {
			hasOld = true
		} else {
			hasNew = true
		}
	}
	switch {
	case hasOld && !hasNew:
		return ChangeRemoved
	case hasNew && !hasOld:
		return ChangeAdded
	default:
		return ChangeModified
	}
}

// ExpandAcknowledgement resolves an Acknowledgement into the Excerpts it stands
// in for — the actual Changed Lines of each file it claims. It is the Reviewer
// calling a bulk claim: it shows the code the Acknowledgement asked to skip
// without altering the plan. An Opaque Change has no lines and says so.
func (s *Session) ExpandAcknowledgement(stepPosition, ackIndex int) ([]ExcerptView, error) {
	if s.walkthrough == nil {
		return nil, reject(RejectedNoWalkthrough, "there is no Walkthrough to expand")
	}
	if stepPosition < 1 || stepPosition > len(s.walkthrough.Steps) {
		return nil, reject(RejectedNoSuchStep,
			"there is no Step %d; this Walkthrough has %d", stepPosition, len(s.walkthrough.Steps))
	}
	step := s.walkthrough.Steps[stepPosition-1]
	if ackIndex < 0 || ackIndex >= len(step.Acknowledgements) {
		return nil, reject(RejectedNoSuchAcknowledgement,
			"Step %d has no Acknowledgement %d", stepPosition, ackIndex+1)
	}
	ack := step.Acknowledgements[ackIndex]

	parts := s.acknowledgedParts(ack)
	var views []ExcerptView
	for i := range parts {
		part := parts[i]
		if part.opaque != nil {
			views = append(views, ExcerptView{
				Excerpt: Excerpt{Repository: ack.Repository, File: part.opaque.File},
				Problem: fmt.Sprintf("%s is an Opaque Change (%s) with no lines to show", part.opaque.File, part.opaque.Detail),
			})
			continue
		}
		views = append(views, s.resolveExcerpt(part.excerpt, expansionContext(parts, i)))
	}
	return views, nil
}

// expansionContext makes an Acknowledgement's expansion its own Walkthrough of
// one Step, positioned at the part being drawn.
//
// An expansion is a complete account of a file's changes, not a Step's editorial
// selection, so it draws every before-side it covers regardless of what the
// Steps around it claim — a claim made elsewhere in the Walkthrough must not
// take a removal out of the diff the Reviewer asked to see in full.
func expansionContext(parts []acknowledgedPart, at int) drawnIn {
	step := Step{}
	position := -1
	for i, part := range parts {
		if part.opaque != nil {
			continue
		}
		if i == at {
			position = len(step.Excerpts)
		}
		step.Excerpts = append(step.Excerpts, part.excerpt)
	}
	return drawnIn{step: step, at: position, all: []Step{step}}
}

// acknowledgedPart is one piece of what an Acknowledgement expands into: an
// Excerpt of a file's Changed Lines, or an Opaque Change, which has none.
type acknowledgedPart struct {
	excerpt Excerpt
	opaque  *OpaqueChange
}

// acknowledgedParts derives, from the ledger alone, what an Acknowledgement
// stands in for, in the order it is drawn. It is the one source for both the
// expansion the Reviewer reads and the Excerpt an Anchor into it resolves against.
func (s *Session) acknowledgedParts(ack Acknowledgement) []acknowledgedPart {
	var parts []acknowledgedPart
	for _, file := range ack.Files {
		if opaque, ok := s.ledger.opaqueFor(ack.Repository, file); ok {
			parts = append(parts, acknowledgedPart{opaque: &opaque})
			continue
		}
		for _, excerpt := range s.ledger.expansionExcerpts(ack.Repository, file) {
			parts = append(parts, acknowledgedPart{excerpt: excerpt})
		}
	}
	return parts
}

func (s *Session) seenFlags() []bool {
	flags := make([]bool, len(s.walkthrough.Steps))
	for i := range flags {
		flags[i] = s.seen[i+1]
	}
	return flags
}

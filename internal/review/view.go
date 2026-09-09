package review

import "fmt"

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
}

// Resolver turns an Excerpt into the lines it names. The core performs no I/O:
// whoever constructs the Session supplies the thing that reads bytes, and
// resolution happens at render time so a file changing on disk is noticed the
// next time it is drawn, not hidden behind a cache.
type Resolver interface {
	Resolve(Excerpt) ([]Line, error)
}

// ChangeSetAware is an optional capability of a Resolver that reads the old side:
// the before-side of a change lives in git at each repository's merge-base, which
// needs that repository's range. The Session hands the resolver the Change Set
// when a Walkthrough is posted. A resolver that reads only the working tree (or a
// test stub) need not implement it.
type ChangeSetAware interface {
	UseChangeSet(ChangeSet)
}

// ExcerptView is an Excerpt with its content resolved, or the reason it could
// not be. The two are mutually exclusive: dbn never renders code it could not
// actually read.
type ExcerptView struct {
	Excerpt Excerpt
	Lines   []Line
	Problem string
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
	// Stale is set when a file this Step reads has changed since the Walkthrough
	// was accepted; StaleFiles names them. A stale Step shows no code — the
	// explanation can no longer be trusted to describe what is on disk.
	Stale      bool
	StaleFiles []string
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
	Posted       bool
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
	StepStatuses   []StepStatus
	ChangeRequests []ChangeRequest
	Finished       bool
	// Dispositions accounts for the previous round's Change Requests in a Revision
	// Round — shown before any code, so a decline is seen before the fix.
	Dispositions []ResolvedDisposition
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
		Brief:        w.Brief,
		StepNames:    names,
		StepCount:    len(w.Steps),
		Position:     s.position,
		Repositories: w.ChangeSet.Repositories,
		Coverage: Coverage{
			Seen:  s.ledger.seenBy(w.Steps, s.position, s.preShown),
			Total: s.ledger.total(),
		},
		Seen:           s.seenFlags(),
		StepStatuses:   s.stepStatuses(),
		ChangeRequests: s.ChangeRequests(),
		Finished:       s.finished,
		Dispositions:   s.Dispositions(),
	}
	if s.position > 0 {
		view.Step = s.stepView(s.position)
	}
	return view
}

func (s *Session) stepView(position int) *StepView {
	step := s.walkthrough.Steps[position-1]
	// A Step whose files have changed refuses to render: showing code beneath an
	// explanation that no longer describes it is the worst thing this tool could
	// do. The blast radius is only this Step — others render normally.
	if stale := s.staleFiles(step); len(stale) > 0 {
		return &StepView{
			Number:      position,
			Name:        step.Name,
			Explanation: step.Explanation,
			Stale:       true,
			StaleFiles:  stale,
		}
	}
	excerpts := make([]ExcerptView, 0, len(step.Excerpts))
	for _, excerpt := range step.Excerpts {
		excerpts = append(excerpts, s.resolveExcerpt(excerpt))
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

// resolveExcerpt reads an Excerpt's lines and marks the Changed ones, or records
// why it could not be read. dbn never renders code it could not actually read.
//
// A new-side Excerpt renders as a unified diff: its after-side lines, with the
// before-side of each edit it shows injected as removed lines just above their
// replacement. An old-side Excerpt is a deliberately shown deletion and renders
// before-only.
func (s *Session) resolveExcerpt(excerpt Excerpt) ExcerptView {
	lines, err := s.resolver.Resolve(excerpt)
	if err != nil {
		return ExcerptView{Excerpt: excerpt, Problem: err.Error()}
	}
	if excerpt.Side == NewSide {
		return ExcerptView{Excerpt: excerpt, Lines: s.interleaveBefore(excerpt, lines)}
	}
	for i := range lines {
		lines[i].Side = excerpt.Side
		lines[i].Changed = s.ledger.isChanged(excerpt.Repository, excerpt.File, excerpt.Side, lines[i].Number)
	}
	return ExcerptView{Excerpt: excerpt, Lines: lines}
}

// interleaveBefore turns a new-side Excerpt's after-side lines into a unified
// diff: for each edit whose replacement begins inside the Excerpt, the before-side
// lines it removed are read and placed immediately above their replacement, so the
// Reviewer reads "these lines became these" as one thought. Reference lines stay
// unmarked; a before-side that cannot be read is left out rather than faked.
func (s *Session) interleaveBefore(excerpt Excerpt, after []Line) []Line {
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
		for n := cursor; n < c.NewFirst && n <= excerpt.LastLine; n++ {
			out = append(out, newLine(n))
		}
		before, err := s.resolver.Resolve(Excerpt{
			Repository: excerpt.Repository, File: excerpt.File, Side: OldSide,
			FirstLine: c.OldFirst, LastLine: c.OldLast,
		})
		if err != nil {
			// The before-side rode along, so it is accounted for — it must not vanish
			// silently, or a covered line would go unshown. Mark the gap instead.
			out = append(out, Line{Number: c.OldFirst, Side: OldSide,
				Text: fmt.Sprintf("(the before-side could not be read: %v)", err)})
		}
		for _, line := range before {
			line.Side = OldSide
			line.Changed = true
			out = append(out, line)
		}
		last := c.NewLast
		if last > excerpt.LastLine {
			last = excerpt.LastLine
		}
		for n := c.NewFirst; n <= last; n++ {
			out = append(out, newLine(n))
		}
		cursor = last + 1
	}
	for n := cursor; n <= excerpt.LastLine; n++ {
		out = append(out, newLine(n))
	}
	return out
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

	var views []ExcerptView
	for _, file := range ack.Files {
		if opaque, ok := s.ledger.opaqueFor(ack.Repository, file); ok {
			views = append(views, ExcerptView{
				Excerpt: Excerpt{Repository: ack.Repository, File: file},
				Problem: fmt.Sprintf("%s is an Opaque Change (%s) with no lines to show", file, opaque.Detail),
			})
			continue
		}
		for _, excerpt := range excerptsForChangedLines(ack.Repository, file, s.ledger.changedLinesFor(ack.Repository, file)) {
			views = append(views, s.resolveExcerpt(excerpt))
		}
	}
	return views, nil
}

func (s *Session) seenFlags() []bool {
	flags := make([]bool, len(s.walkthrough.Steps))
	for i := range flags {
		flags[i] = s.seen[i+1]
	}
	return flags
}

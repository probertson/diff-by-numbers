package review

// Line is one row of resolved file content, tagged with whether git considers it
// a Changed Line (versus reference context the Excerpt included for readability).
type Line struct {
	Number  int
	Text    string
	Changed bool
}

// Resolver turns an Excerpt into the lines it names. The core performs no I/O:
// whoever constructs the Session supplies the thing that reads bytes, and
// resolution happens at render time so a file changing on disk is noticed the
// next time it is drawn, not hidden behind a cache.
type Resolver interface {
	Resolve(Excerpt) ([]Line, error)
}

// ExcerptView is an Excerpt with its content resolved, or the reason it could
// not be. The two are mutually exclusive: dbn never renders code it could not
// actually read.
type ExcerptView struct {
	Excerpt Excerpt
	Lines   []Line
	Problem string
}

// StepView is one Step ready to draw.
type StepView struct {
	Number                int
	Name                  string
	Explanation           string
	OversizeJustification string
	Excerpts              []ExcerptView
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
			Seen:  s.ledger.seenBy(w.Steps, s.position),
			Total: s.ledger.total(),
		},
		Seen:           s.seenFlags(),
		StepStatuses:   s.stepStatuses(),
		ChangeRequests: s.ChangeRequests(),
		Finished:       s.finished,
	}
	if s.position > 0 {
		view.Step = s.stepView(s.position)
	}
	return view
}

func (s *Session) stepView(position int) *StepView {
	step := s.walkthrough.Steps[position-1]
	excerpts := make([]ExcerptView, 0, len(step.Excerpts))
	for _, excerpt := range step.Excerpts {
		lines, err := s.resolver.Resolve(excerpt)
		if err != nil {
			excerpts = append(excerpts, ExcerptView{Excerpt: excerpt, Problem: err.Error()})
			continue
		}
		for i := range lines {
			lines[i].Changed = s.ledger.isChanged(excerpt.Repository, excerpt.File, excerpt.Side, lines[i].Number)
		}
		excerpts = append(excerpts, ExcerptView{Excerpt: excerpt, Lines: lines})
	}
	return &StepView{
		Number:                position,
		Name:                  step.Name,
		Explanation:           step.Explanation,
		OversizeJustification: step.OversizeJustification,
		Excerpts:              excerpts,
	}
}

func (s *Session) seenFlags() []bool {
	flags := make([]bool, len(s.walkthrough.Steps))
	for i := range flags {
		flags[i] = s.seen[i+1]
	}
	return flags
}

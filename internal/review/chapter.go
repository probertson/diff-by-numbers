package review

import (
	"fmt"
	"strings"
)

// Chapter is a named run of consecutive Steps in a Round, opened by an intro in
// the Authoring Agent's own words. A Chapter holds its Steps rather than pointing
// at them, so the shape itself keeps Chapters consecutive, complete and
// un-nested: no Problem can arise for those rules.
type Chapter struct {
	Name  string
	Intro string
	Steps []Step
}

// ChapterView is one Chapter of the Round on screen, with the Steps it holds
// named by their numbers in the Round.
type ChapterView struct {
	// Number counts the Round's Chapters from 1.
	Number    int
	Name      string
	Intro     string
	FirstStep int
	LastStep  int
	// Seen reports that the Reviewer has seen every Step in the Chapter.
	Seen bool
	// QuestionCount is how many Agent Questions the Chapter's Steps ask.
	QuestionCount int
	// UnchangedSincePrevious reports that nothing any of the Chapter's Steps
	// shows is new or edited since the previous round; false in a first round.
	UnchangedSincePrevious bool
}

// chapterSpan is where an accepted Round's Chapter sits among its Steps, which
// are held as one list once the Round is accepted.
type chapterSpan struct {
	name, intro string
	first, last int
}

// flattened lays a Round's Steps out as one list, numbered straight through
// across its Chapters, and says where each Chapter sits in it. Every check and
// every view after this reads the one list, so a Step's number means the same
// with Chapters as without them.
func (w Round) flattened() (Round, []chapterSpan) {
	if len(w.Chapters) == 0 {
		return w, nil
	}
	steps := append([]Step(nil), w.Steps...)
	var spans []chapterSpan
	for _, chapter := range w.Chapters {
		first := len(steps) + 1
		steps = append(steps, chapter.Steps...)
		spans = append(spans, chapterSpan{name: chapter.Name, intro: chapter.Intro, first: first, last: len(steps)})
	}
	w.Steps = steps
	w.Chapters = nil
	return w, spans
}

// validateChapters checks what the nesting cannot: that a Round chose one shape,
// and that each Chapter has something to say and something to show. Each kind of
// fault is one Problem naming every Chapter that has it, so the agent fixes them
// all before posting again.
//
// None of these faults leaves the rest of the post unreadable, so they are
// reported with whatever else the post gets wrong rather than on their own.
func validateChapters(w Round) []Problem {
	if len(w.Chapters) == 0 {
		return nil
	}
	var problems []Problem
	if len(w.Steps) > 0 {
		problems = append(problems, Problem{Reason: RejectedStepsAndChapters,
			Detail: "the Round gives both steps and chapters; give a flat list of steps, or chapters each holding its own steps, not both"})
	}
	var empty, unnamed, noIntro []string
	for i, chapter := range w.Chapters {
		which := fmt.Sprintf("Chapter %d", i+1)
		if len(chapter.Steps) == 0 {
			empty = append(empty, which)
		}
		if strings.TrimSpace(chapter.Name) == "" {
			unnamed = append(unnamed, which)
		}
		if strings.TrimSpace(chapter.Intro) == "" {
			noIntro = append(noIntro, which)
		}
	}
	if len(empty) > 0 {
		problems = append(problems, Problem{Reason: RejectedEmptyChapter,
			Detail: fmt.Sprintf("%s %s no steps; every Chapter shows something, so give it its steps or leave it out",
				strings.Join(empty, ", "), hasOrHave(len(empty)))})
	}
	var malformed []string
	if len(unnamed) > 0 {
		malformed = append(malformed, fmt.Sprintf("%s %s no name", strings.Join(unnamed, ", "), hasOrHave(len(unnamed))))
	}
	if len(noIntro) > 0 {
		malformed = append(malformed, fmt.Sprintf("%s %s no intro", strings.Join(noIntro, ", "), hasOrHave(len(noIntro))))
	}
	if len(malformed) > 0 {
		problems = append(problems, Problem{Reason: RejectedMalformedChapter,
			Detail: strings.Join(malformed, "; ") + "; every Chapter needs a name the Reviewer knows it by and an intro saying what it covers and why it comes where it does"})
	}
	return problems
}

func hasOrHave(n int) string {
	if n == 1 {
		return "has"
	}
	return "have"
}

// chapterOf is the Chapter a Step is in, numbered from 1, or 0 in a Round
// without Chapters.
func (s *Session) chapterOf(step int) int {
	for i, span := range s.chapters {
		if step >= span.first && step <= span.last {
			return i + 1
		}
	}
	return 0
}

// chapterViews describes the Chapters of the Round on screen, or none.
func (s *Session) chapterViews() []ChapterView {
	unchanged := s.previousUnchanged()
	var views []ChapterView
	for i, span := range s.chapters {
		view := ChapterView{
			Number: i + 1, Name: span.name, Intro: span.intro, FirstStep: span.first, LastStep: span.last,
			Seen: true, UnchangedSincePrevious: unchanged != nil,
		}
		for step := span.first; step <= span.last; step++ {
			view.Seen = view.Seen && s.seen[step]
			view.QuestionCount += len(s.questionsOnStep(step))
			view.UnchangedSincePrevious = view.UnchangedSincePrevious && step <= len(unchanged) && unchanged[step-1]
		}
		views = append(views, view)
	}
	return views
}

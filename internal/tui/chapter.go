package tui

import (
	"fmt"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/daemon"
	"github.com/probertson/diff-by-numbers/internal/present"
)

// onIntro reports whether a Chapter's intro is on screen. The first Chapter's
// intro shares the Overview's position, before any Step, so the position alone
// cannot tell the two apart.
func (m model) onIntro() bool {
	return m.view != nil && m.view.Posted && m.view.ChapterIntro > 0
}

// onOverview reports whether the Overview is on screen.
func (m model) onOverview() bool {
	return m.view != nil && m.view.Posted && m.view.Position == 0 && m.view.ChapterIntro == 0
}

// chapter is the Round's Chapter numbered n, from 1, and whether there is one.
func (m model) chapter(n int) (daemon.ChapterWire, bool) {
	if m.view == nil || n < 1 || n > len(m.view.Chapters) {
		return daemon.ChapterWire{}, false
	}
	return m.view.Chapters[n-1], true
}

// sameStop reports whether two views stand at the same stop. Position and
// ChapterIntro name a stop together: a Chapter's intro shares its position
// with the Step before it, or with the Overview for the first Chapter.
func sameStop(a, b *daemon.ViewWire) bool {
	return a.Position == b.Position && a.ChapterIntro == b.ChapterIntro
}

// chapterHeading names a Chapter by number and name, e.g. "3 · EL-7 Pack
// holds". On screen the word "Chapter" is the Overview's heading and the
// keybar's "go to Chapter" only.
func chapterHeading(chapter daemon.ChapterWire) string {
	return present.ChapterHeading(chapter.Number, chapter.Name)
}

// stepRange names the Steps a Chapter holds, e.g. "Steps 12–16". The
// Overview's rows show it alone; an intro's header places it among the Round's
// Steps, e.g. "Steps 12–16 of 40".
func stepRange(chapter daemon.ChapterWire) string {
	return present.StepRange(chapter.FirstStep, chapter.LastStep)
}

// chapterRows is the Overview's list of a Round's Chapters, one row each in
// place of the Steps. The rows are for reading, not picking: the Reviewer
// reaches a Chapter by moving through the Round, or by its number.
func (m model) chapterRows() string {
	var b strings.Builder
	b.WriteString(labelSt.Render("Chapters") + "\n")
	for _, chapter := range m.view.Chapters {
		mark := " "
		if chapter.Seen {
			mark = "✓"
		}
		unchanged := ""
		if chapter.UnchangedSincePrevious {
			unchanged = dimSt.Render(fmt.Sprintf(" unchanged since round %d", m.view.PreviousRound))
		}
		b.WriteString(fmt.Sprintf("  %s %2d. %s  %s%s%s\n", dimSt.Render(mark), chapter.Number, chapter.Name,
			dimSt.Render(stepRange(chapter)), unchanged, askedMark(chapter.QuestionCount)))
	}
	return b.String()
}

// stepChapterHeading names the Chapter of the Step in view, or "" in a Round
// without Chapters.
func (m model) stepChapterHeading() string {
	if !m.inStep() {
		return ""
	}
	chapter, ok := m.chapter(m.view.Step.Chapter)
	if !ok {
		return ""
	}
	return chapterHeading(chapter)
}

// intro draws a Chapter's intro: its number and name, the agent's intro, and
// the Chapter's Steps, marking those that ask the Reviewer something. It is for
// reading, not picking: the Reviewer moves on through the Steps in order.
func (m model) intro() string {
	chapter, ok := m.chapter(m.view.ChapterIntro)
	if !ok {
		return ""
	}
	width := proseWidthFor(m.viewport.Width, m.proseWidth)

	var b strings.Builder
	b.WriteString(labelSt.Render(chapterHeading(chapter)) + "\n\n")
	b.WriteString(wrapTo(chapter.Intro, width) + "\n\n")
	for step := chapter.FirstStep; step <= chapter.LastStep && step <= len(m.view.StepNames); step++ {
		b.WriteString(fmt.Sprintf("  %2d. %s%s\n", step, m.view.StepNames[step-1], m.asksMark(step)))
	}
	return b.String()
}

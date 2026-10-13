package review_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// namedStep is a Step that shows one file's first lines, over a diff that need
// not cover anything, so Chapters can be exercised without coverage in the way.
func namedStep(name string) review.Step {
	return review.Step{Name: name, Explanation: "what " + name + " shows", Excerpts: oneExcerpt(name + ".ts")}
}

// chapteredRound is a Round of five Steps in three Chapters: two, one and two.
func chapteredRound() review.Round {
	walkthrough := validRound()
	walkthrough.Steps = nil
	walkthrough.Chapters = []review.Chapter{
		{Name: "Outline", Intro: "what the Round is about", Steps: []review.Step{namedStep("a"), namedStep("b")}},
		{Name: "EL-7 Pack holds", Intro: "the holds come first", Steps: []review.Step{namedStep("c")}},
		{Name: "Tidying", Intro: "what is left", Steps: []review.Step{namedStep("d"), namedStep("e")}},
	}
	return walkthrough
}

func chapteredSession(t *testing.T) *review.Session {
	t.Helper()
	session := newSession()
	mustPost(t, session, chapteredRound())
	return session
}

func TestARoundInChaptersNumbersItsStepsStraightThrough(t *testing.T) {
	session := chapteredSession(t)

	view := session.View()

	want := []string{"a", "b", "c", "d", "e"}
	if view.StepCount != len(want) {
		t.Fatalf("expected %d Steps across the Chapters, got %d", len(want), view.StepCount)
	}
	for i, name := range want {
		if view.StepNames[i] != name {
			t.Errorf("expected Step %d to be %q, got %q", i+1, name, view.StepNames[i])
		}
	}
	wantChapters := []review.ChapterView{
		{Number: 1, Name: "Outline", Intro: "what the Round is about", FirstStep: 1, LastStep: 2},
		{Number: 2, Name: "EL-7 Pack holds", Intro: "the holds come first", FirstStep: 3, LastStep: 3},
		{Number: 3, Name: "Tidying", Intro: "what is left", FirstStep: 4, LastStep: 5},
	}
	if len(view.Chapters) != len(wantChapters) {
		t.Fatalf("expected %d Chapters, got %+v", len(wantChapters), view.Chapters)
	}
	for i, chapter := range wantChapters {
		if view.Chapters[i] != chapter {
			t.Errorf("expected Chapter %d to be %+v, got %+v", i+1, chapter, view.Chapters[i])
		}
	}
}

// problemDetail is what a rejection says about one of its reasons.
func problemDetail(t *testing.T, err error, reason review.RejectionReason) string {
	t.Helper()
	var rejection *review.Rejection
	if !errors.As(err, &rejection) {
		t.Fatalf("expected a structured rejection, got %v", err)
	}
	for _, problem := range rejection.Problems {
		if problem.Reason == reason {
			return problem.Detail
		}
	}
	t.Fatalf("expected a %q problem, got %v", reason, reasonsOf(rejection))
	return ""
}

func TestARoundGivingBothStepsAndChaptersIsRefused(t *testing.T) {
	walkthrough := chapteredRound()
	walkthrough.Steps = []review.Step{namedStep("flat")}

	err := newSession().Post(walkthrough)

	assertProblems(t, err, review.RejectedStepsAndChapters)
}

func TestEveryChapterProblemIsReportedTogether(t *testing.T) {
	walkthrough := chapteredRound()
	walkthrough.Steps = []review.Step{namedStep("flat")}
	walkthrough.Chapters[0].Name = ""
	walkthrough.Chapters[1].Steps = nil
	walkthrough.Chapters[2].Intro = ""

	err := newSession().Post(walkthrough)

	assertProblems(t, err,
		review.RejectedStepsAndChapters,
		review.RejectedEmptyChapter,
		review.RejectedMalformedChapter)
}

func TestAChapterWithNoNameOrNoIntroIsNamedInTheProblem(t *testing.T) {
	walkthrough := chapteredRound()
	walkthrough.Chapters[0].Name = ""
	walkthrough.Chapters[2].Intro = ""

	err := newSession().Post(walkthrough)

	assertProblems(t, err, review.RejectedMalformedChapter)
	detail := problemDetail(t, err, review.RejectedMalformedChapter)
	for _, want := range []string{"Chapter 1", "Chapter 3"} {
		if !strings.Contains(detail, want) {
			t.Errorf("expected the problem to name %s, got %q", want, detail)
		}
	}
}

// A Chapter Problem is no reason to keep back the rest: everything else the post
// gets wrong is reported with it.
func TestAChapterProblemIsReportedAlongsideTheRest(t *testing.T) {
	walkthrough := chapteredRound()
	walkthrough.Chapters[1].Intro = ""
	session := sessionDeriving(changed("elsewhere.ts", 1, 3)) // uncovered

	err := session.Post(walkthrough)

	assertProblems(t, err, review.RejectedMalformedChapter, review.RejectedUncoveredChanges)
}

func TestAChapterProblemIsReportedAlongsideAStructuralOne(t *testing.T) {
	walkthrough := chapteredRound()
	walkthrough.Chapters[1].Steps = nil
	walkthrough.Brief.Goal = ""

	err := newSession().Post(walkthrough)

	assertProblems(t, err, review.RejectedEmptyChapter, review.RejectedMalformedBrief)
}

// stop names where the Reviewer is, as the view reports it.
func stop(view review.ViewModel) string {
	switch {
	case view.ChapterIntro > 0:
		return fmt.Sprintf("intro %d", view.ChapterIntro)
	case view.Step != nil:
		return fmt.Sprintf("step %d", view.Step.Number)
	}
	return "overview"
}

func TestAdvancingWalksEachChaptersIntroBeforeItsSteps(t *testing.T) {
	session := chapteredSession(t)
	want := []string{
		"intro 1", "step 1", "step 2",
		"intro 2", "step 3",
		"intro 3", "step 4", "step 5",
		"step 5", // the last Step stays put
	}

	var got []string
	for range want {
		mustAdvance(t, session)
		got = append(got, stop(session.View()))
	}

	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		t.Errorf("expected to advance through\n  %v\ngot\n  %v", want, got)
	}
}

func TestGoingBackFromAChaptersFirstStepLandsOnItsOwnIntro(t *testing.T) {
	session := chapteredSession(t)
	if err := session.GoTo(5); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"step 4", "intro 3",
		"step 3", "intro 2",
		"step 2", "step 1", "intro 1",
		"overview", "overview",
	}

	var got []string
	for range want {
		mustBack(t, session)
		got = append(got, stop(session.View()))
	}

	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		t.Errorf("expected to go back through\n  %v\ngot\n  %v", want, got)
	}
}

func TestGoingToAChapterLandsOnItsIntro(t *testing.T) {
	session := chapteredSession(t)

	err := session.GoToChapter(3)

	if err != nil {
		t.Fatalf("expected to go to Chapter 3, got %v", err)
	}
	if got := stop(session.View()); got != "intro 3" {
		t.Errorf("expected to land on Chapter 3's intro, got %s", got)
	}
}

func TestAChaptersIntroLeadsToItsFirstStep(t *testing.T) {
	session := chapteredSession(t)
	if err := session.GoToChapter(3); err != nil {
		t.Fatal(err)
	}

	mustAdvance(t, session)

	if got := stop(session.View()); got != "step 4" {
		t.Errorf("expected Chapter 3's intro to lead to its first Step, got %s", got)
	}
}

func TestGoingToAChapterTheRoundDoesNotHaveIsRefused(t *testing.T) {
	chaptered := chapteredSession(t)
	flat := threeStepSession(t)

	for _, n := range []int{0, 4} {
		assertRejected(t, chaptered.GoToChapter(n), review.RejectedNoSuchChapter)
	}
	assertRejected(t, flat.GoToChapter(1), review.RejectedNoSuchChapter)
}

// An intro is a stop, not a Step: reaching it marks nothing seen, however it
// was reached.
func TestAnIntroMarksNoStepSeen(t *testing.T) {
	session := chapteredSession(t)

	if err := session.GoToChapter(3); err != nil {
		t.Fatal(err)
	}

	for i, seen := range session.View().Seen {
		if seen {
			t.Errorf("expected no Step seen at an intro, but Step %d was", i+1)
		}
	}
}

func TestNothingCanBeAnchoredOnAnIntro(t *testing.T) {
	session := chapteredSession(t)
	if err := session.GoTo(2); err != nil {
		t.Fatal(err)
	}
	mustAdvance(t, session) // Chapter 2's intro

	_, err := session.Anchor(span(0, 1, 1))

	assertRejected(t, err, review.RejectedNoSuchStep)
}

func TestARevisionRoundCanDeclareDifferentChapters(t *testing.T) {
	session := chapteredSession(t)
	handOffWithAComment(t, session)
	revision := validRound()
	revision.Steps = nil
	revision.Chapters = []review.Chapter{
		{Name: "The fix", Intro: "what changed since", Steps: []review.Step{namedStep("a"), namedStep("b"), namedStep("c")}},
		{Name: "The rest", Intro: "unchanged", Steps: []review.Step{namedStep("d"), namedStep("e")}},
	}

	mustRevise(t, session, revising(revision))

	chapters := session.View().Chapters
	if len(chapters) != 2 || chapters[0].Name != "The fix" || chapters[1].FirstStep != 4 {
		t.Errorf("expected the Revision Round's own two Chapters, got %+v", chapters)
	}
}

func TestARevisionRoundCanDeclareNoChapters(t *testing.T) {
	session := chapteredSession(t)
	handOffWithAComment(t, session)
	revision := validRound()
	revision.Steps = []review.Step{namedStep("a"), namedStep("b"), namedStep("c"), namedStep("d"), namedStep("e")}

	mustRevise(t, session, revising(revision))

	if chapters := session.View().Chapters; len(chapters) != 0 {
		t.Errorf("expected a Round without Chapters to carry none forward, got %+v", chapters)
	}
	mustAdvance(t, session)
	if got := stop(session.View()); got != "step 1" {
		t.Errorf("expected a Round without Chapters to have no intros, got %s", got)
	}
}

// Chapters only organise what the Reviewer sees: a Step in one is held to
// coverage and its budget as any other.
func TestChaptersLeaveCoverageAndBudgetsAsTheyWere(t *testing.T) {
	walkthrough := chapteredRound()
	walkthrough.Chapters[1].Steps[0].Excerpts[0] = review.Excerpt{
		Repository: "/repos/argus-portal", File: "big.ts", Side: review.NewSide, FirstLine: 1, LastLine: 200,
	}
	var lines []review.ChangedLine
	lines = append(lines, changed("big.ts", 1, 40)...)      // oversized
	lines = append(lines, changed("elsewhere.ts", 1, 3)...) // uncovered
	session := sessionDeriving(lines)

	err := session.Post(walkthrough)

	assertProblems(t, err, review.RejectedOversizedStep, review.RejectedUncoveredChanges)
}

// An intro shows no code, so the coverage it reports is what the Steps before
// it showed.
func TestCoverageOnAnIntroIsWhatTheStepsBeforeItShowed(t *testing.T) {
	session := sessionDeriving(changed("c.ts", 1, 3))
	mustPost(t, session, chapteredRound())

	if err := session.GoToChapter(2); err != nil {
		t.Fatal(err)
	}
	atIntro := session.View().Coverage
	mustAdvance(t, session)
	atStep := session.View().Coverage

	if atIntro.Seen != 0 || atStep.Seen != 3 {
		t.Errorf("expected nothing seen at Chapter 2's intro and its Step's 3 lines after, got %d then %d",
			atIntro.Seen, atStep.Seen)
	}
}

func TestAStepNamesItsChapter(t *testing.T) {
	session := chapteredSession(t)

	if err := session.GoTo(3); err != nil {
		t.Fatal(err)
	}

	if chapter := session.View().Step.Chapter; chapter != 2 {
		t.Errorf("expected Step 3 to be in Chapter 2, got %d", chapter)
	}
}

// The Overview's row for a Chapter is ticked only once the Reviewer has seen
// every Step in it, so a Chapter left half-read does not look finished.
func TestAChapterIsSeenOnceEveryStepInItIs(t *testing.T) {
	session := chapteredSession(t)
	for _, step := range []int{1, 2, 4} {
		if err := session.GoTo(step); err != nil {
			t.Fatal(err)
		}
	}

	chapters := session.View().Chapters

	var got []bool
	for _, chapter := range chapters {
		got = append(got, chapter.Seen)
	}
	if fmt.Sprint(got) != "[true false false]" {
		t.Errorf("expected only the fully read Chapter 1 seen, got %v", got)
	}
}

// A Chapter counts the Agent Questions its Steps ask, so the Overview can mark
// its row; a question on the Round belongs to no Chapter.
func TestAChapterCountsTheQuestionsItsStepsAsk(t *testing.T) {
	walkthrough := chapteredRound()
	walkthrough.Questions = []review.Question{{Text: "is this the right approach?"}}
	walkthrough.Chapters[0].Steps[1].Questions = []review.Question{{Text: "why b?"}}
	walkthrough.Chapters[2].Steps[0].Questions = []review.Question{{Text: "why d?"}}
	walkthrough.Chapters[2].Steps[1].Questions = []review.Question{{Text: "why e?"}, {Text: "and e again?"}}
	session := newSession()
	mustPost(t, session, walkthrough)

	chapters := session.View().Chapters

	var got []int
	for _, chapter := range chapters {
		got = append(got, chapter.QuestionCount)
	}
	if fmt.Sprint(got) != "[1 0 3]" {
		t.Errorf("expected each Chapter to count its own Steps' questions, got %v", got)
	}
}

// A Chapter reads as unchanged since the previous round only when every Step
// in it is: one changed Step is enough to send the Reviewer back in.
func TestAChapterIsUnchangedSinceThePreviousRoundOnlyWhenEveryStepInItIs(t *testing.T) {
	deriver := &roundsDeriver{
		fixedDeriver: fixedDeriver{lines: changedApp(1, 6)},
		touched:      map[string]bool{"app.ts:5": true},
		edits:        map[string][]review.RoundEdit{"app.ts": {{OldFirst: 5, OldCount: 1, NewFirst: 5, NewCount: 1}}},
	}
	session := review.NewSession(&textResolver{text: map[string]string{}}, deriver)
	walkthrough := appRound(nil, nil)
	walkthrough.Chapters = []review.Chapter{
		{Name: "Untouched", Intro: "as it was", Steps: []review.Step{appStep(1, 2), appStep(3, 3)}},
		{Name: "Touched", Intro: "one Step changed", Steps: []review.Step{appStep(4, 4), appStep(5, 6)}},
	}
	mustPost(t, session, walkthrough)
	handOffWithAComment(t, session)
	mustRevise(t, session, revising(walkthrough))

	chapters := session.View().Chapters

	var got []bool
	for _, chapter := range chapters {
		got = append(got, chapter.UnchangedSincePrevious)
	}
	if fmt.Sprint(got) != "[true false]" {
		t.Errorf("expected only the Chapter whose every Step is unchanged to say so, got %v", got)
	}
}

func TestNoChapterIsUnchangedInAFirstRound(t *testing.T) {
	session := chapteredSession(t)

	chapters := session.View().Chapters

	for _, chapter := range chapters {
		if chapter.UnchangedSincePrevious {
			t.Errorf("expected a first round to have nothing to compare with, but Chapter %d is unchanged", chapter.Number)
		}
	}
}

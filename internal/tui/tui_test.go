package tui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// widestLine is the width in cells of the widest row of s, ignoring the ANSI
// styling lipgloss embeds. It is how the wrapping tests assert that nothing runs
// off the right edge of the width the view was given.
func widestLine(s string) int {
	widest := 0
	for _, line := range strings.Split(s, "\n") {
		if w := lipgloss.Width(line); w > widest {
			widest = w
		}
	}
	return widest
}

// newNote builds a textarea configured the way the model's WindowSizeMsg handler
// does, so a view test that renders the note input matches what runs.
func newNote(width int) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "what should change here?"
	ta.CharLimit = 1000
	ta.ShowLineNumbers = false
	ta.SetWidth(max(20, width-4))
	ta.SetHeight(4)
	return ta
}

func TestBriefWrapsTheSourceCitation(t *testing.T) {
	// A long provenance citation used to run off the right edge because the Source
	// line was written without wrapping, unlike Goal and Approach.
	const width = 60
	m := model{
		width:    width,
		viewport: viewport.New(width, 20),
		view: &daemon.ViewWire{
			Posted: true,
			Brief: daemon.BriefWire{
				Ask:                "short ask",
				Approach:           "short approach",
				ProvenanceKind:     "stated",
				ProvenanceCitation: "the session on 2026-09-01 where the reviewer asked for the retry backoff to be threaded through every caller of the fetch layer",
			},
			Repositories: []daemon.RepositoryWire{{Root: "repo", Range: "main"}},
			StepNames:    []string{"one"},
			Seen:         []bool{false},
		},
	}

	out := m.brief()

	if over := widestLine(out); over > width {
		t.Errorf("the Source citation is %d cells wide, over the %d viewport — it did not wrap:\n%s", over, width, out)
	}
}

func TestNoteViewWrapsTheAnchorHeader(t *testing.T) {
	// The anchor's "Re: …" header is one long line; in the New Comment modal
	// it used to run off the edge instead of wrapping.
	const width = 50
	m := model{
		width: width,
		note:  newNote(width),
		pendingCode: "Re: internal/review/anchor.go:110-125 (new side) — Step \"Compose the paste-ready Anchor text\" in diff-by-numbers\n" +
			"+   110 | func (a Anchor) Render() string {\n",
	}

	out := m.noteView()

	if over := widestLine(out); over > width {
		t.Errorf("the anchor header is %d cells wide, over the %d modal — it did not wrap:\n%s", over, width, out)
	}
}

func TestNoteInputGrowsToTenLinesThenIsBoundedBySpace(t *testing.T) {
	tall := model{width: 80, height: 40, note: newNote(80)}
	tall.setNoteHeight()
	if got := tall.note.Height(); got != 10 {
		t.Errorf("with ample height the note should grow to 10 lines, got %d", got)
	}

	short := model{width: 80, height: 12, note: newNote(80)}
	short.setNoteHeight()
	if got := short.note.Height(); got < 1 || got >= 10 {
		t.Errorf("with little height the note should shrink below 10 (and stay >=1), got %d", got)
	}
}

func TestNoteViewShowsACharacterCounter(t *testing.T) {
	m := model{width: 80, height: 40, note: newNote(80)}
	m.note.SetValue("hello")

	out := m.noteView()

	if !strings.Contains(out, "5/1000") {
		t.Errorf("expected a '5/1000' character counter, got:\n%s", out)
	}
}

func TestListViewTitlePluralizesComments(t *testing.T) {
	m := model{
		width: 80,
		view:  &daemon.ViewWire{Comments: []daemon.CommentWire{{ID: 1, Step: 1, Note: "n"}}},
	}

	out := m.listView()

	if !strings.Contains(out, "1 Comment") || strings.Contains(out, "Request(s)") {
		t.Errorf("one Comment should read '1 Comment', got:\n%s", out)
	}
}

func TestDoneViewPluralizesItsCounts(t *testing.T) {
	// Finished with a Comment outstanding is the waiting face (State 1).
	m := model{
		view: &daemon.ViewWire{
			Finished:     true,
			StepStatuses: []string{"seen"},
			Comments:     []daemon.CommentWire{{ID: 1}},
		},
	}

	out := m.doneView()

	if strings.Contains(out, "(s)") {
		t.Errorf("the finish summary should not use the lazy (s) form, got:\n%s", out)
	}
	if !strings.Contains(out, "1 Step seen") {
		t.Errorf("expected '1 Step seen', got:\n%s", out)
	}
	if !strings.Contains(out, "1 Comment raised") {
		t.Errorf("expected '1 Comment raised', got:\n%s", out)
	}
}

func TestCtrlDArmsDeleteOnlyWhenEditingAnExistingRequest(t *testing.T) {
	editing := model{mode: modeNote, editingID: 7, note: newNote(80)}

	armed, _ := editing.updateNote(tea.KeyMsg{Type: tea.KeyCtrlD})

	if !armed.(model).confirmingDelete {
		t.Error("ctrl+d while editing an existing Comment should arm the delete confirm")
	}

	composing := model{mode: modeNote, editingID: 0, note: newNote(80)}

	still, _ := composing.updateNote(tea.KeyMsg{Type: tea.KeyCtrlD})

	if still.(model).confirmingDelete {
		t.Error("ctrl+d while composing a new Comment has nothing to delete and must not arm")
	}
}

func TestArmedDeleteCancelKeepsTheNoteAndStaysInTheEditor(t *testing.T) {
	m := model{mode: modeNote, editingID: 7, confirmingDelete: true, note: newNote(80)}
	m.note.SetValue("half-written feedback")

	cancelled, _ := m.updateNote(tea.KeyMsg{Type: tea.KeyEsc})
	cm := cancelled.(model)

	if cm.confirmingDelete {
		t.Error("esc should cancel the armed delete")
	}
	if cm.mode != modeNote {
		t.Error("cancelling the delete should leave the reviewer in the editor, not exit it")
	}
	if cm.note.Value() != "half-written feedback" {
		t.Errorf("the in-progress note should survive a cancelled delete, got %q", cm.note.Value())
	}
}

func TestArmedDeleteConfirmedReturnsToTheStep(t *testing.T) {
	m := model{mode: modeNote, editingID: 7, confirmingDelete: true, note: newNote(80)}

	deleted, _ := m.updateNote(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	dm := deleted.(model)

	if dm.confirmingDelete {
		t.Error("confirming the delete should disarm the prompt")
	}
	if dm.mode != modeReview {
		t.Error("deleting from the editor should return to the Step, matching esc")
	}
	if dm.editingID != 0 {
		t.Error("editingID should clear after the Comment is deleted")
	}
}

func TestArmedDeleteSwallowsStrayKeys(t *testing.T) {
	m := model{mode: modeNote, editingID: 7, confirmingDelete: true, note: newNote(80)}
	m.note.SetValue("draft")

	after, _ := m.updateNote(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	am := after.(model)

	if !am.confirmingDelete {
		t.Error("a stray key while armed should leave the delete armed, not disarm it")
	}
	if am.note.Value() != "draft" {
		t.Errorf("a stray key while armed must not leak into the note, got %q", am.note.Value())
	}
}

func TestListDeleteArmsBeforeWithdrawing(t *testing.T) {
	m := model{
		mode: modeList,
		view: &daemon.ViewWire{Comments: []daemon.CommentWire{{ID: 3, Step: 1, Note: "n"}}},
	}

	armed, _ := m.updateList("d")

	if !armed.(model).confirmingDelete {
		t.Error("d in the List should arm the confirm, not withdraw immediately")
	}
}

func TestArmedListDeleteEscCancelsRatherThanLeavingTheList(t *testing.T) {
	m := model{
		mode:             modeList,
		confirmingDelete: true,
		view:             &daemon.ViewWire{Comments: []daemon.CommentWire{{ID: 3, Step: 1, Note: "n"}}},
	}

	cancelled, _ := m.updateList("esc")
	cm := cancelled.(model)

	if cm.confirmingDelete {
		t.Error("esc should cancel the armed delete")
	}
	if cm.mode != modeList {
		t.Error("esc while a delete is armed should stay in the List, not exit to the Step")
	}
}

func TestArmedListDeleteConfirmedWithdrawsAndStaysInList(t *testing.T) {
	m := model{
		mode:             modeList,
		confirmingDelete: true,
		commentCursor:    0,
		view:             &daemon.ViewWire{Comments: []daemon.CommentWire{{ID: 3, Step: 1, Note: "n"}}},
	}

	confirmed, _ := m.updateList("y")
	cm := confirmed.(model)

	if cm.confirmingDelete {
		t.Error("confirming should disarm the prompt")
	}
	if cm.mode != modeList {
		t.Error("deleting from the List should stay in the List, not exit to the Step")
	}
}

func TestArmedListDeleteSwallowsStrayKeys(t *testing.T) {
	m := model{
		mode:             modeList,
		confirmingDelete: true,
		commentCursor:    0,
		view:             &daemon.ViewWire{Comments: []daemon.CommentWire{{ID: 3, Step: 1, Note: "n"}}},
	}

	after, _ := m.updateList("e")
	am := after.(model)

	if !am.confirmingDelete {
		t.Error("a stray key while armed should leave the delete armed, not disarm it")
	}
	if am.mode != modeList {
		t.Error("a stray key while armed must not act on the List (e would otherwise open the editor)")
	}
	if am.commentCursor != 0 {
		t.Errorf("a stray key while armed must not move the List cursor, got %d", am.commentCursor)
	}
}

func TestEditKeybarOffersDeleteOnlyWhenEditing(t *testing.T) {
	editing := model{editingID: 7}
	if !strings.Contains(editing.noteKeys(), "ctrl+d") {
		t.Error("editing an existing Comment should advertise ctrl+d delete")
	}

	composing := model{editingID: 0}
	if strings.Contains(composing.noteKeys(), "ctrl+d") {
		t.Error("composing a new Comment has nothing to delete, so must not advertise ctrl+d")
	}
	if strings.Contains(composing.noteKeys(), "newline") {
		t.Error("the unreliable shift+enter newline hint should be gone from the editor keybar")
	}
}

func TestArmedDeleteShowsPromptAsAToastAboveTheKeybar(t *testing.T) {
	m := model{
		mode:             modeNote,
		editingID:        7,
		confirmingDelete: true,
		ready:            true,
		width:            80,
		height:           24,
		note:             newNote(80),
		view:             &daemon.ViewWire{Posted: true},
	}

	out := m.View()

	if !strings.Contains(out, deleteConfirmPrompt) {
		t.Fatalf("an armed delete should show the confirm prompt, got:\n%s", out)
	}
	if !strings.Contains(out, "ctrl+d") {
		t.Error("the shortcut row should stay visible while the confirm is armed, not be replaced")
	}
	if strings.Index(out, "Delete this Comment") > strings.LastIndex(out, "ctrl+d") {
		t.Error("the confirm prompt should sit above the shortcut row, not below it")
	}
}

func TestUnarmedEditorShowsNoConfirmToast(t *testing.T) {
	m := model{
		mode:      modeNote,
		editingID: 7,
		ready:     true,
		width:     80,
		height:    24,
		note:      newNote(80),
		view:      &daemon.ViewWire{Posted: true},
	}

	out := m.View()

	if strings.Contains(out, deleteConfirmPrompt) {
		t.Errorf("the confirm prompt should only appear while a delete is armed, got:\n%s", out)
	}
}

// isQuit reports whether a command is tea.Quit (its message is a tea.QuitMsg).
func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestAdvancePastTheLastStepEntersTheConclusionScreen(t *testing.T) {
	last := model{
		mode: modeReview,
		view: &daemon.ViewWire{Posted: true, Position: 2, StepCount: 2, Step: &daemon.StepWire{Name: "s"}},
	}

	after, _ := last.Update(tea.KeyMsg{Type: tea.KeyRight})

	if after.(model).mode != modeConclusion {
		t.Error("advancing past the last Step should land on the conclusion screen")
	}
}

func TestAdvanceBeforeTheLastStepDoesNotEnterTheConclusionScreen(t *testing.T) {
	notLast := model{
		mode: modeReview,
		view: &daemon.ViewWire{Posted: true, Position: 1, StepCount: 2, Step: &daemon.StepWire{Name: "s"}},
	}

	after, _ := notLast.Update(tea.KeyMsg{Type: tea.KeyRight})

	if after.(model).mode == modeConclusion {
		t.Error("advancing before the last Step should not reach the conclusion screen")
	}
}

func TestConclusionBackReturnsToTheStep(t *testing.T) {
	m := model{mode: modeConclusion, view: &daemon.ViewWire{Posted: true, Position: 2, StepCount: 2}}

	after, _ := m.updateConclusion("left")

	if after.(model).mode != modeReview {
		t.Error("back from the conclusion screen should return to the Step")
	}
}

func TestConclusionHandOffGoesToTheHandedOffScreen(t *testing.T) {
	m := model{mode: modeConclusion, view: &daemon.ViewWire{Posted: true, Position: 2, StepCount: 2}}

	after, _ := m.updateConclusion("h")

	if after.(model).mode != modeDone {
		t.Error("h from the conclusion screen should hand off and show the handed-off screen")
	}
}

func TestQuitGuardArmsOnAnUnfinishedReviewThenASecondQQuits(t *testing.T) {
	m := model{mode: modeReview, view: &daemon.ViewWire{Posted: true, Finished: false}}

	armed, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	am := armed.(model)

	if !am.confirmingQuit {
		t.Fatal("q on an unfinished review should arm the quit heads-up, not quit outright")
	}
	if isQuit(cmd) {
		t.Error("the first q must not quit")
	}

	_, cmd = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})

	if !isQuit(cmd) {
		t.Error("a second q should quit")
	}
}

func TestQuitGuardHandOffTakesTheBetterPath(t *testing.T) {
	m := model{mode: modeReview, confirmingQuit: true, view: &daemon.ViewWire{Posted: true}}

	after, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	am := after.(model)

	if am.confirmingQuit {
		t.Error("choosing h should disarm the heads-up")
	}
	if am.mode != modeDone {
		t.Error("h from the heads-up should hand off, not exit")
	}
}

func TestCtrlCQuitsEvenWithTheQuitGuardArmed(t *testing.T) {
	fresh := model{mode: modeReview, view: &daemon.ViewWire{Posted: true, Finished: false}}
	if _, cmd := fresh.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); !isQuit(cmd) {
		t.Error("ctrl+c should quit outright on an unfinished review")
	}

	armed := model{mode: modeReview, confirmingQuit: true, view: &daemon.ViewWire{Posted: true}}
	if _, cmd := armed.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); !isQuit(cmd) {
		t.Error("ctrl+c should quit even while the quit heads-up is armed")
	}
}

func TestFinishingElsewhereClearsTheGuardAndShowsTheFinishedScreen(t *testing.T) {
	// The review finishes via another path (e.g. a second attached TUI) while this
	// one sits armed on a Step: the stale heads-up must clear and the mode advance.
	armed := model{mode: modeReview, confirmingQuit: true, view: &daemon.ViewWire{Posted: true}}

	after, _ := armed.Update(refreshMsg{view: &daemon.ViewWire{Posted: true, Finished: true}})
	am := after.(model)

	if am.confirmingQuit {
		t.Error("a review finishing should clear a stale quit heads-up")
	}
	if am.mode != modeDone {
		t.Error("a review finishing should move a walking reviewer to the finished screen")
	}
}

func TestFinishingElsewhereLeavesTheConclusionScreen(t *testing.T) {
	m := model{mode: modeConclusion, view: &daemon.ViewWire{Posted: true, Position: 2, StepCount: 2}}

	after, _ := m.Update(refreshMsg{view: &daemon.ViewWire{Posted: true, Finished: true, Position: 2, StepCount: 2}})

	if after.(model).mode != modeDone {
		t.Error("a review finishing should move the conclusion screen to the finished screen")
	}
}

func TestConcludingWithoutFinishingReachesTheFinishedScreen(t *testing.T) {
	// An explicit conclude sets Concluded without Finished. A walking reviewer must
	// still be moved to the finished screen (State 3), or they never see it — and
	// the daemon, now no longer Active, could self-exit under them.
	m := model{mode: modeReview, view: &daemon.ViewWire{Posted: true, Finished: false}}

	after, _ := m.Update(refreshMsg{view: &daemon.ViewWire{Posted: true, Finished: false, Concluded: true}})
	am := after.(model)

	if am.mode != modeDone {
		t.Error("a concluded review should move the reviewer to the finished screen even without a finish")
	}
	if am.doneState() != doneComplete {
		t.Error("a concluded review should show the complete face")
	}
}

func TestShouldGuardQuitOnlyWhenPostedAndUnfinished(t *testing.T) {
	cases := []struct {
		name string
		view *daemon.ViewWire
		want bool
	}{
		{"posted and unfinished", &daemon.ViewWire{Posted: true, Finished: false}, true},
		{"posted but finished", &daemon.ViewWire{Posted: true, Finished: true}, false},
		{"posted but concluded outright", &daemon.ViewWire{Posted: true, Finished: false, Concluded: true}, false},
		{"not posted", &daemon.ViewWire{Posted: false}, false},
		{"no view", nil, false},
	}

	for _, c := range cases {
		if got := (model{view: c.view}).shouldGuardQuit(); got != c.want {
			t.Errorf("%s: shouldGuardQuit = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestQuitGuardMessageNamesPendingComments(t *testing.T) {
	many := model{view: &daemon.ViewWire{Comments: []daemon.CommentWire{{ID: 1}, {ID: 2}}}}
	if !strings.Contains(many.quitGuardMessage(), "Your 2 Comments will not be lost") {
		t.Errorf("the heads-up should name the pending Comments, got:\n%s", many.quitGuardMessage())
	}

	one := model{view: &daemon.ViewWire{Comments: []daemon.CommentWire{{ID: 1}}}}
	if !strings.Contains(one.quitGuardMessage(), "Your 1 Comment will not be lost") {
		t.Errorf("a single Comment should read in the singular, got:\n%s", one.quitGuardMessage())
	}

	without := model{view: &daemon.ViewWire{}}
	if !strings.Contains(without.quitGuardMessage(), "Nothing will be lost") {
		t.Errorf("with no Comments the heads-up should reassure plainly, got:\n%s", without.quitGuardMessage())
	}
}

func TestConclusionViewShowsTheSummaryAndHandOffCTA(t *testing.T) {
	m := model{view: &daemon.ViewWire{
		StepCount: 3,
		Comments:  []daemon.CommentWire{{ID: 1}, {ID: 2}},
	}}

	out := m.conclusionView()

	if !strings.Contains(out, "2 Comments across 3 Steps") {
		t.Errorf("expected the light summary, got:\n%s", out)
	}
	if !strings.Contains(out, "Press h to hand off") {
		t.Errorf("expected the hand-off call to action, got:\n%s", out)
	}
}

// The #55 vocabulary sweep: the turn-boundary action is "hand off" on h, and
// closing the viewer is "exit" on q. "finish" and "quit" read as synonyms of each
// other, which is what made the end of a round ambiguous, so neither word should
// survive anywhere the Reviewer reads.

func TestTheKeybarNamesHandOffAndExit(t *testing.T) {
	m := model{view: &daemon.ViewWire{Posted: true, StepCount: 3}}

	bar := m.globalKeys()

	if !strings.Contains(bar, "h"+nbsp+"hand"+nbsp+"off") {
		t.Errorf("expected the hand-off key, got:\n%s", bar)
	}
	if !strings.Contains(bar, "q"+nbsp+"exit") {
		t.Errorf("expected exit rather than quit, got:\n%s", bar)
	}
	if strings.Contains(bar, "finish") || strings.Contains(bar, "quit") {
		t.Errorf("neither old term should survive in the keybar, got:\n%s", bar)
	}
}

func TestTheKeybarSaysExitBeforeAnythingIsPosted(t *testing.T) {
	m := model{view: &daemon.ViewWire{Posted: false}}

	if bar := m.globalKeys(); !strings.Contains(bar, "q"+nbsp+"exit") {
		t.Errorf("expected exit rather than quit with nothing posted, got:\n%s", bar)
	}
}

func TestHandOffFromAStepShowsTheHandedOffScreen(t *testing.T) {
	m := model{mode: modeReview, view: &daemon.ViewWire{Posted: true, Position: 1, StepCount: 2}}

	after, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})

	if after.(model).mode != modeDone {
		t.Error("h should hand off and show the handed-off screen")
	}
}

func TestFNoLongerHandsOff(t *testing.T) {
	// f is freed deliberately: "f hand off" renders as "f… off" in the keybar's
	// `key label` format, so the key moved rather than the label being reworded.
	m := model{mode: modeReview, view: &daemon.ViewWire{Posted: true, Position: 1, StepCount: 2}}

	after, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})

	if after.(model).mode == modeDone {
		t.Error("f should no longer hand off")
	}
}

func TestTheHandedOffScreenSaysHandedOff(t *testing.T) {
	m := model{view: &daemon.ViewWire{
		Posted: true, Finished: true,
		Comments: []daemon.CommentWire{{ID: 1}},
	}}

	out := m.doneView()

	if !strings.Contains(out, "Review handed off") {
		t.Errorf("expected the handed-off heading, got:\n%s", out)
	}
}

func TestTheCompleteScreenOffersExitRatherThanQuit(t *testing.T) {
	m := model{view: &daemon.ViewWire{Posted: true, Finished: true, Concluded: true}}

	out := m.doneView()

	if !strings.Contains(out, "Press q to exit") {
		t.Errorf("expected exit rather than quit, got:\n%s", out)
	}
}

func TestQuitGuardMessageOffersAllThreeWaysOut(t *testing.T) {
	m := model{view: &daemon.ViewWire{Posted: true}}

	msg := m.quitGuardMessage()

	for _, want := range []string{"Confirm exit?", "Press <esc> to go back", "h to hand off the review", "q again to exit anyway"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the heads-up should offer %q, got:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "finish") {
		t.Errorf("the heads-up should not say finish, got:\n%s", msg)
	}
}

func TestQuitGuardEscGoesBack(t *testing.T) {
	m := model{mode: modeReview, confirmingQuit: true, view: &daemon.ViewWire{Posted: true}}

	after, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	am := after.(model)

	if am.confirmingQuit {
		t.Error("esc should dismiss the heads-up")
	}
	if isQuit(cmd) {
		t.Error("esc should go back, not exit")
	}
	if am.mode != modeReview {
		t.Error("esc should leave the reviewer where they were")
	}
}

func TestDoneStatePrecedence(t *testing.T) {
	cases := []struct {
		name string
		view *daemon.ViewWire
		want doneState
	}{
		{"concluded wins even over finished", &daemon.ViewWire{Finished: true, Concluded: true}, doneComplete},
		{"unfinished means a revision arrived", &daemon.ViewWire{Finished: false}, doneRevision},
		{"finished with work outstanding is waiting", &daemon.ViewWire{Finished: true}, doneWaiting},
	}

	for _, c := range cases {
		if got := (model{view: c.view}).doneState(); got != c.want {
			t.Errorf("%s: doneState = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestDoneViewRevisionBoxSummarizesDispositions(t *testing.T) {
	m := model{view: &daemon.ViewWire{
		Finished: false,
		Dispositions: []daemon.DispositionWire{
			{CommentID: 1, Status: "addressed"},
			{CommentID: 2, Status: "addressed"},
			{CommentID: 3, Status: "declined"},
		},
	}}

	out := m.doneView()

	if !strings.Contains(out, "Revision Round ready") {
		t.Errorf("expected the revision announcement, got:\n%s", out)
	}
	if !strings.Contains(out, "addressed 2 and declined 1") {
		t.Errorf("expected the disposition summary, got:\n%s", out)
	}
	if !strings.Contains(out, "Press enter to review it") {
		t.Errorf("expected the review call to action, got:\n%s", out)
	}
}

func TestDoneViewRevisionBoxCountsAnsweredComments(t *testing.T) {
	m := model{view: &daemon.ViewWire{
		Dispositions: []daemon.DispositionWire{
			{CommentID: 1, Status: "addressed"},
			{CommentID: 2, Status: "answered", Response: "it guards the retry"},
			{CommentID: 3, Status: "declined", Response: "intended"},
		},
	}}

	out := m.doneView()

	if !strings.Contains(out, "The agent addressed 1, answered 1 and declined 1 of your Comments.") {
		t.Errorf("expected the summary to count the answered Comment, got:\n%s", out)
	}
}

func TestOverviewShowsAnsweredCommentsWithTheirResponse(t *testing.T) {
	m := model{
		width:    80,
		viewport: viewport.New(80, 40),
		view: &daemon.ViewWire{
			Posted: true,
			Brief:  daemon.BriefWire{Ask: "ask", Approach: "approach", ProvenanceKind: "stated", ProvenanceCitation: "chat"},
			Dispositions: []daemon.DispositionWire{
				{CommentID: 1, Status: "addressed", Note: "cap the retry", Response: "capped at 3"},
				{CommentID: 2, Status: "answered", Note: "why this timeout?", Response: "the upstream SLA is 5s"},
			},
			Repositories: []daemon.RepositoryWire{{Root: "repo", Range: "main"}},
			StepNames:    []string{"one"},
			Seen:         []bool{false},
		},
	}

	out := m.brief()

	if !strings.Contains(out, "answered") || strings.Count(out, "addressed") != 1 {
		t.Errorf("expected one addressed and one answered item, got:\n%s", out)
	}
	for _, response := range []string{"the upstream SLA is 5s", "capped at 3"} {
		if !strings.Contains(out, response) {
			t.Errorf("expected the agent's response %q, got:\n%s", response, out)
		}
	}
}

func TestDoneViewRevisionBoxOmitsSummaryWhenNoDispositions(t *testing.T) {
	m := model{view: &daemon.ViewWire{Finished: false}}

	out := m.doneView()

	if !strings.Contains(out, "Revision Round ready") {
		t.Errorf("expected the revision announcement, got:\n%s", out)
	}
	if strings.Contains(out, "addressed") {
		t.Errorf("with no dispositions the summary line should be omitted, got:\n%s", out)
	}
}

func TestDoneViewCompleteState(t *testing.T) {
	m := model{view: &daemon.ViewWire{Finished: true, Concluded: true}}

	out := m.doneView()

	if !strings.Contains(out, "Review complete") {
		t.Errorf("expected the complete face, got:\n%s", out)
	}
	if !strings.Contains(out, "raised nothing") {
		t.Errorf("expected the complete explanation, got:\n%s", out)
	}
}

func TestFinishedScreenEnterReviewsTheArrivedRevision(t *testing.T) {
	// A Revision Round arrived (Finished flipped back off) while on the finished
	// screen: enter — and r, for muscle memory — proceed into the new round.
	for _, key := range []string{"enter", "r"} {
		m := model{mode: modeDone, view: &daemon.ViewWire{Posted: true, Finished: false}}

		after, _ := m.updateDone(key)

		if after.(model).mode != modeReview {
			t.Errorf("%q on the revision face should proceed into the new round", key)
		}
	}
}

func TestFinishedScreenResumeFromTheWaitingFace(t *testing.T) {
	m := model{mode: modeDone, view: &daemon.ViewWire{Posted: true, Finished: true}}

	after, _ := m.updateDone("r")
	am := after.(model)

	if am.mode != modeReview {
		t.Error("r on the waiting face should resume the round for more editing")
	}
	if !strings.Contains(am.status, "resumed") {
		t.Errorf("resuming should say so, got %q", am.status)
	}
}

func TestListViewWrapsLongNotes(t *testing.T) {
	// A long Comment note used to print raw, running off the right edge.
	const width = 50
	m := model{
		width: width,
		view: &daemon.ViewWire{
			Comments: []daemon.CommentWire{{
				ID: 1, Step: 1, Location: "a.go:1-2 (new)", Anchor: "+ 1 | x",
				Note: "This name reads as a boolean but returns the count; rename it so a caller is not misled into an if-check that is always true.",
			}},
		},
	}

	out := m.listView()

	if over := widestLine(out); over > width {
		t.Errorf("a Comment note is %d cells wide, over the %d list — it did not wrap:\n%s", over, width, out)
	}
}

// CL-1: the edit screen returns to wherever it was opened from.

// acceptingServer answers every request with 200, so a save in the editor
// reports success and sets the status message the Step path is meant to keep.
func acceptingServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(server.Close)
	return server
}

// listEditModel is the editor as the List opens it: an existing Comment loaded,
// with the List recorded as where to go back to.
func listEditModel(t *testing.T, comments ...daemon.CommentWire) model {
	t.Helper()
	m := model{
		client:     client{base: acceptingServer(t).URL},
		mode:       modeNote,
		noteReturn: modeList,
		editingID:  comments[0].ID,
		note:       newNote(80),
		view:       &daemon.ViewWire{Posted: true, Position: 1, StepCount: 1, Comments: comments},
	}
	m.note.SetValue(comments[0].Note)
	return m
}

func TestSavingAnEditOpenedFromTheListReturnsToTheList(t *testing.T) {
	m := listEditModel(t, daemon.CommentWire{ID: 3, Step: 1, Note: "n"})
	m.note.SetValue("edited")

	saved, _ := m.updateNote(tea.KeyMsg{Type: tea.KeyEnter})
	sm := saved.(model)

	if sm.mode != modeList {
		t.Error("saving an edit opened from the List should return to the List, not the Step")
	}
	if sm.status != "" {
		t.Errorf("the List shows no status row, so the List path must set none, got %q", sm.status)
	}
}

func TestCancellingAnEditOpenedFromTheListReturnsToTheList(t *testing.T) {
	m := listEditModel(t, daemon.CommentWire{ID: 3, Step: 1, Note: "n"})

	cancelled, _ := m.updateNote(tea.KeyMsg{Type: tea.KeyEsc})

	if cancelled.(model).mode != modeList {
		t.Error("cancelling an edit opened from the List should return to the List, not the Step")
	}
}

func TestDeletingAnEditOpenedFromTheListReturnsToTheList(t *testing.T) {
	m := listEditModel(t, daemon.CommentWire{ID: 3, Step: 1, Note: "n"})
	m.confirmingDelete = true

	deleted, _ := m.updateNote(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	dm := deleted.(model)

	if dm.mode != modeList {
		t.Error("deleting an edit opened from the List should return to the List, not the Step")
	}
	if dm.status != "" {
		t.Errorf("the List shows no status row, so the List path must set none, got %q", dm.status)
	}
}

func TestOpeningAnEditFromTheFilteredListKeepsTheFilterAcrossTheRoundTrip(t *testing.T) {
	filter := commentFilter{active: true, file: "a.go", side: "after", line: 12}
	m := model{
		client:        client{base: acceptingServer(t).URL},
		mode:          modeList,
		commentFilter: filter,
		note:          newNote(80),
		view: &daemon.ViewWire{Posted: true, Position: 1, StepCount: 1, Comments: []daemon.CommentWire{{
			ID: 3, Step: 1, Note: "n", Anchor: "code", Location: "a.go:12",
			File:     "a.go",
			Segments: []daemon.SegmentWire{{Side: "after", FirstLine: 12, LastLine: 12}},
		}}},
	}

	editing, _ := m.updateList("e")
	saved, _ := editing.(model).updateNote(tea.KeyMsg{Type: tea.KeyEnter})
	sm := saved.(model)

	if sm.mode != modeList {
		t.Fatal("saving should return to the List")
	}
	if sm.commentFilter != filter {
		t.Errorf("the filtered List should still be filtered to the same line, got %+v", sm.commentFilter)
	}
}

func TestAnEditOpenedFromAStepStillReturnsToTheStep(t *testing.T) {
	base := func() model {
		m := model{
			client:    client{base: acceptingServer(t).URL},
			mode:      modeNote,
			editingID: 7,
			note:      newNote(80),
			view:      &daemon.ViewWire{Posted: true, Position: 1, StepCount: 1},
		}
		m.note.SetValue("n")
		return m
	}

	saved, _ := base().updateNote(tea.KeyMsg{Type: tea.KeyEnter})
	if sm := saved.(model); sm.mode != modeReview {
		t.Error("saving an edit opened from a Step should still return to the Step")
	} else if sm.status != "Comment updated" {
		t.Errorf("the Step path keeps its status message, got %q", sm.status)
	}

	cancelled, _ := base().updateNote(tea.KeyMsg{Type: tea.KeyEsc})
	if cancelled.(model).mode != modeReview {
		t.Error("cancelling an edit opened from a Step should still return to the Step")
	}

	armed := base()
	armed.confirmingDelete = true
	deleted, _ := armed.updateNote(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if dm := deleted.(model); dm.mode != modeReview {
		t.Error("deleting an edit opened from a Step should still return to the Step")
	} else if dm.status != "Comment deleted" {
		t.Errorf("the Step path keeps its status message, got %q", dm.status)
	}
}

func TestDeletingAnEditOpenedFromTheListClampsTheListCursor(t *testing.T) {
	m := listEditModel(t,
		daemon.CommentWire{ID: 1, Step: 1, Note: "one"},
		daemon.CommentWire{ID: 2, Step: 1, Note: "two"},
		daemon.CommentWire{ID: 3, Step: 1, Note: "three"},
	)
	m.editingID = 3
	m.commentCursor = 2
	m.confirmingDelete = true

	deleted, _ := m.updateNote(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})

	if got := deleted.(model).commentCursor; got != 1 {
		t.Errorf("deleting the last entry should leave the cursor on the new last entry, got %d", got)
	}
}

// CL-2: the Comment list opens from the conclusion screen and returns to it.

// concludingModel is a review sitting on the conclusion screen, rendered wide
// enough that the footer does not wrap.
func concludingModel(comments ...daemon.CommentWire) model {
	return model{
		mode: modeConclusion,
		view: &daemon.ViewWire{
			Posted: true, Position: 2, StepCount: 2,
			Comments: comments,
		},
		width: 100, height: 30, ready: true,
	}
}

func TestListOpensFromTheConclusionScreenUnfiltered(t *testing.T) {
	for _, key := range []string{"l", "L"} {
		m := concludingModel(daemon.CommentWire{ID: 1, Step: 1, Note: "n"})
		m.commentFilter = commentFilter{active: true, file: "a.go", side: "after", line: 12}
		m.commentCursor = 4

		opened, _ := m.updateConclusion(key)
		om := opened.(model)

		if om.mode != modeList {
			t.Errorf("%q on the conclusion screen should open the Comment list", key)
		}
		if om.commentFilter.active {
			t.Errorf("%q should open the full list, not a filtered one", key)
		}
		if om.commentCursor != 0 {
			t.Errorf("%q should open the list at the first entry, got %d", key, om.commentCursor)
		}
	}
}

func TestListOpensFromTheConclusionScreenWithNoComments(t *testing.T) {
	m := concludingModel()

	opened, _ := m.updateConclusion("l")
	om := opened.(model)

	if om.mode != modeList {
		t.Fatal("l should open the list even with no Comments to show")
	}
	if !strings.Contains(om.View(), "No Comments to show.") {
		t.Errorf("the empty list should show its empty state, got:\n%s", om.View())
	}
}

func TestLeavingAListOpenedFromTheConclusionScreenReturnsToIt(t *testing.T) {
	for _, key := range []string{"esc", "L", "q"} {
		m := concludingModel(daemon.CommentWire{ID: 1, Step: 1, Note: "n"})
		opened, _ := m.updateConclusion("l")

		left, _ := opened.(model).updateList(key)
		lm := left.(model)

		if lm.mode != modeConclusion {
			t.Errorf("%q should leave the list back to the conclusion screen, not a Step", key)
		}
		if lm.confirmingQuit {
			t.Errorf("%q in the list closes the list, so it should not arm the quit heads-up", key)
		}
	}
}

func TestLeavingAListOpenedFromAStepStillReturnsToTheStep(t *testing.T) {
	m := model{
		mode: modeList,
		view: &daemon.ViewWire{Posted: true, Position: 1, StepCount: 2, Comments: []daemon.CommentWire{{ID: 1, Step: 1, Note: "n"}}},
	}

	left, _ := m.updateList("esc")

	if left.(model).mode != modeReview {
		t.Error("a list opened from a Step should still leave to the Step")
	}
}

func TestEditingFromTheConclusionScreensListReturnsToTheListThenTheConclusionScreen(t *testing.T) {
	m := concludingModel(daemon.CommentWire{ID: 1, Step: 1, Note: "n", Anchor: "code"})
	m.client = client{base: acceptingServer(t).URL}
	m.note = newNote(80)

	opened, _ := m.updateConclusion("l")
	editing, _ := opened.(model).updateList("e")
	saved, _ := editing.(model).updateNote(tea.KeyMsg{Type: tea.KeyEnter})
	sm := saved.(model)

	if sm.mode != modeList {
		t.Fatal("saving should return to the conclusion screen's list")
	}

	left, _ := sm.updateList("esc")

	if left.(model).mode != modeConclusion {
		t.Error("leaving that list should return to the conclusion screen, never a Step")
	}
}

func TestConclusionFooterOffersTheListAndHandOff(t *testing.T) {
	m := concludingModel(daemon.CommentWire{ID: 1, Step: 1, Note: "n"})

	out := m.View()

	want := keybar("← back", "g Overview", "l list", "h hand off", "q exit")
	if !strings.Contains(out, want) {
		t.Errorf("expected the conclusion footer %q, got:\n%s", want, out)
	}
}

// CL-3: the conclusion screen invites the Reviewer to look over what they raised.

func TestConclusionViewPromptsForTheListBetweenTheSummaryAndTheHandOff(t *testing.T) {
	m := model{view: &daemon.ViewWire{
		StepCount: 3,
		Comments:  []daemon.CommentWire{{ID: 1}, {ID: 2}},
	}}

	out := m.conclusionView()

	prompt := strings.Index(out, "Press l to see your Comments.")
	if prompt < 0 {
		t.Fatalf("expected the plural prompt, got:\n%s", out)
	}
	if summary := strings.Index(out, "You raised"); prompt < summary {
		t.Errorf("the prompt belongs below the summary, got:\n%s", out)
	}
	if handOff := strings.Index(out, "Press h to hand off"); prompt > handOff {
		t.Errorf("the prompt belongs above the hand-off line, got:\n%s", out)
	}
	if !strings.Contains(out, "\n\nPress l to see your Comments.\n\n") {
		t.Errorf("the prompt should have a blank line above and below it, got:\n%s", out)
	}
}

func TestConclusionViewPromptsInTheSingularForOneComment(t *testing.T) {
	m := model{view: &daemon.ViewWire{
		StepCount: 3,
		Comments:  []daemon.CommentWire{{ID: 1}},
	}}

	out := m.conclusionView()

	if !strings.Contains(out, "Press l to see your Comment.") {
		t.Errorf("one Comment should read in the singular, got:\n%s", out)
	}
}

func TestConclusionViewOmitsThePromptWithNoComments(t *testing.T) {
	m := model{view: &daemon.ViewWire{StepCount: 3}}

	out := m.conclusionView()

	if strings.Contains(out, "Press l") {
		t.Errorf("with nothing raised there is nothing to look over, got:\n%s", out)
	}
	if !strings.Contains(out, "Press h to hand off") {
		t.Errorf("the hand-off line stays whatever the count, got:\n%s", out)
	}
}

func TestConclusionViewDropsThePromptWhenTheLastCommentIsWithdrawn(t *testing.T) {
	m := model{view: &daemon.ViewWire{
		StepCount: 3,
		Comments:  []daemon.CommentWire{{ID: 1}},
	}}
	if !strings.Contains(m.conclusionView(), "Press l") {
		t.Fatal("expected the prompt while a Comment stands")
	}

	// The refreshed view the daemon sends back after the withdrawal.
	m.view = &daemon.ViewWire{StepCount: 3}

	if strings.Contains(m.conclusionView(), "Press l") {
		t.Errorf("withdrawing the last Comment should take the prompt with it, got:\n%s", m.conclusionView())
	}
}

func TestTheHeaderStaysEndOfReviewOnScreensOpenedFromTheConclusionScreen(t *testing.T) {
	m := concludingModel(daemon.CommentWire{ID: 1, Step: 1, Note: "n", Anchor: "code"})
	m.note = newNote(80)

	opened, _ := m.updateConclusion("l")
	om := opened.(model)

	if !strings.Contains(om.headerLine(), "End of review") {
		t.Errorf("the list opened from the conclusion screen is not a Step, got:\n%s", om.headerLine())
	}

	editing, _ := om.updateList("e")

	if got := editing.(model).headerLine(); !strings.Contains(got, "End of review") {
		t.Errorf("editing from that list is not a Step either, got:\n%s", got)
	}
}

func TestTheHeaderStillNamesTheStepForAListOpenedFromOne(t *testing.T) {
	m := model{
		view:  &daemon.ViewWire{Posted: true, Position: 2, StepCount: 7, Comments: []daemon.CommentWire{{ID: 1, Step: 2, Note: "n"}}},
		width: 100, height: 30, ready: true,
	}
	m.openList(modeReview)

	if !strings.Contains(m.headerLine(), "Step 2 of 7") {
		t.Errorf("a list opened from a Step belongs to that Step, got:\n%s", m.headerLine())
	}
}

// Folded into the epic at the final pause: two screens that still named a Step.

func TestTheHeaderDoesNotNameAStepOnTheHandedOffScreen(t *testing.T) {
	cases := []struct {
		name string
		view *daemon.ViewWire
		want string
	}{
		{"handed off, waiting", &daemon.ViewWire{Posted: true, Position: 7, StepCount: 7, Finished: true}, "End of review"},
		{"complete", &daemon.ViewWire{Posted: true, Position: 7, StepCount: 7, Finished: true, Concluded: true}, "End of review"},
		{"revision round ready", &daemon.ViewWire{Posted: true, Position: 7, StepCount: 7}, "Revision Round"},
	}

	for _, c := range cases {
		m := model{mode: modeDone, view: c.view, width: 100, height: 30, ready: true}

		got := m.headerLine()

		if strings.Contains(got, "Step 7 of 7") {
			t.Errorf("%s: the handed-off screen is not a Step, got:\n%s", c.name, got)
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: expected %q in the header, got:\n%s", c.name, c.want, got)
		}
	}
}

func TestOpeningTheCommentListClearsTheStatusMessage(t *testing.T) {
	m := model{
		mode:   modeReview,
		status: "Comment updated",
		view:   &daemon.ViewWire{Posted: true, Position: 1, StepCount: 2},
	}

	m.openList(modeReview)

	if m.status != "" {
		t.Errorf("the list has no status row, so a message must not survive the round trip, got %q", m.status)
	}
}

// TestKeybarLeavesItsCallersSliceAlone guards the trap a caller falls into when
// it spreads its own token list: keybar joins labels for display, and must not
// edit the slice it was handed.
func TestKeybarLeavesItsCallersSliceAlone(t *testing.T) {
	tokens := []string{"↑/↓ move", "y copy"}

	keybar(tokens...)

	if tokens[0] != "↑/↓ move" || tokens[1] != "y copy" {
		t.Errorf("keybar rewrote its caller's slice, got %q", tokens)
	}
}

func TestKeybarMakesTheSpacesInsideALabelNonBreaking(t *testing.T) {
	got := keybar("y copy", "q exit")

	want := "y" + nbsp + "copy" + "  ·  " + "q" + nbsp + "exit"
	if got != want {
		t.Errorf("keybar = %q, want %q", got, want)
	}
}

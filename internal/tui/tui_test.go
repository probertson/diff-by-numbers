package tui

import (
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
	// The anchor's "Re: …" header is one long line; in the New Change Request modal
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

func TestListViewTitlePluralizesChangeRequests(t *testing.T) {
	m := model{
		width: 80,
		view:  &daemon.ViewWire{ChangeRequests: []daemon.ChangeRequestWire{{ID: 1, Step: 1, Note: "n"}}},
	}

	out := m.listView()

	if !strings.Contains(out, "1 Change Request") || strings.Contains(out, "Request(s)") {
		t.Errorf("one Change Request should read '1 Change Request', got:\n%s", out)
	}
}

func TestDoneViewPluralizesItsCounts(t *testing.T) {
	// Finished with a Change Request outstanding is the waiting face (State 1).
	m := model{
		view: &daemon.ViewWire{
			Finished:       true,
			StepStatuses:   []string{"seen"},
			ChangeRequests: []daemon.ChangeRequestWire{{ID: 1}},
		},
	}

	out := m.doneView()

	if strings.Contains(out, "(s)") {
		t.Errorf("the finish summary should not use the lazy (s) form, got:\n%s", out)
	}
	if !strings.Contains(out, "1 Step seen") {
		t.Errorf("expected '1 Step seen', got:\n%s", out)
	}
	if !strings.Contains(out, "1 Change Request raised") {
		t.Errorf("expected '1 Change Request raised', got:\n%s", out)
	}
}

func TestCtrlDArmsDeleteOnlyWhenEditingAnExistingRequest(t *testing.T) {
	editing := model{mode: modeNote, editingID: 7, note: newNote(80)}

	armed, _ := editing.updateNote(tea.KeyMsg{Type: tea.KeyCtrlD})

	if !armed.(model).confirmingDelete {
		t.Error("ctrl+d while editing an existing Change Request should arm the delete confirm")
	}

	composing := model{mode: modeNote, editingID: 0, note: newNote(80)}

	still, _ := composing.updateNote(tea.KeyMsg{Type: tea.KeyCtrlD})

	if still.(model).confirmingDelete {
		t.Error("ctrl+d while composing a new Change Request has nothing to delete and must not arm")
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
		t.Error("editingID should clear after the Change Request is deleted")
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
		view: &daemon.ViewWire{ChangeRequests: []daemon.ChangeRequestWire{{ID: 3, Step: 1, Note: "n"}}},
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
		view:             &daemon.ViewWire{ChangeRequests: []daemon.ChangeRequestWire{{ID: 3, Step: 1, Note: "n"}}},
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
		crCursor:         0,
		view:             &daemon.ViewWire{ChangeRequests: []daemon.ChangeRequestWire{{ID: 3, Step: 1, Note: "n"}}},
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
		crCursor:         0,
		view:             &daemon.ViewWire{ChangeRequests: []daemon.ChangeRequestWire{{ID: 3, Step: 1, Note: "n"}}},
	}

	after, _ := m.updateList("e")
	am := after.(model)

	if !am.confirmingDelete {
		t.Error("a stray key while armed should leave the delete armed, not disarm it")
	}
	if am.mode != modeList {
		t.Error("a stray key while armed must not act on the List (e would otherwise open the editor)")
	}
	if am.crCursor != 0 {
		t.Errorf("a stray key while armed must not move the List cursor, got %d", am.crCursor)
	}
}

func TestEditKeybarOffersDeleteOnlyWhenEditing(t *testing.T) {
	editing := model{editingID: 7}
	if !strings.Contains(editing.noteKeys(), "ctrl+d") {
		t.Error("editing an existing Change Request should advertise ctrl+d delete")
	}

	composing := model{editingID: 0}
	if strings.Contains(composing.noteKeys(), "ctrl+d") {
		t.Error("composing a new Change Request has nothing to delete, so must not advertise ctrl+d")
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
	if strings.Index(out, "Delete this Change Request") > strings.LastIndex(out, "ctrl+d") {
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

func TestQuitGuardMessageNamesPendingChangeRequests(t *testing.T) {
	many := model{view: &daemon.ViewWire{ChangeRequests: []daemon.ChangeRequestWire{{ID: 1}, {ID: 2}}}}
	if !strings.Contains(many.quitGuardMessage(), "Your 2 Change Requests will not be lost") {
		t.Errorf("the heads-up should name the pending Change Requests, got:\n%s", many.quitGuardMessage())
	}

	one := model{view: &daemon.ViewWire{ChangeRequests: []daemon.ChangeRequestWire{{ID: 1}}}}
	if !strings.Contains(one.quitGuardMessage(), "Your 1 Change Request will not be lost") {
		t.Errorf("a single Change Request should read in the singular, got:\n%s", one.quitGuardMessage())
	}

	without := model{view: &daemon.ViewWire{}}
	if !strings.Contains(without.quitGuardMessage(), "Nothing will be lost") {
		t.Errorf("with no Change Requests the heads-up should reassure plainly, got:\n%s", without.quitGuardMessage())
	}
}

func TestConclusionViewShowsTheSummaryAndHandOffCTA(t *testing.T) {
	m := model{view: &daemon.ViewWire{
		StepCount:      3,
		ChangeRequests: []daemon.ChangeRequestWire{{ID: 1}, {ID: 2}},
	}}

	out := m.conclusionView()

	if !strings.Contains(out, "2 Change Requests across 3 Steps") {
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
		ChangeRequests: []daemon.ChangeRequestWire{{ID: 1}},
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
			{ChangeRequestID: 1, Status: "addressed"},
			{ChangeRequestID: 2, Status: "addressed"},
			{ChangeRequestID: 3, Status: "declined"},
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
	// A long Change Request note used to print raw, running off the right edge.
	const width = 50
	m := model{
		width: width,
		view: &daemon.ViewWire{
			ChangeRequests: []daemon.ChangeRequestWire{{
				ID: 1, Step: 1, Location: "a.go:1-2 (new)", Anchor: "+ 1 | x",
				Note: "This name reads as a boolean but returns the count; rename it so a caller is not misled into an if-check that is always true.",
			}},
		},
	}

	out := m.listView()

	if over := widestLine(out); over > width {
		t.Errorf("a Change Request note is %d cells wide, over the %d list — it did not wrap:\n%s", over, width, out)
	}
}

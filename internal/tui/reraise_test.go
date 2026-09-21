package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// reraiseRecorder answers /reraise and keeps what it was asked to re-raise.
type reraiseRecorder struct {
	path string
	note string
	hits int
}

func reraiseServer(t *testing.T) (*reraiseRecorder, string) {
	t.Helper()
	rec := &reraiseRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/reraise/") {
			return
		}
		var body struct {
			Note string `json:"note"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		rec.path, rec.note, rec.hits = r.URL.Path, body.Note, rec.hits+1
	}))
	t.Cleanup(server.Close)
	return rec, server.URL
}

// pushbackModel is a Revision Round whose Overview offers the given
// dispositions, with the Comments this round already holds.
func pushbackModel(t *testing.T, dispositions []daemon.DispositionWire, comments ...daemon.CommentWire) model {
	t.Helper()
	_, base := reraiseServer(t)
	return model{
		client:   client{base: base},
		mode:     modeReview,
		width:    80,
		height:   30,
		ready:    true,
		viewport: viewport.New(80, 30),
		note:     newNote(80),
		view: &daemon.ViewWire{
			Posted: true, Position: 0, StepCount: 1,
			Brief: daemon.BriefWire{Ask: "a", Approach: "b",
				ProvenanceKind: "stated", ProvenanceCitation: "c"},
			Dispositions: dispositions,
			Comments:     comments,
			Repositories: []daemon.RepositoryWire{{Root: "repo", Base: "main"}},
			StepNames:    []string{"one"},
			Seen:         []bool{false},
		},
	}
}

func unresolved(id int, status string) daemon.DispositionWire {
	return daemon.DispositionWire{
		CommentID: id, Status: status, Location: "a.go — after 25",
		Note: "the original note", Response: "the agent's reasoning",
		Anchor: "Re: a.go:25 (new side) — Step \"s\" in repo\n\n+    25 | code\n",
	}
}

func TestThePickerOffersAnsweredCommentsAsWellAsDeclinedOnes(t *testing.T) {
	m := pushbackModel(t, []daemon.DispositionWire{
		unresolved(1, "declined"), unresolved(2, "answered"), unresolved(3, "addressed"),
	})

	out := m.reraiseView()

	if !strings.Contains(out, "#1") || !strings.Contains(out, "#2") {
		t.Errorf("a question the agent only answered is re-raisable too, got:\n%s", out)
	}
	if strings.Contains(out, "#3") {
		t.Errorf("an addressed Comment has new code to comment on instead, got:\n%s", out)
	}
	if !strings.Contains(out, "Re-raise a declined or answered Comment") {
		t.Errorf("the title should say what it offers, got:\n%s", out)
	}
}

func TestThePickerHidesAResolutionAlreadyReRaised(t *testing.T) {
	m := pushbackModel(t,
		[]daemon.DispositionWire{unresolved(1, "declined"), unresolved(2, "declined")},
		daemon.CommentWire{ID: 12, Step: 0, ReRaisedFrom: 1, Note: "n"})

	out := m.reraiseView()

	if strings.Contains(out, "#1 ") {
		t.Errorf("a decline already re-raised must not be offered again, got:\n%s", out)
	}
	if !strings.Contains(out, "#2") {
		t.Errorf("the one still standing should still be offered, got:\n%s", out)
	}
}

func TestRSaysSoWhenEveryResolutionIsAlreadyReRaised(t *testing.T) {
	m := pushbackModel(t,
		[]daemon.DispositionWire{unresolved(1, "declined")},
		daemon.CommentWire{ID: 12, Step: 0, ReRaisedFrom: 1, Note: "n"})

	after, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	am := after.(model)

	if am.mode == modeReraise {
		t.Error("with nothing left to offer, R should not open an empty picker")
	}
	if !strings.Contains(am.status, "already re-raised") {
		t.Errorf("expected R to say why nothing happened, got %q", am.status)
	}
}

func TestReRaisingOpensTheEditorOverTheOriginal(t *testing.T) {
	m := pushbackModel(t, []daemon.DispositionWire{unresolved(1, "declined")})
	m.mode = modeReraise

	opened, _ := m.updateReraise("enter")
	om := opened.(model)

	if om.mode != modeNote {
		t.Fatalf("re-raising should open the note editor, got mode %v", om.mode)
	}
	if om.note.Value() != "the original note" {
		t.Errorf("the editor should be pre-filled with what was said before, got %q", om.note.Value())
	}
	if !strings.Contains(om.pendingCode, "code") {
		t.Errorf("the editor should show the code the point was about, got %q", om.pendingCode)
	}
	if om.editingID != 0 {
		t.Error("a re-raise composes a new Comment; it is not an edit of an old one")
	}
}

func TestEscapingTheEditorAbandonsTheReRaise(t *testing.T) {
	rec, base := reraiseServer(t)
	m := pushbackModel(t, []daemon.DispositionWire{unresolved(1, "declined")})
	m.client = client{base: base}
	m.mode = modeReraise
	opened, _ := m.updateReraise("enter")

	left, _ := opened.(model).updateNote(tea.KeyMsg{Type: tea.KeyEsc})

	if rec.hits != 0 {
		t.Error("esc must not send the re-raise")
	}
	if left.(model).reraisingID != 0 {
		t.Error("leaving the editor should forget the re-raise it was composing")
	}
}

func TestSavingTheEditorSendsTheReRaiseWithWhatWasWritten(t *testing.T) {
	rec, base := reraiseServer(t)
	m := pushbackModel(t, []daemon.DispositionWire{unresolved(1, "declined")})
	m.client = client{base: base}
	m.mode = modeReraise
	opened, _ := m.updateReraise("enter")
	om := opened.(model)
	om.note.SetValue("I still disagree, and here is why")

	saved, _ := om.updateNote(tea.KeyMsg{Type: tea.KeyEnter})

	if rec.hits != 1 || rec.path != "/reraise/1" {
		t.Fatalf("expected one re-raise of Comment 1, got %d at %q", rec.hits, rec.path)
	}
	if rec.note != "I still disagree, and here is why" {
		t.Errorf("the Reviewer's follow-up should travel with it, got %q", rec.note)
	}
	if saved.(model).reraisingID != 0 {
		t.Error("the composed re-raise should be cleared once sent")
	}
}

func TestTheOverviewMarksAResolutionThatHasBeenReRaised(t *testing.T) {
	m := pushbackModel(t,
		[]daemon.DispositionWire{unresolved(1, "declined"), unresolved(2, "declined")},
		daemon.CommentWire{ID: 12, Step: 0, ReRaisedFrom: 1, Note: "n"})

	out := m.brief()

	if !strings.Contains(out, "↻ re-raised as #12") {
		t.Errorf("a re-raised decline should say where it went, got:\n%s", out)
	}
	if !strings.Contains(out, "Declined (2, 1 re-raised)") {
		t.Errorf("the heading should count what is still standing, got:\n%s", out)
	}
	if !strings.Contains(out, "press R to re-raise one") {
		t.Errorf("one decline is still open, so the hint stays, got:\n%s", out)
	}
}

func TestTheOverviewDropsTheHintOnceEverythingIsReRaised(t *testing.T) {
	m := pushbackModel(t,
		[]daemon.DispositionWire{unresolved(1, "declined")},
		daemon.CommentWire{ID: 12, Step: 0, ReRaisedFrom: 1, Note: "n"})

	out := m.brief()

	if strings.Contains(out, "press R") {
		t.Errorf("with nothing left to re-raise the invitation goes, got:\n%s", out)
	}
	if !strings.Contains(out, "Declined (1, 1 re-raised)") {
		t.Errorf("the heading still says what happened, got:\n%s", out)
	}
}

func TestRWorksFromTheConclusionScreenAndComesBackToIt(t *testing.T) {
	// The conclusion screen carries the count of what is still standing, so the
	// key that hint names has to work there — and land back there afterwards.
	m := pushbackModel(t, []daemon.DispositionWire{unresolved(1, "declined")})
	m.mode = modeConclusion

	opened, _ := m.updateConclusion("R")
	om := opened.(model)
	if om.mode != modeReraise {
		t.Fatalf("R on the conclusion screen should open the picker, got mode %v", om.mode)
	}

	left, _ := om.updateReraise("esc")
	if left.(model).mode != modeConclusion {
		t.Error("leaving the picker should return to the screen it was opened from")
	}

	composing, _ := om.updateReraise("enter")
	abandoned, _ := composing.(model).updateNote(tea.KeyMsg{Type: tea.KeyEsc})
	if abandoned.(model).mode != modeConclusion {
		t.Error("abandoning the editor should return there too, not to a Step")
	}
}

func TestTheConclusionScreenCountsOnlyWhatIsStillStanding(t *testing.T) {
	some := pushbackModel(t,
		[]daemon.DispositionWire{unresolved(1, "declined"), unresolved(2, "declined")},
		daemon.CommentWire{ID: 12, Step: 0, ReRaisedFrom: 1, Note: "n"})
	all := pushbackModel(t,
		[]daemon.DispositionWire{unresolved(1, "declined")},
		daemon.CommentWire{ID: 12, Step: 0, ReRaisedFrom: 1, Note: "n"})

	if !strings.Contains(some.conclusionView(), "1 of 2 declines not re-raised — R to re-raise") {
		t.Errorf("expected the standing count, got:\n%s", some.conclusionView())
	}
	if strings.Contains(all.conclusionView(), "re-raise") {
		t.Errorf("with every decline re-raised the hint goes entirely, got:\n%s", all.conclusionView())
	}
}

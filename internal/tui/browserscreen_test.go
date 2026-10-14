package tui

import (
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// reportingModel is review a1 open on a screen, recording each screen it
// reports to dbn as "review:screen".
func reportingModel(on mode) (model, *[]string) {
	m, _ := openingModel(on, stepView())
	reported := &[]string{}
	m.screenReport = func(reviewID, screen string) {
		*reported = append(*reported, reviewID+":"+screen)
	}
	return m, reported
}

func send(m model, msg tea.Msg) model {
	after, _ := m.Update(msg)
	return after.(model)
}

// dbn stays on the last Step while the conclusion screen shows, so the TUI
// tells it when the Reviewer gets there and when they go back.
func TestTheConclusionScreenIsReportedComingAndGoing(t *testing.T) {
	m, reported := reportingModel(modeReview) // on the last Step

	past := send(m, tea.KeyMsg{Type: tea.KeyEnter})

	if want := []string{"a1:conclusion"}; !slices.Equal(*reported, want) {
		t.Fatalf("expected reaching the conclusion screen reported as %v, got %v", want, *reported)
	}

	send(past, tea.KeyMsg{Type: tea.KeyLeft})

	if want := []string{"a1:conclusion", "a1:review"}; !slices.Equal(*reported, want) {
		t.Errorf("expected going back to the last Step reported as %v, got %v", want, *reported)
	}
}

// The Inbox is the window's, so the TUI tells the review it set aside, and
// tells it again when the Reviewer comes back to it.
func TestSettingAReviewAsideAndOpeningItAgainIsReported(t *testing.T) {
	m, reported := reportingModel(modeReview)
	m.inbox = rows()
	m.inboxCursor = "a1"

	home := press(m, "i")

	if want := []string{"a1:inbox"}; !slices.Equal(*reported, want) {
		t.Fatalf("expected setting the review aside reported as %v, got %v", want, *reported)
	}

	pressEnter(home)

	if want := []string{"a1:inbox", "a1:review"}; !slices.Equal(*reported, want) {
		t.Errorf("expected opening it again reported as %v, got %v", want, *reported)
	}
}

func TestMovingBetweenStepsReportsNothing(t *testing.T) {
	m, reported := reportingModel(modeReview)

	press(press(m, "down"), "up")

	if len(*reported) != 0 {
		t.Errorf("expected nothing reported while the Reviewer stays on dbn's stops, got %v", *reported)
	}
}

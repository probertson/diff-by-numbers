package daemon

import (
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
)

func TestThePostedMessageTellsTheAgentHowTheReviewerOpensIt(t *testing.T) {
	message := postedMessage(review.PostedNewReview, DefaultPort)

	want := "Posted. The Reviewer opens it by running dbn in a terminal; if dbn is already open, it appears there. Tell them it's ready, then end your turn."
	if message != want {
		t.Errorf("expected %q, got %q", want, message)
	}
}

func TestThePostedMessageNamesAPortThatIsNotTheDefault(t *testing.T) {
	message := postedMessage(review.PostedNewReview, 7374)

	if !strings.Contains(message, "running dbn -port 7374 in a terminal") {
		t.Errorf("expected the command to carry the port, got %q", message)
	}
}

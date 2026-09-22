package tui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// A Revision Round is shaded by what moved since the previous round unless the
// Reviewer asks for every change under review (#44).

// roundModel is a round-3 Overview compared with round 2, answering its toggle
// on a server that records the paths it was sent.
func comparedRoundModel(t *testing.T, since bool) (model, *[]string) {
	t.Helper()
	var hits []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.Method+" "+r.URL.Path)
	}))
	t.Cleanup(server.Close)
	return model{
		client: client{base: server.URL}, mode: modeReview, width: 100, height: 40, ready: true,
		viewport: viewport.New(100, 40), note: newNote(100),
		view: &daemon.ViewWire{
			Posted: true, Position: 0, StepCount: 2,
			Brief:        daemon.BriefWire{Goal: "a", Approach: "b"},
			Repositories: []daemon.RepositoryWire{{Root: "repo", Base: "feature/a-very-long-base-branch"}},
			StepNames:    []string{"Retry", "Config"},
			Seen:         []bool{false, false},
			Round:        3, PreviousRound: 2, SincePreviousRound: since,
			UnchangedSincePrevious: []bool{false, true},
			Withdrawn: []daemon.WithdrawalWire{
				{Repository: "repo", File: "app.ts", After: 4, PreviousFirst: 5, Lines: []string{"one", "two", "three", "four", "five", "six", "seven"}},
			},
		},
	}, &hits
}

func TestTheHeaderSaysWhatTheCodeIsShadedAgainst(t *testing.T) {
	since, _ := comparedRoundModel(t, true)
	all, _ := comparedRoundModel(t, false)

	if header := since.headerLine(); !strings.Contains(header, "changes since round 2") || strings.Contains(header, "feature/") {
		t.Errorf("expected the since-round-2 label and no ref, got %q", header)
	}
	if header := all.headerLine(); !strings.Contains(header, "all changes under review") {
		t.Errorf("expected the all-changes label, got %q", header)
	}
}

func TestBOffersTheOtherComparisonAndSwitchesToIt(t *testing.T) {
	m, hits := comparedRoundModel(t, true)

	keys := strings.ReplaceAll(m.modeKeys(), nbsp, " ")
	press(m, "b")

	if !strings.Contains(keys, "show all changes under review") {
		t.Errorf("expected b to offer all changes, got %q", keys)
	}
	if len(*hits) != 1 || (*hits)[0] != "POST /since-previous" {
		t.Errorf("expected b to ask the daemon to switch, got %v", *hits)
	}
	all, _ := comparedRoundModel(t, false)
	if keys := strings.ReplaceAll(all.modeKeys(), nbsp, " "); !strings.Contains(keys, "show only changes since round 2") {
		t.Errorf("expected b to offer the since-round view back, got %q", keys)
	}
}

func TestAFirstRoundOffersNoComparison(t *testing.T) {
	m, hits := comparedRoundModel(t, false)
	m.view.Round, m.view.PreviousRound = 1, 0

	keys := strings.ReplaceAll(m.modeKeys(), nbsp, " ")
	press(m, "b")

	if strings.Contains(keys, "changes") || len(*hits) != 0 {
		t.Errorf("a first round has nothing to compare with, got keys %q and requests %v", keys, *hits)
	}
}

func TestTheOverviewListsWhatWasWithdrawnCappedAtFiveLines(t *testing.T) {
	m, _ := comparedRoundModel(t, true)

	out := flatten(m.brief())

	withdrawn := strings.Index(out, "Withdrawn since round 2")
	if withdrawn < 0 {
		t.Fatalf("expected the Withdrawn section, got:\n%s", out)
	}
	section := out[withdrawn:]
	for _, want := range []string{"app.ts", "below line 4", "- five", "… 2 more lines"} {
		if !strings.Contains(section, want) {
			t.Errorf("expected %q in the Withdrawn section:\n%s", want, section)
		}
	}
	if strings.Contains(section, "- six") {
		t.Errorf("expected the block capped at five lines:\n%s", section)
	}
}

func TestAStepUnchangedSinceThePreviousRoundSaysSo(t *testing.T) {
	m, _ := comparedRoundModel(t, true)

	out := flatten(m.brief())

	if !strings.Contains(out, "Config unchanged since round 2") || strings.Contains(out, "Retry unchanged") {
		t.Errorf("expected only Config marked unchanged, got:\n%s", out)
	}
}

func TestAPreviousRoundRowIsDrawnAsARemoval(t *testing.T) {
	step := &daemon.StepWire{
		Number: 1, Name: "Retry", Explanation: "e",
		Excerpts: []daemon.ExcerptWire{{Repository: "/r", File: "app.ts", Side: "new", FirstLine: 3, LastLine: 3,
			Lines: []daemon.LineWire{
				{Number: 3, Side: "previous", Text: "old text", Changed: true},
				{Number: 3, Side: "new", Text: "new text", Changed: true},
			}}},
	}
	cur := newStepCursor(step, nil)

	rows := paneRows(step, cur, nil, nil, 80, 4, false, false)

	var removed, added bool
	for _, r := range rows {
		removed = removed || strings.Contains(r.text, "-     3 │ old text")
		added = added || strings.Contains(r.text, "+     3 │ new text")
	}
	if !removed || !added {
		t.Errorf("expected the previous round's line drawn as a removal above its replacement, got %+v", rows)
	}
}

package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// roundModel is the Overview of a Revision Round: a Brief plus the dispositions
// the agent came back with.
func roundModel(width int, dispositions ...daemon.DispositionWire) model {
	return model{
		width:    width,
		viewport: viewport.New(width, 30),
		view: &daemon.ViewWire{
			Posted: true,
			Brief: daemon.BriefWire{
				Ask: "ask", Approach: "approach",
				ProvenanceKind: "stated", ProvenanceCitation: "the session",
			},
			Dispositions: dispositions,
			Repositories: []daemon.RepositoryWire{{Root: "repo", Range: "main"}},
			StepNames:    []string{"one"},
			Seen:         []bool{false},
		},
	}
}

func declined(id int) daemon.DispositionWire {
	return daemon.DispositionWire{CommentID: id, Status: "declined", Location: "a.go — after 25",
		Note: "you asked this", Response: "the agent said this"}
}

func answered(id int) daemon.DispositionWire {
	return daemon.DispositionWire{CommentID: id, Status: "answered", Location: "b.go — line 3",
		Note: "you wondered this", Response: "the agent answered this"}
}

func addressed(id int) daemon.DispositionWire {
	return daemon.DispositionWire{CommentID: id, Status: "addressed", Location: "c.go — lines 1-2",
		Note: "you asked for this"}
}

func TestTheOverviewGroupsDeclinesThenAnswersThenChanges(t *testing.T) {
	m := roundModel(80, addressed(1), declined(2), answered(3))

	out := m.brief()

	declines := strings.Index(out, "Declined (1)")
	answers := strings.Index(out, "Answered (1)")
	changes := strings.Index(out, "Addressed (1)")
	if declines < 0 || answers < 0 || changes < 0 {
		t.Fatalf("expected all three headings, got:\n%s", out)
	}
	if !(declines < answers && answers < changes) {
		t.Errorf("expected declines, then answers, then changes, got:\n%s", out)
	}
}

func TestTheOverviewKeepsCommentOrderWithinAGroup(t *testing.T) {
	m := roundModel(80, declined(7), declined(2))

	out := m.brief()

	if strings.Index(out, "#7") > strings.Index(out, "#2") {
		t.Errorf("a group keeps the order the Comments came in, got:\n%s", out)
	}
}

func TestTheOverviewLeavesOutAnEmptyGroup(t *testing.T) {
	m := roundModel(80, addressed(1))

	out := m.brief()

	if strings.Contains(out, "Declined") || strings.Contains(out, "Answered") {
		t.Errorf("a group with nothing in it should not be announced, got:\n%s", out)
	}
	if !strings.Contains(out, "Addressed (1)") {
		t.Errorf("expected the one group that has something, got:\n%s", out)
	}
}

func TestTheOverviewDropsTheStatusWordFromAnItem(t *testing.T) {
	m := roundModel(80, declined(3))

	out := m.brief()

	item := lineWith(t, out, "#3")
	if strings.Contains(item, "declined") {
		t.Errorf("the heading carries the status, so the item need not repeat it, got %q", item)
	}
	if !strings.Contains(item, "✗") {
		t.Errorf("the item keeps its mark, got %q", item)
	}
}

func TestTheOverviewSeparatesItemsWithABlankLine(t *testing.T) {
	m := roundModel(80, declined(1), declined(2))

	out := m.brief()

	rows := strings.Split(out, "\n")
	for i, row := range rows {
		if strings.Contains(row, "#2") && strings.Contains(row, "✗") {
			if i == 0 || strings.TrimSpace(rows[i-1]) != "" {
				t.Errorf("expected a blank row above the second item, got:\n%s", out)
			}
			return
		}
	}
	t.Fatalf("never found the second item:\n%s", out)
}

func TestTheOverviewHangsAWrappedAnswerInAFixedBlock(t *testing.T) {
	const width = 50
	long := declined(1)
	long.Response = strings.Repeat("the agent explained itself at length ", 4) + "ENDOFSAID"
	m := roundModel(width, long)

	out := m.brief()

	label := lineWith(t, out, "agent:")
	tail := lineWith(t, out, "ENDOFSAID")
	if got := indentOf(label); got != 6 {
		t.Errorf("a label sits six columns in, got %d in %q", got, label)
	}
	if got := indentOf(tail); got != 10 {
		t.Errorf("a continuation sits four columns further in, got %d in %q", got, tail)
	}
	if w := widestLine(out); w > width {
		t.Errorf("a row is %d cells wide, over the %d viewport:\n%s", w, width, out)
	}
}

func TestTheOverviewStaysInsideANarrowViewport(t *testing.T) {
	// Narrower than the label, and narrower than the indent, are both reachable on
	// a split terminal. Neither may put a row past the edge.
	long := declined(1)
	long.Response = strings.Repeat("the agent explained itself at length ", 3)

	// The disposition block below its section label: every section label in the
	// Brief is a fixed string shown whole, whatever the width.
	for _, width := range []int{8, 10, 14, 20, 30} {
		out := roundModel(width, long).sinceTheLastRound(width)

		for _, row := range strings.Split(out, "\n")[1:] {
			if w := widestLine(row); w > width {
				t.Errorf("width %d: the row %q is %d cells wide", width, row, w)
			}
		}
	}
}

func TestTheOverviewPutsTheReRaiseHintInTheDeclinedHeading(t *testing.T) {
	withDeclines := roundModel(80, declined(1), declined(2)).brief()
	without := roundModel(80, addressed(1)).brief()

	if !strings.Contains(withDeclines, "Declined (2) — press R to re-raise one") {
		t.Errorf("the hint belongs in the heading the Reviewer is already reading, got:\n%s", withDeclines)
	}
	if strings.Contains(withDeclines, "press R to re-raise a declined Comment") {
		t.Errorf("the old dim line below the whole list should be gone, got:\n%s", withDeclines)
	}
	if strings.Contains(without, "press R") {
		t.Errorf("with nothing declined there is nothing to re-raise, got:\n%s", without)
	}
}

func TestTheOverviewLabelsTheAgentDistinctlyFromTheReviewer(t *testing.T) {
	m := roundModel(80, answered(1))

	out := m.brief()

	if !strings.Contains(out, "you asked:") || !strings.Contains(out, "agent:") {
		t.Errorf("expected both labels, got:\n%s", out)
	}
	if lineWith(t, out, "agent:") == lineWith(t, out, "you asked:") {
		t.Error("the two labels belong on rows of their own")
	}
}

func TestTheRevisionRoundBoxSaysDeclinesCanBePushedBack(t *testing.T) {
	with := model{view: &daemon.ViewWire{Posted: true, Dispositions: []daemon.DispositionWire{declined(1)}}}
	without := model{view: &daemon.ViewWire{Posted: true, Dispositions: []daemon.DispositionWire{addressed(1)}}}

	if !strings.Contains(flatten(with.doneView()), "You can re-raise a decline with R.") {
		t.Errorf("the box announcing the round should say you can push back, got:\n%s", with.doneView())
	}
	if strings.Contains(without.doneView(), "re-raise") {
		t.Errorf("with nothing declined the box should not mention it, got:\n%s", without.doneView())
	}
}

func TestTheConclusionScreenSaysWhatWasDeclined(t *testing.T) {
	with := model{view: &daemon.ViewWire{
		StepCount:    3,
		Comments:     []daemon.CommentWire{{ID: 1}},
		Dispositions: []daemon.DispositionWire{declined(1), declined(2), addressed(3)},
	}}
	without := model{view: &daemon.ViewWire{StepCount: 3, Comments: []daemon.CommentWire{{ID: 1}}}}

	out := with.conclusionView()

	if !strings.Contains(out, "The agent declined 2 Comments — R to re-raise") {
		t.Errorf("the last chance to push back belongs on the conclusion screen, got:\n%s", out)
	}
	// The agreed layout: the count, then the declines hint, then the list prompt.
	count := strings.Index(out, "Comment for your agent")
	hint := strings.Index(out, "The agent declined")
	prompt := strings.Index(out, "Press l to see your")
	if !(count < hint && hint < prompt) {
		t.Errorf("the declines hint sits between the count and the list prompt, got:\n%s", out)
	}
	if strings.Contains(without.conclusionView(), "declined") {
		t.Errorf("with nothing declined the hint goes entirely, got:\n%s", without.conclusionView())
	}
}

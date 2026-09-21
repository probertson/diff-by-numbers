package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// tallListModel is a Comment list far taller than the screen it is shown on.
func tallListModel(n int) model {
	comments := make([]daemon.CommentWire, 0, n)
	for i := 1; i <= n; i++ {
		comments = append(comments, daemon.CommentWire{
			ID: i, Step: i, Location: fmt.Sprintf("a.go — line %d", i),
			Anchor: fmt.Sprintf("Re: a.go:%d (new side) — Step \"s\" in repo\n\n+    %2d | code %d\n", i, i, i),
			Note:   fmt.Sprintf("NOTE%d", i),
		})
	}
	return model{
		mode:  modeList,
		view:  &daemon.ViewWire{Posted: true, Comments: comments},
		width: 80, height: 24, ready: true,
	}
}

func TestTheCommentListOpensAtTheTopWithItsTitle(t *testing.T) {
	m := tallListModel(20)

	out := m.listView()

	if !strings.Contains(out, "20 Comments") {
		t.Errorf("the title should stay at the top, got:\n%s", out)
	}
	if !strings.Contains(out, "NOTE1") {
		t.Errorf("the list should open on the first Comment, where the cursor is, got:\n%s", out)
	}
	if h := lipgloss.Height(out); h > m.bodyHeight() {
		t.Errorf("the list is %d rows, over the %d body:\n%s", h, m.bodyHeight(), out)
	}
}

func TestTheCommentListKeepsTheCursorsCommentOnScreen(t *testing.T) {
	m := tallListModel(20)

	for _, cursor := range []int{0, 5, 12, 19} {
		m.commentCursor = cursor

		out := m.listView()

		want := fmt.Sprintf("NOTE%d", cursor+1)
		if !strings.Contains(out, want) {
			t.Errorf("cursor %d: expected %s on screen, got:\n%s", cursor, want, out)
		}
		if h := lipgloss.Height(out); h > m.bodyHeight() {
			t.Errorf("cursor %d: the list is %d rows, over the %d body", cursor, h, m.bodyHeight())
		}
	}
}

func TestTheCommentListMarksWhatIsOffScreen(t *testing.T) {
	m := tallListModel(20)

	atTop := m.listView()
	m.commentCursor = 10
	middle := m.listView()
	m.commentCursor = 19
	atBottom := m.listView()

	if strings.Contains(atTop, "more above") {
		t.Errorf("nothing is above the first Comment, got:\n%s", atTop)
	}
	if !strings.Contains(atTop, "more below") {
		t.Errorf("expected a downward marker at the top of a long list, got:\n%s", atTop)
	}
	if !strings.Contains(middle, "more above") || !strings.Contains(middle, "more below") {
		t.Errorf("expected both markers in the middle of a long list, got:\n%s", middle)
	}
	if strings.Contains(atBottom, "more below") {
		t.Errorf("nothing is below the last Comment, got:\n%s", atBottom)
	}
	if lipgloss.Height(atTop) != lipgloss.Height(middle) {
		t.Errorf("a blank marker row keeps the body height constant: %d then %d", lipgloss.Height(atTop), lipgloss.Height(middle))
	}
}

func TestTheCommentListShortEnoughToFitHasNoMarkers(t *testing.T) {
	m := tallListModel(2)

	out := m.listView()

	if strings.Contains(out, "more above") || strings.Contains(out, "more below") {
		t.Errorf("a list that fits needs no markers, got:\n%s", out)
	}
}

func TestTheCommentListCapsALongAnchorQuote(t *testing.T) {
	var anchor strings.Builder
	anchor.WriteString("Re: a.go:1-9 (new side) — Step \"s\" in repo\n\n")
	for i := 1; i <= 9; i++ {
		fmt.Fprintf(&anchor, "+    %2d | code %d\n", i, i)
	}
	m := tallListModel(1)
	m.view.Comments[0].Anchor = anchor.String()

	// The cap counts source lines and is applied before wrapping, so a narrower
	// terminal — where each source line takes several rows — quotes the same five.
	for _, width := range []int{80, 26} {
		m.width = width

		out := m.listView()

		if !strings.Contains(out, "code 5") {
			t.Errorf("width %d: the first five source lines should be quoted, got:\n%s", width, out)
		}
		if strings.Contains(out, "code 6") {
			t.Errorf("width %d: past the cap the quote stops, got:\n%s", width, out)
		}
		if !strings.Contains(out, "… 4 more lines") {
			t.Errorf("width %d: the cap should say how much it left out, got:\n%s", width, out)
		}
	}
}

func TestTheCommentListClipsAnItemTallerThanTheWindow(t *testing.T) {
	m := tallListModel(3)
	m.height = 14 // a body of a handful of rows
	m.view.Comments[0].Note = strings.Repeat("first ", 200)

	out := m.listView()

	if h := lipgloss.Height(out); h > m.bodyHeight() {
		t.Errorf("an oversized item is clipped, not allowed to grow the body: %d rows over %d:\n%s", h, m.bodyHeight(), out)
	}
	if !strings.Contains(out, "Step 1") {
		t.Errorf("an oversized item is held to its top row, got:\n%s", out)
	}
	if last := lastRow(out); last != "  ↓ 2 more below · this Comment continues — e to read it all" {
		t.Errorf("expected the two other Comments counted and the clipped one announced apart, got %q", last)
	}
}

func lastRow(out string) string {
	rows := strings.Split(out, "\n")
	return rows[len(rows)-1]
}

// The Comment under the cursor is never one of the Comments "below": a single
// Comment too tall for the window said "1 more below" when there was nothing to
// move to, and with a second Comment the count read 2.
func TestAClippedCommentUnderTheCursorIsNotCountedAsMoreBelow(t *testing.T) {
	for _, tc := range []struct {
		comments int
		want     string
	}{
		{1, "  ↓ this Comment continues — e to read it all"},
		{2, "  ↓ 1 more below · this Comment continues — e to read it all"},
	} {
		m := tallListModel(tc.comments)
		m.height = 14
		m.view.Comments[0].Note = strings.Repeat("first ", 200)

		out := m.listView()

		if last := lastRow(out); last != tc.want {
			t.Errorf("with %d Comments: expected %q, got %q", tc.comments, tc.want, last)
		}
	}
}

func TestTheCommentListNeverOutgrowsItsBodyHoweverShortTheTerminal(t *testing.T) {
	// The body floors at four rows, of which the title takes two — too few to
	// spend on markers. The items must win rather than the frame break.
	m := tallListModel(20)

	for height := 1; height <= 30; height++ {
		for _, width := range []int{12, 40, 80} {
			m.height, m.width = height, width

			for _, cursor := range []int{0, 9, 19} {
				m.commentCursor = cursor

				out := m.listView()

				if h := lipgloss.Height(out); h > m.bodyHeight() {
					t.Fatalf("height %d width %d cursor %d: the list is %d rows, over the %d body:\n%s",
						height, width, cursor, h, m.bodyHeight(), out)
				}
			}
		}
	}
}

func TestTheReRaisePickerIsWindowedToo(t *testing.T) {
	dispositions := make([]daemon.DispositionWire, 0, 20)
	for i := 1; i <= 20; i++ {
		dispositions = append(dispositions, daemon.DispositionWire{
			CommentID: i, Status: "declined",
			Location: fmt.Sprintf("a.go — line %d", i),
			Note:     fmt.Sprintf("ASKED%d", i), Response: fmt.Sprintf("SAID%d", i),
		})
	}
	m := model{
		mode:  modeReraise,
		view:  &daemon.ViewWire{Posted: true, Dispositions: dispositions},
		width: 80, height: 24, ready: true,
	}
	m.reraiseCursor = 17

	out := m.reraiseView()

	if !strings.Contains(out, "ASKED18") {
		t.Errorf("the cursor's decline must stay on screen, got:\n%s", out)
	}
	if h := lipgloss.Height(out); h > m.bodyHeight() {
		t.Errorf("the picker is %d rows, over the %d body:\n%s", h, m.bodyHeight(), out)
	}
	if !strings.Contains(out, "more above") {
		t.Errorf("expected an upward marker near the bottom of a long picker, got:\n%s", out)
	}
}

func TestTheContinuesMarkerIsClippedToANarrowTerminal(t *testing.T) {
	m := tallListModel(2)
	m.width, m.height = 26, 14
	m.view.Comments[0].Note = strings.Repeat("first ", 200)

	out := m.listView()

	if w := lipgloss.Width(lastRow(out)); w > m.width {
		t.Errorf("a marker wider than the terminal wraps into a row the window never counted: %d > %d in %q", w, m.width, lastRow(out))
	}
}

func TestAClippedCommentAwayFromTheCursorIsStillCountedBelow(t *testing.T) {
	// The oversized Comment is the second one; the cursor is on the first. It is
	// another Comment the Reviewer cannot read from here, so it is counted.
	m := tallListModel(3)
	m.height = 14
	m.view.Comments[1].Note = strings.Repeat("second ", 200)

	out := m.listView()

	if last := lastRow(out); last != "  ↓ 2 more below" {
		t.Errorf("expected the clipped second Comment counted with the third, got %q", last)
	}
}

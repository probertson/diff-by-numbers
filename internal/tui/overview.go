package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

const (
	// dispositionIndent is where an Overview item sits, and where its labels sit
	// under the item's own mark.
	dispositionIndent = 2
	// dispositionLabelIndent is where "you asked:" and "agent:" begin.
	dispositionLabelIndent = 6
	// dispositionHang is how much further a wrapped continuation sits in, so a long
	// answer reads as one block instead of running back to the margin (#75). It is
	// a fixed step rather than an alignment under the text after the label, so
	// "you asked:" and "agent:" carry on at the same column as each other.
	dispositionHang = 4
)

// dispositionGroup is one heading of "Since the last round" and the mark its
// items carry. Declines come first: they are the ones the Reviewer may want to
// push back on, and burying them under the changes is what made the agent's
// reasoning easy to miss (#75).
type dispositionGroup struct {
	status  string
	heading string
	mark    string
	style   lipgloss.Style
	// disputable marks a group the Reviewer can push back on. An addressed
	// Comment is not one: the code moved, and there is fresh code to comment on.
	disputable bool
}

func dispositionGroups() []dispositionGroup {
	return []dispositionGroup{
		{status: "declined", heading: "Declined", mark: "✗", style: warnSt, disputable: true},
		{status: "answered", heading: "Answered", mark: "↩", style: accentSt, disputable: true},
		{status: "addressed", heading: "Addressed", mark: "✓", style: addSt},
	}
}

// sinceTheLastRound draws what the agent did with the previous round's Comments,
// grouped by what it did rather than run together as one block. The item carries
// no status word — the heading it sits under says it — and the invitation to
// push back on a decline sits in that heading, where the Reviewer is already
// reading, rather than dimmed below the whole list.
func (m model) sinceTheLastRound(width int) string {
	var b strings.Builder
	b.WriteString(labelSt.Render("Since the last round") + "\n")
	indent := strings.Repeat(" ", dispositionIndent)
	for _, group := range dispositionGroups() {
		var members []daemon.DispositionWire
		for _, disposition := range m.view.Dispositions {
			if dispositionStatus(disposition) == group.status {
				members = append(members, disposition)
			}
		}
		if len(members) == 0 {
			continue
		}
		// A re-raised resolution stays in its group — it is still what the agent
		// did — but the heading says how many have been pushed back on, so the
		// count the Reviewer acts on is the one still standing (#80).
		reRaised := 0
		for _, disposition := range members {
			if _, ok := m.reRaiseOf(disposition.CommentID); ok {
				reRaised++
			}
		}
		name := fmt.Sprintf("%s (%d)", group.heading, len(members))
		if reRaised > 0 {
			name = fmt.Sprintf("%s (%d, %d re-raised)", group.heading, len(members), reRaised)
		}
		hint := ""
		if group.disputable && reRaised < len(members) {
			hint = " — press R to re-raise one"
		}
		b.WriteString(indent + fitRow(name+hint, labelSt.Render(name)+dimSt.Render(hint), width-dispositionIndent) + "\n")
		for _, disposition := range members {
			mark := ""
			if reRaise, ok := m.reRaiseOf(disposition.CommentID); ok {
				mark = fmt.Sprintf("↻ re-raised as #%d", reRaise.ID)
			}
			for _, row := range dispositionItem(group, disposition, mark, width) {
				b.WriteString(row + "\n")
			}
		}
	}
	return b.String()
}

// dispositionStatus is the group a disposition belongs to, defaulting anything
// the daemon did not name to addressed, which is what the Overview showed before
// answered and declined were distinguished. Every surface that sorts by status
// goes through here, so a status none of them knows is counted once rather than
// in one place and not another.
func dispositionStatus(disposition daemon.DispositionWire) string {
	switch disposition.Status {
	case "declined", "answered":
		return disposition.Status
	default:
		return "addressed"
	}
}

// dispositionItem draws one Comment's outcome: the mark, its number and where it
// was raised, then what the Reviewer asked and what the agent said back, each
// hanging in its own block. The blank row at the end is the whitespace that was
// missing when every item ran together into one paragraph (#75).
func dispositionItem(group dispositionGroup, disposition daemon.DispositionWire, reRaisedAs string, width int) []string {
	tail := fmt.Sprintf(" #%d  %s", disposition.CommentID, disposition.Location)
	rows := []string{strings.Repeat(" ", dispositionIndent) +
		fitRow(group.mark+tail, group.style.Render(group.mark)+dimSt.Render(tail), width-dispositionIndent)}
	if reRaisedAs != "" {
		// Under the item rather than beside it: the Reviewer scanning for what is
		// still open reads the marks down the left, and this one says "not this".
		rows = append(rows, strings.Repeat(" ", dispositionLabelIndent)+
			fitRow(reRaisedAs, accentSt.Render(reRaisedAs), width-dispositionLabelIndent))
	}
	rows = append(rows, hangingField("you asked: ", disposition.Note, dimSt, width)...)
	// Required of an answer and a decline, and welcome on one the agent addressed.
	// Its label carries the accent where "you asked:" stays dim, so the eye finds
	// the agent's half of the exchange rather than reading past it (#75).
	if disposition.Response != "" {
		rows = append(rows, hangingField("agent: ", disposition.Response, accentSt, width)...)
	}
	return append(rows, "")
}

// fitRow returns the dressed row where it fits the room it has, and the clipped
// plain text where it does not. lipgloss styling cannot be cut mid-escape, so a
// row that has to be shortened gives up its parts' separate colours rather than
// its integrity. Room of less than one cell means the model has not been sized
// yet, which is no limit — the reading wrapTo gives it.
func fitRow(plain, dressed string, room int) string {
	if room < 1 || lipgloss.Width(plain) <= room {
		return dressed
	}
	return dimSt.Render(truncateTo(plain, room))
}

// hangingField draws one labelled field of an item: the label and the start of
// its text at the label indent, and every continuation a fixed step further in.
// It wraps on spaces, the same rule the Step pane's code wrap follows, and takes
// no row cap — an agent's reasoning is the thing being read. labelStyle dresses
// the label alone; the text it introduces is left plain.
func hangingField(label, text string, labelStyle lipgloss.Style, width int) []string {
	lead := strings.Repeat(" ", dispositionLabelIndent)
	hang := strings.Repeat(" ", dispositionLabelIndent+dispositionHang)
	if width < 1 {
		// Not sized yet: no limit, the reading wrapTo gives it.
		return []string{lead + labelStyle.Render(label) + text}
	}
	first := width - dispositionLabelIndent - lipgloss.Width(label)
	rest := width - dispositionLabelIndent - dispositionHang
	if rest < 1 {
		// Narrower than the indent, so there is no block to hang: clip the row.
		return []string{truncateTo(lead+label+text, width)}
	}
	if first < 1 {
		// The label alone fills its row; what it introduces starts on the next.
		rows := []string{truncateTo(lead+label, width)}
		for _, chunk := range wrapCode(text, rest, rest, len(text)+1) {
			rows = append(rows, hang+chunk)
		}
		return rows
	}
	chunks := wrapCode(text, first, rest, len(text)+1)
	rows := []string{lead + labelStyle.Render(label) + chunks[0]}
	for _, chunk := range chunks[1:] {
		rows = append(rows, hang+chunk)
	}
	return rows
}

// declinesHint is the conclusion screen's last chance to push back: the round
// ends at the hand-off, and a decline the Reviewer disagreed with is easiest to
// forget there. It counts only the declines still standing (#80), so a Reviewer
// who has already pushed back on all of them is not nagged; empty when there are
// none left, or none at all.
func (m model) declinesHint() string {
	declined := m.declinedDispositions()
	standing := 0
	for _, disposition := range declined {
		if _, ok := m.reRaiseOf(disposition.CommentID); !ok {
			standing++
		}
	}
	if standing == 0 {
		return ""
	}
	return fmt.Sprintf("%d of %s not re-raised — R to re-raise", standing, pluralize(len(declined), "decline"))
}

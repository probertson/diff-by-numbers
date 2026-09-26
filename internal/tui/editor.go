package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// commentPlaceholder is what an empty Comment input says, which is what the
// input says until something else opens it.
const commentPlaceholder = "what should change here?"

// noteEditor is what the note editor is open for, supplied by whoever opens it:
// the title over the input, the placeholder in it, the context drawn above it
// and what submitting sends. The editor itself knows nothing about Comments.
//
// Its zero value stands for a Comment, read from the model's Comment fields,
// since a Comment is what the editor was open for before anything else could be
// written; editor() makes that so.
type noteEditor struct {
	title       string
	placeholder string
	// action is what enter does, as the keybar names it: "add" or "save".
	action string
	// context is what the note is about, as rows ready to draw at a width. It
	// takes the model rather than capturing it, so it reads the model as it is
	// when drawn.
	context func(m model, width int) []string
	// submit sends what was written and returns the status to show for it, or
	// "" when there was nothing to send.
	submit func(m model, text string) string
}

// editor is what the note editor is open for, a Comment unless a caller said
// otherwise.
func (m model) editor() noteEditor {
	if m.writing.submit != nil {
		return m.writing
	}
	return m.commentEditor()
}

// openEditor opens the note editor for editor, holding value to begin with,
// and returning to back when it closes.
func (m *model) openEditor(editor noteEditor, value string, back mode) tea.Cmd {
	m.writing = editor
	m.note.Placeholder = editor.placeholder
	m.note.SetValue(value)
	m.note.Focus()
	m.noteReturn = back
	m.mode = modeNote
	m.setNoteHeight()
	return textarea.Blink
}

// commentEditor is the editor raising a Comment on the selection, or editing
// the one editingID names.
func (m model) commentEditor() noteEditor {
	editor := noteEditor{
		title:       "New Comment",
		placeholder: commentPlaceholder,
		action:      "add",
		context:     anchorContext,
		submit:      submitComment,
	}
	if m.editingID > 0 {
		editor.title = "Edit Comment"
		editor.action = "save"
	}
	return editor
}

// reraiseEditor is the editor pushing back on the resolution reraisingID
// names, over the Comment's original wording (#80).
func reraiseEditor() noteEditor {
	return noteEditor{
		title:       "New Comment",
		placeholder: commentPlaceholder,
		action:      "add",
		context:     anchorContext,
		submit:      submitReraise,
	}
}

// anchorContext draws the code a Comment is about above its note: its Anchor,
// or how many lines it covers while the Anchor could not be composed.
func anchorContext(m model, width int) []string {
	if m.pendingCode == "" {
		return []string{dimSt.Render(pluralize(m.pendingSel.rows, "line"))}
	}
	return renderAnchorRows(m.pendingCode, width)
}

// submitComment raises the Comment, or saves the edit to it. An empty note
// sends nothing: esc is how a Comment is abandoned, and ctrl+d how one is
// deleted.
func submitComment(m model, note string) string {
	switch {
	case note == "":
		return ""
	case m.editingID > 0:
		if m.client.editComment(m.editingID, note) {
			return "Comment updated"
		}
		return "could not update the Comment"
	case m.client.raiseComment(m.pendingSel, note):
		return "Comment added"
	}
	return "could not add the Comment"
}

// submitReraise sends whatever is in the box, empty included: the Reviewer
// picked a resolution to push back on, and clearing the pre-filled text means
// "as it stood", not "never mind" — esc is how you take it back.
func submitReraise(m model, note string) string {
	if m.client.reraise(m.reraisingID, note) {
		return fmt.Sprintf("re-raised Comment #%d — it stands again this round", m.reraisingID)
	}
	return "could not re-raise the Comment"
}

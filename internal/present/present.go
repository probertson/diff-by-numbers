// Package present is what the TUI and the Browser Renderer both work out to
// draw a review the same way: how inline Markdown reads within a block, where
// struck words sit among the text an edit kept, and how a Chapter and a side
// are named. Like syntax highlighting, it is presentation only (ADR-0005,
// #166): nothing here decides what a block is, which lines a row shows or what
// an edit changed. That is the review package's, which this one never imports.
package present

import (
	"fmt"
	"strings"
	"unicode"
)

// Spans is which inline spans are open, carried through a block.
type Spans struct{ Bold, Italic, Code bool }

// TokenKind is what a Token of inline Markdown is.
type TokenKind int

const (
	// TokenText is text, drawn in the spans open where it stands.
	TokenText TokenKind = iota
	// TokenMarker is a marker: emphasis, a code span's backtick, or a table's
	// pipe.
	TokenMarker
	// TokenEscape is an escape's backslash, which a surface may draw muted or
	// leave out. The character it escapes follows it as text.
	TokenEscape
)

// Token is a run of a block's text read one way.
type Token struct {
	Kind TokenKind
	Text string
	// Spans are the spans open for text.
	Spans Spans
}

// Inline splits text into its inline markers and the text between them,
// opening and closing spans in st as it goes, so a span opened on one line or
// word stays open on the next. Inside a code span nothing but the closing
// backtick is a marker. An underscore inside a word (snake_case) opens
// nothing, as in CommonMark. With pipes, as in a table row, a pipe is a marker.
func Inline(text string, st *Spans, pipes bool) []Token {
	var out []Token
	var run []rune
	flush := func() {
		if len(run) > 0 {
			out = append(out, Token{Kind: TokenText, Text: string(run), Spans: *st})
			run = nil
		}
	}
	mark := func(kind TokenKind, text string) {
		flush()
		out = append(out, Token{Kind: kind, Text: text})
	}
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		pair := i+1 < len(rs) && rs[i+1] == r
		switch {
		case r == '|' && pipes:
			mark(TokenMarker, "|")
		case r == '`':
			mark(TokenMarker, "`")
			st.Code = !st.Code
		case st.Code:
			run = append(run, r)
		case r == '\\' && i+1 < len(rs):
			mark(TokenEscape, "\\")
			run = append(run, rs[i+1])
			i++
		case (r == '*' || r == '_') && pair && delimiterAt(rs, i, 2):
			mark(TokenMarker, string([]rune{r, r}))
			st.Bold = !st.Bold
			i++
		case (r == '*' || r == '_') && delimiterAt(rs, i, 1):
			mark(TokenMarker, string(r))
			st.Italic = !st.Italic
		default:
			run = append(run, r)
		}
	}
	flush()
	return out
}

// delimiterAt reports whether a run of n emphasis characters at i opens or
// closes a span: a `*` always may, an `_` only at the edge of a word.
func delimiterAt(rs []rune, i, n int) bool {
	if rs[i] == '*' {
		return true
	}
	before := i == 0 || !isWordRune(rs[i-1])
	after := i+n >= len(rs) || !isWordRune(rs[i+n])
	return before || after
}

// isWordRune reports whether a character is part of a word, which an
// underscore beside it is inside of: a letter, a digit or an underscore, in
// any script. Punctuation is not, so `—_this_—` is emphasis, as in CommonMark.
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// Separated sets struck runs apart from the text either side with a space,
// for a line drawn as it stands, where nothing else would: dbn gives struck
// words without the spaces around them, which belong to the text the edit
// kept. struck and text read a run, and space is a kept run of one space.
func Separated[R any](runs []R, struck func(R) bool, text func(R) string, space R) []R {
	var out []R
	for i, run := range runs {
		if !struck(run) {
			out = append(out, run)
			continue
		}
		if i > 0 && !strings.HasSuffix(text(runs[i-1]), " ") {
			out = append(out, space)
		}
		out = append(out, run)
		if i+1 < len(runs) && !strings.HasPrefix(text(runs[i+1]), " ") {
			out = append(out, space)
		}
	}
	return out
}

// RemovedSide reports whether a row on this side is a removal: from the
// merge-base, or from the previous round a Revision Round is compared with.
func RemovedSide(side string) bool { return side == "old" || side == "previous" }

// ChapterHeading names a Chapter by number and name, e.g. "3 · EL-7 Pack
// holds". Numbers and names do the work, so the word "Chapter" is left out.
func ChapterHeading(number int, name string) string {
	return fmt.Sprintf("%d · %s", number, name)
}

// StepRange names the Steps a Chapter holds, e.g. "Steps 12–16", or "Step 3"
// for a Chapter of one.
func StepRange(first, last int) string {
	if first == last {
		return fmt.Sprintf("Step %d", first)
	}
	return fmt.Sprintf("Steps %d–%d", first, last)
}

// LineRange names a run of a file's lines, e.g. "12–16", or "3" for one line.
func LineRange(first, last int) string {
	if first == last {
		return fmt.Sprint(first)
	}
	return fmt.Sprintf("%d–%d", first, last)
}

// Outcome is one heading a Revision Round's Overview sorts the previous
// round's Comments or Agent Questions under, by the status the agent gave each,
// and the mark each item under it carries.
type Outcome struct {
	Status, Heading, Mark string
}

// CommentOutcomes are the headings of "Since the last round", in order.
// Declines come first: they are the ones the Reviewer may want to push back
// on, and burying them under the changes is what made the agent's reasoning
// easy to miss (#75).
var CommentOutcomes = []Outcome{
	{Status: "declined", Heading: "Declined", Mark: "✗"},
	{Status: "answered", Heading: "Answered", Mark: "↩"},
	{Status: "addressed", Heading: "Addressed", Mark: "✓"},
}

// QuestionOutcomes are the headings of the previous round's Agent Questions,
// in order. The agent's own calls come first: the Reviewer was asked and never
// decided them, so they are the ones to check.
var QuestionOutcomes = []Outcome{
	{Status: "agents_call", Heading: "Agent's call", Mark: "◆"},
	{Status: "asked_again", Heading: "Asked again", Mark: "↻"},
	{Status: "addressed", Heading: "Addressed", Mark: "✓"},
	{Status: "no_change_needed", Heading: "No change needed", Mark: "="},
}

// CommentStatus is the status a Comment's disposition is sorted by, defaulting
// anything the daemon did not name to addressed, which is what the Overview
// showed before answered and declined were distinguished. Every surface that
// sorts by status goes through here, so a status none of them knows is counted
// once rather than in one place and not another.
func CommentStatus(status string) string {
	switch status {
	case "declined", "answered":
		return status
	default:
		return "addressed"
	}
}

// AskedWhere names where an Agent Question was asked: on a Step, or with the
// Brief for one asked on the Round.
func AskedWhere(step int) string {
	if step == 0 {
		return "with the Brief"
	}
	return fmt.Sprintf("Step %d", step)
}

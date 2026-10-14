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

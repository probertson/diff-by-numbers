# The MVP renders diffs itself rather than integrating delta or difftastic

The MVP renders unified diffs directly — +/- colouring, no syntax highlighting, no
side-by-side. Integrating a best-of-breed renderer (delta, difftastic) is deferred,
not rejected.

The non-obvious constraint worth recording: borrowing a renderer costs anchoring
precision. What comes back from an external renderer is styled text, and dbn cannot
reliably map a row of it back to a line of source — so it cannot attach a Change
Request to a place in the code. Owning the render loop is what makes anchoring
possible at all.

This tension is live, not hypothetical. Change Requests and question Anchors both point at
a Reviewer-selected *line range* (ADR-0011), so dbn must know precisely what sits on every
row. Adopting an external renderer would mean giving that up, or reconstructing the mapping
from its output. That is the cost anyone proposing delta or difftastic has to pay.

## Amendment (#81): delegate the algorithm, not the rendering

Colour has become more than +/−. A changed row is tinted across its width, green for an
addition and red for a removal. Between a removed line and the added line matched with it, the
words that changed get a stronger tint of the same colour. The tint is the channel for "changed",
so the other signals keep theirs: a Comment is the foreground, the cursor is bold, and a
selection's blue replaces every tint while it lasts.

Finding the changed words is borrowed. `github.com/sergi/go-diff` (diff-match-patch) runs
in-process over the two lines' tokens: words, whitespace runs and single punctuation. It
returns *ranges of runes*, not styled text, so the constraint above holds. dbn still draws
every row, and now also carries each row's changed ranges from the daemon to the TUI, which
only styles them. That keeps highlights correct on everything dbn draws itself: interleaved
before-sides, previous-round rows, tab expansion and wrapped continuations.

Matching a removed line with the added line that took its place never looks outside one
edit, and it reads the edit whole, however many ranges or Steps it is drawn across. Within
an edit, lines match in order when at least 40% of their *words* are shared. Whitespace and
punctuation are compared but not counted: nearly every line of code shares its spaces,
brackets and operators with every other, and counting them matched unrelated lines. A
wrong match could only misplace a highlight on lines already shown, and the threshold keeps
unrelated lines apart. The same matching runs within each edit made since the previous
round, when a Revision Round is shaded by those (#44).

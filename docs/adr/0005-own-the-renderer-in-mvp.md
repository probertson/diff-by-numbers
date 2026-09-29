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
addition and red for a removal. The words an edit removed or added get a stronger tint of the
same colour (as #125 amends below). The tint is the channel for "changed",
so the other signals keep theirs: a Comment is the foreground, the cursor is bold, and a
selection's blue replaces every tint while it lasts.

Finding the changed words is borrowed. `github.com/sergi/go-diff` (diff-match-patch) runs
in-process over tokens: words, whitespace runs and single punctuation. It
returns *ranges of runes*, not styled text, so the constraint above holds. dbn still draws
every row, and now also carries each row's changed ranges from the daemon to the TUI, which
only styles them. That keeps highlights correct on everything dbn draws itself: interleaved
before-sides, previous-round rows, tab expansion and wrapped continuations.

Finding what changed never looks outside one edit, and it reads the edit whole, however
many ranges or Steps it is drawn across, so a row is lit the same wherever it appears. The
same runs within each edit made since the previous round, when a Revision Round is shaded by
those (#44). *How* it finds what changed was line matching, and is now the #125 amendment
below.

## Amendment (#125): diff the whole edit, not line pairs

#81 matched each removed line with the next added line that shared 40% of its words, then lit
what differed within the pair. The Reviewer never saw which line a row was paired with, so a
lit word meant "not in some other line", not "removed" or "added". When hard-wrapped prose was
rewritten and rewrapped, a word that only moved across a wrap was lit on both sides, lines far
apart were paired because they shared a phrase, and the real rewrite was left dark because its
lines found no partner.

Now the removed lines and the added lines of one edit are each read as one passage, with every
line break as whitespace, and diffed once. What changed is mapped back onto each row, so a lit
run means "this text was removed" or "this text was added", and nothing else. It still hands
back rune ranges, and the daemon, the wire format and the TUI are unchanged.

- **What a rewrap moves is never lit.** The whitespace around a line break and the next line's
  indentation count as the break. So does a comment marker (`//`, `#`, `>`, `*`) the next line
  shares with the one before, but only when the other side also carries that marker down
  consecutive lines, as a rewrapped comment does. Otherwise uncommenting code, or dropping a
  `*` dereference from one of two lines, would go unlit. Whitespace compares equal to
  whitespace, and no lit range is whitespace alone, even inside a string literal.
- **No scatter.** An unchanged run of fewer than 2 words between two edits is lit as part of the
  edit. This replaces diff-match-patch's semantic cleanup, which at token level could swallow a
  long unchanged phrase in a heavy rewrite. The cost is that a lone matching word no longer
  splits an edit, so a gofmt-realigned rename is lit as one span (`ChangeRequestID:
  d.ChangeRequestID`) rather than as the two names.
- **An edit or a replacement.** Changed words are lit only when the two sides are one passage
  edited: at least 40% of their words shared, or at least 60% of the smaller side's words (and
  at least 4) found in the larger, which keeps a small edit beside many added lines lit. An edit
  that is neither was replaced, and every row of it is lit whole. So is an edit with more
  distinct tokens than the 131,072 private-use runes the diff encodes them as, which could not
  be diffed faithfully.
- **An entirely new line is lit whole.** Together with the rule above, a tinted row with nothing
  lit always means "all of this text is on the other side too", never "all of this is new" or
  "this was replaced".

A prototype ran both approaches over every modification block in the repository's history:
2,531 blocks, of which 2,042 were Go code, 294 Go comment-only and 195 Markdown.

- **Rewrapped text:** words lit that are unchanged on the other side fell from 627 to 237 in
  Markdown and from 520 to 204 in comments.
- **Code:** the block diff scattered a little, as small unlit gaps such as `return` or `)`
  inside lit lines. That is no worse than line pairs, which also lit 865 runs of realigning
  whitespace.
- **Cleanup:** without semantic cleanup, lines with three or more lit fragments went from 148 to
  410 in code. With it, an unchanged 11-word phrase was swallowed in one rewrite. The
  minimum-match rule brought such lines down to 68 in code and 3 in Markdown, and swallowed no
  phrase.
- **Similarity:** the 40% test alone left 291 code blocks and 24 Markdown blocks unlit that line
  pairs had lit. The 60%-of-the-smaller-side test recovered 127 of the code ones (291 → 164). At
  30%, about 70 more code blocks passed, but more unchanged words were lit. At 50%, about 90
  more code blocks and 15 more Markdown blocks failed.
- **Comment markers:** folding them cut lit ranges containing no word, in comment-only blocks,
  from 98 to 10.

Code and prose are treated alike; the prototype found no need to tell them apart. A large
edit is fast enough to diff whole: an edited copy of a 2,749-line file took 64 ms, and the
result is kept for the round.

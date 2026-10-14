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

## Amendment (#134, #140, #155): parse Documents, still draw them ourselves

Document text is budgeted in words, per block, not in lines (#140, #155): a Step is charged
every word of each changed paragraph, list item, heading or table row it shows, so dbn has to
know where each block of a Document begins and ends. Markdown's block structure is subtle
enough (lazy continuation, setext headings, fences, nested lists) that finding it is borrowed,
like the word diff in #81: `github.com/yuin/goldmark` parses each Document in-process, and dbn
reads only the source positions of its blocks (#134). goldmark never renders anything. It hands
back line ranges, not HTML or styled text, so the constraint above holds: dbn still draws every
row itself, Document rows included, and still knows precisely what sits on each.

## Amendment (#166): structure is the review package's, inline styling is presentation

A Document Step is drawn formatted (#168): each block reflowed to the prose width, its kind
styled, and an edit drawn as track changes. That raises where Markdown is read: by the daemon,
which already parses Documents for their budget (#134), or by the TUI, which lays the text out.
The line falls between structure and styling.

- **Structure is the review package's, parsed with goldmark.** Anything that affects coverage,
  costs or Anchors is decided there: where each block begins and ends, its kind, which source
  lines it spans, its treatment (new, edited, removed or context), the rows an Excerpt is widened
  by, and the words an edit kept, struck and added. The daemon carries all of it to the TUI, which
  never decides any of it again, so the Step view, the budget and every Anchor agree on what a
  block is.
- **Inline styling within a block is presentation, like syntax highlighting.** Emphasis markers,
  list bullets, heading hashes, quote prefixes, hard breaks and table pipes are worked out by the
  TUI from the block's own text. That never changes which source lines a row shows, which is what
  the constraint above protects: dbn still knows precisely what sits on every row, because the
  rows and the blocks they belong to came from the daemon. A styling rule the TUI gets wrong
  draws a word in the wrong style; it cannot move, hide or misattribute a line.

## Amendment (#184): the Browser Renderer may render Documents to HTML

The Browser Renderer (#184) is a page in the Reviewer's browser that draws the Step they are on
in the TUI, so that wide tables, images and typography can be read properly. It renders a
Document's new and unchanged blocks to HTML with goldmark's own renderer: the first time dbn
lets a borrowed renderer draw. The constraint above still holds, because the page only draws.
Nothing on it can be selected, commented on or acknowledged, so dbn never has to map a piece of
its output back to a source line.

- **Anchors still come only from the TUI's rows.** Every Comment, Anchor and question is still
  made in the TUI, from the rows dbn drew there. The TUI tells dbn where its cursor is (a block,
  or a line of code) so the page can brighten it, but that report changes nothing in the review.
- **The block mapping is kept with data attributes.** The element that starts each block carries
  the block's number and its place on the page (`data-block`, `data-place`), set from the source
  positions goldmark parsed, against the blocks the review package decided (#166). The page marks
  a block new, edited, removed, commented on or under the cursor by those attributes, never by
  reading its own HTML. An edited block is not rendered: it is drawn as track changes from the
  review package's word runs, as the TUI draws it. The inline styling both surfaces read from a
  block's text (emphasis, code spans, escapes, pipes) is shared code, so the two agree.
- **Nothing in a Document runs or loads anything.** Raw HTML is shown as text. An image is drawn
  only from dbn, and only from inside the Document's repository; a remote one is never fetched.
  dbn serves a file as an image only if its extension names an image type, and sandboxed, so a
  Document cannot have dbn serve a page, or an SVG carrying a script, that runs in dbn's origin.
  The page allows no script or style but its own.

# Prose-aware diffing

Research for [#135](https://github.com/probertson/diff-by-numbers/issues/135), part of
[#131 Improve the document review experience](https://github.com/probertson/diff-by-numbers/issues/131).

**Question.** What does prose-aware diffing look like, and can it coexist with dbn's
git-derived Changed Lines? A rewrapped paragraph currently reads as wholly changed.

Sources are listed at the end and cited inline as [S1], [S2] and so on. Where a claim rests on an
experiment run for this note rather than on a document, it says so and shows the output.

## Short answer

- **Prose-aware diffing is word diffing across line breaks.** The established tools (GNU wdiff,
  `git --word-diff`) all do the same thing. They split both sides into a stream of words, treat a
  line break as ordinary whitespace, diff the two streams, and map the result back onto the text
  [S2][S3][S7]. Diffing sentences first and then words, or diffing after reflowing the text, are
  refinements of that idea, not alternatives to it.
- **It coexists with Changed Lines, and should not replace them.** A rewrap *is* a change to every
  line it touches. git reports it as one Modification, and that is correct. What reads wrongly is
  dbn's *emphasis*. `internal/intraline` pairs lines one to one, and a rewrap moves words across
  line boundaries, so the pairing lights up the moved words as changes. The fix belongs in the
  emphasis layer. Diff the whole Modification as one token stream, with line breaks treated as
  whitespace, and project the result back onto each row as rune ranges. This is what `git
  --word-diff` does per hunk [S2][S3], and it fits ADR-0005's "delegate the algorithm, not the
  rendering". It needs no new dependency: `sergi/go-diff` is already in use.
- **The Coverage Ledger should not start ignoring reflow.** Every rewrapped line still has to be
  shown. Coverage would change only if dbn counted lines as accounted for without showing them,
  and ADR-0002's Absorption amendment (#85) already rejected that. The same amendment and #84
  give the precedent for what *can* change: what a line **costs** against the Step budget, and
  how it is **styled**. The whole-edit word diff can tell apart a row that holds a real edit from a
  row that only carries moved words.
- **git on its own cannot do this.** `-w`/`-b` compare lines [S1] and do nothing for a rewrap
  (verified below). `--word-diff` does help, but its porcelain output follows the *new* side's line
  breaks and does not give old-side positions, so dbn would have to rebuild them [S1][S3]. It also
  cannot serve Revision-Round edits, which are not git diffs. Computing in-process is the better
  fit.

## How dbn sees a rewrap today

The model in brief:

- **Changed Lines.** `internal/git/deriver.go` runs `git diff --unified=0 -M <merge-base> --` and
  reads the `@@` ranges as Changed Lines. With zero context, each hunk is exactly one edit, so each
  one also becomes a `Correspondence` (a Modification): its removed range paired with the range
  that replaced it. The deriver reads the text of a Changed Line only to find whitespace-only
  lines, which feed Absorption.
- **Coverage Ledger.** `internal/review/coverage.go` builds the ledger from the per-repository
  `Derivation`. A Round cannot post while any atom is unaccounted for (ADR-0003). The Step budget
  (30) counts the new reading a Step asks for. Whitespace-only lines and lines pre-marked by a
  Revision Round are free (ADR-0002 amendments #85 and #84).
- **Intraline emphasis.** `internal/intraline.Emphasis(removed, added)` works one Modification at
  a time (`internal/review/emphasis.go`, called from `view.go` and `sinceround.go`):
  - It walks the removed lines in order and pairs each with the next added line whose words are
    at least 40% shared (delta's `--max-line-distance` default of 0.6, inverted [S12]).
  - It diffs each pair token by token with `sergi/go-diff`. The tokens are words, whitespace runs
    and single punctuation characters.
  - It returns rune ranges per row. A row with no partner gets `nil`, which draws the row tint
    only.

  The before-side is drawn as a block above the after-side it was replaced by, and emphasis only
  annotates those rows. Changing how the ranges are computed therefore changes no layout.

**Experiment: git on a rewrapped paragraph.** The experiment changed one word ("lazy" to "sleepy")
in a three-line Markdown paragraph and rewrapped it to four lines. git 2.39.3:

```text
=== git diff -U0            (and identically with -w)
@@ -5,3 +5,4 @@ Intro line stays.
-The quick brown fox jumps over the lazy dog. It was a sunny
-day and the fox was feeling rather pleased with itself, so it
-decided to jump over the dog a second time.
+The quick brown fox jumps over the sleepy dog. It
+was a sunny day and the fox was feeling rather pleased
+with itself, so it decided to jump over the dog a second
+time.
=== git diff -U0 --word-diff=plain
@@ -5,3 +5,4 @@ Intro line stays.
The quick brown fox jumps over the [-lazy-]{+sleepy+} dog. It
was a sunny day and the fox was feeling rather pleased
with itself, so it decided to jump over the dog a second
time.
```

**Experiment: dbn's `intraline.Emphasis` on the same edit, against a whole-edit token diff.** This
ran as a throwaway test against the current package, and was not committed. Brackets mark
emphasised runes:

```text
--- current per-line Emphasis
-0: The quick brown fox jumps over the [lazy] dog. It[ was a sunny]
-1: day and the fox was feeling rather pleased[ with itself, so it]
-2: decided to jump over the dog a second[ time.]
+0: The quick brown fox jumps over the [sleepy] dog. It
+1: [was a sunny ]day and the fox was feeling rather pleased
+2: [with itself, so it ]decided to jump over the dog a second
+3 UNMATCHED: time.
--- whole-edit token diff (all lines as one stream, line breaks as whitespace)
Equal  "The quick brown fox jumps over the"
Delete "lazy"
Insert "sleepy"
Equal  "dog. It was a sunny day and the fox was feeling rather pleased with itself, so it decided to jump over the dog a second time."
```

Today every wrap point is emphasised as if it were an edit. The whole-edit diff finds exactly the
one real change. On this input it gives the same answer as `git --word-diff`.

## Approaches surveyed

### 1. git's own options

| Option | What it does | Helps with rewraps? |
|---|---|---|
| `-w` / `--ignore-all-space`, `-b` / `--ignore-space-change` | "Ignore whitespace when comparing lines" [S1] | **No.** A line is still the unit, and a rewrap changes line boundaries (the experiment above). |
| `--ignore-blank-lines`, `-I<regex>` | Ignore changes whose lines are all blank or all match [S1] | No. |
| `--diff-algorithm=patience\|histogram\|minimal`, `--anchored` | Different line alignment [S1] | No. They only move where line-level hunks fall. |
| `--color-moved`, `--color-moved-ws` | Colour *lines* moved elsewhere in the diff [S1] | No. Moved *words* inside a paragraph are invisible to it. |
| `--word-diff[=porcelain]`, `--word-diff-regex`, `--color-words` | Word diff inside each hunk [S1] | **Yes**, within one hunk. |

How `--word-diff` works, from the git source [S3]:

- The line diff runs first. The `-` and `+` lines of each hunk are gathered into a minus buffer and
  a plus buffer, and word-diffed when the next hunk header or context line arrives
  (`fn_out_consume` in `diff.c`).
- `diff_words_fill` rewrites each buffer as one word per line and keeps each word's original
  offsets. xdiff then runs on those word lists with zero context (`diff_words_show`).
- Words are runs of non-whitespace by default. With `--word-diff-regex`, "anything between these
  matches is considered whitespace and ignored(!)", and a match is cut at a newline [S1].
- The docs state the limitation: it "operates by taking the same line-by-line diff that is
  produced without the option and computing word-by-word changes within each hunk. This may
  produce a larger diff than a dedicated word-diff tool would." [S1]

Properties that matter to dbn:

- **The scope is one hunk.** With `--unified=0` a hunk is a Modification, so git's scope matches
  dbn's `Correspondence` exactly. Words moved to another paragraph are not matched. That is fine:
  dbn's emphasis already never looks outside one edit (ADR-0005 amendment #81).
- **Old-side positions are lost.** `fn_out_diff_words_aux` prints unchanged runs from the *plus*
  (new-side) buffer [S3]. In porcelain mode a newline is a `~` line of its own [S1]. So the output
  follows the new side's line breaks, and a removed word's old-side line number has to be rebuilt.
  dbn draws the before-side as real old-side rows, so it would have to do that rebuilding.
- **git has a Markdown diff driver, but it only matters if configured.** It is enabled with
  `*.md diff=markdown` in `.gitattributes`. Its word regex is `[^<>= \t]+` and its hunk-header
  regex matches ATX headings [S4]. dbn cannot assume a repository sets this, and the word regex is
  no better than dbn's own tokeniser.

### 2. Dedicated word diff: wdiff and the "one word per line" trick

GNU wdiff "is a front end to diff for comparing files on a word per word basis. It works by
creating two temporary files, one word per line, and then executes diff on these files." It is
described as useful for texts "in which a few words have been changed and for which paragraphs
have been refilled" [S7]. This is the same trick as git's `diff_words_fill`, applied to the whole
file rather than per hunk.

Neil Fraser's *Diff Strategies* (the design notes behind diff-match-patch, which dbn uses through
`sergi/go-diff`) says the right unit depends on the text: "comparing an English document is
generally done on a word-by-word basis" [S8]. It sets out two post-processing passes that matter
for prose:

- **Semantic cleanup** removes "chaff", meaning short coincidental equalities between edits.
- **Semantic lossless** slides edits onto word, line and blank-line boundaries.

dbn already calls `DiffCleanupSemantic` [S9].

### 3. Diffing after reflow or normalisation

This means normalising line breaks away, then diffing the normalised text:

- One paragraph per line (unwrap).
- One sentence per line.
- Collapse all whitespace.

Once line breaks are normalised away, a whitespace-insensitive token diff with a line break as one
more whitespace token *is* a diff after reflow. It has no second representation to keep in step
with the source. Local normalisation is therefore the same thing as approach 2, with whitespace
normalised rather than dropped.

Normalising the *whole file* before a line diff would change which lines git calls changed.
Whitespace-only and reflow-only lines would disappear from the diff. That is the thing the Coverage
Ledger must not do (see below).

**Semantic line breaks** are the authoring-side version: one sentence or clause per line. The
convention exists so that "a change to one sentence or clause shows up as a change to one line,
rather than as a reflow of the entire paragraph" [S6]. dbn cannot impose it on the documents under
review, and switching an existing document to it is itself a whole-paragraph rewrap. It is still a
cheap complement for prose the Authoring Agent writes itself, which is direction 1, agent guidance.

### 4. Sentence diff

Diff sentence sequences first, then words inside matched sentences. This is the prose version of
line-then-word. Its value is less the highlighting than the **unit**. "3 sentences changed in this
paragraph" is a prose-sized measure, which direction 5 (Prose-sized rules) needs.

Segmentation is UAX #29 *Sentence Boundaries* [S5]. Its limits are stated outright:

- "Plain text provides inadequate information for determining good sentence boundaries."
- The default rules "cannot detect cases such as 'Mr. Jones'" without CLDR-based tailoring.

**Experiment: UAX #29 on wrapped and unwrapped text.** Rule **SB4 breaks after every LF**
(ParaSep ÷) [S5], so segmenting hard-wrapped Markdown as-is splits a sentence at every wrap point.
This was checked with both Go implementations, and both behaved the same way:

```text
--- wrapped input
  "The quick brown fox jumps over the sleepy dog, e.g. a\n"
  "beagle. "
  "It was a sunny day and the fox was feeling rather\n"
  "pleased with itself, so it decided to jump again."
--- same paragraph with line breaks replaced by spaces
  "The quick brown fox jumps over the sleepy dog, e.g. a beagle. "
  "It was a sunny day and the fox was feeling rather pleased with itself, so it decided to jump again."
```

So sentence diffing has to unwrap first: join a paragraph's lines with spaces while keeping an
offset map back to (line, rune). It also has to be scoped to paragraphs, and knowing where a
paragraph ends needs a Markdown parser (approach 5). ("e.g. a" did not break, because SB8 holds a
break when a lowercase word follows. An abbreviation followed by a capital would break.)

### 5. Structure-aware (Markdown AST) diffing

CommonMark defines a paragraph as "a sequence of non-blank lines that cannot be interpreted as
other kinds of blocks". A line ending inside it is a *soft line break*, which a renderer may show
as a space or a newline [S10]. That is the formal reason a rewrap means nothing to the reader.

It also marks the limit on ignoring line breaks. Some line breaks are meaningful:

- A **hard break**: a trailing backslash, or two or more trailing spaces [S10].
- A line that starts a **new block**: a list item, a heading, a fence. A reflow can move `- ` or
  `#` to the start of a line and create a list item or heading.
- **Code blocks and tables**, where whitespace carries meaning.

A word-level "only reflowed" verdict is safe **inside a paragraph on both sides**, and nowhere else.

`yuin/goldmark` (CommonMark-compliant, MIT) keeps source positions on block nodes. In v2,
`BlockNode.Source()` returns the text segments a Paragraph or Heading was parsed from [S11]. Those
segments are enough to find which source lines form a paragraph on each side. The same parser is
the likely basis for direction 3 (Markdown-aware display). Using it for diff scoping would put it
behind the same seam rather than adding a second one.

### 6. Why line-pairing highlighters fail on rewraps

delta, and dbn following it, decide which removed line is "homologous" to which added line by a
distance threshold, then diff within each pair [S12]. That model assumes an edited line stays one
line. A rewrap breaks the assumption: each old line's words are spread over two new lines. So
either:

- the pairing succeeds and emphasises the moved words (rows +1 and +2 in the experiment), or
- it fails and leaves a row unmatched (row +3).

A better threshold cannot fix this. The unit has to be the edit, not the line.

## Go libraries

| Library | What it offers | Status | Fit for dbn |
|---|---|---|---|
| `github.com/sergi/go-diff` v1.4.0 (MIT) [S9] | Port of diff-match-patch: `DiffMainRunes`, line mode, `DiffCleanupSemantic` / `…Lossless`, `DiffTimeout` (default 1 s, after which the unfinished part comes back as one delete plus one insert) | Already a direct dependency. Last push June 2025. Upstream `google/diff-match-patch` archived 2024 | **Use it.** The whole-edit diff is the existing `compare` over the concatenated tokens of every line in the edit |
| `znkr.io/diff` v1.0.2 (Apache-2.0) [S13] | Generic `Edits[T comparable](x, y []T)` / `Hunks` over any slice, with positions (`PosX`, `PosY`). `textdiff` adds git's indent heuristic. Myers plus heuristics; much faster than diffmatchpatch in its own benchmarks | Active (release Sept 2026). `go 1.25`; its `go.mod` requires `golang.org/x/tools` for tool directives | Upgrade path. Diffing `[]string` tokens directly removes the private-use-rune encoding in `intraline.compare`. It has no semantic cleanup, so chaff handling would be dbn's own |
| `github.com/aymanbagabas/go-udiff` v0.4.1 (BSD-3) [S14] | Exported copy of `golang.org/x/tools/internal/diff`: Myers, byte-offset edits, unified output | Active | Line and byte oriented. No word-level help. Produces far more edits than the others in znkr's benchmark [S13] |
| `github.com/pmezard/go-difflib` | Partial port of Python's `difflib` (SequenceMatcher, unified output) | Last push 2023 | Nothing prose-specific |
| `github.com/hexops/gotextdiff` | Copy of gopls's internal diff | Last push 2023 | Superseded by go-udiff |
| `github.com/rivo/uniseg` v0.4.7 (MIT) | UAX #29 sentences (`FirstSentenceInString`) plus graphemes, words, line breaking | **Already an indirect dependency** (through charmbracelet) | Sentence segmentation, after unwrapping |
| `github.com/clipperhouse/uax29/v2` v2.5.0 (MIT) | UAX #29 words, sentences, graphemes, run against the official Unicode test suites | **Already an indirect dependency** | The same, with an iterator API. Either works |
| `github.com/yuin/goldmark/v2` (MIT) [S11] | CommonMark AST with source segments on blocks | Active | Paragraph scoping for a "reflow-only" verdict, shared with Markdown-aware display |

Nothing needs adding for the core change. Sentence segmentation is already in the module graph.
goldmark is the only new dependency, and only if dbn adopts the paragraph-scoped "reflow-only"
classification.

## How this relates to dbn's model

**Changed Lines and the Derivation: unchanged.** git's `-U0` Modification is exactly the scope a
word diff needs, and it is already retained as a `Correspondence`. No second git invocation is
needed. The alternative, parsing `--word-diff=porcelain`, has three drawbacks:

- It follows new-side line breaks only [S3].
- It duplicates what `emphasis.go` already resolves from the Round's own sources.
- It cannot serve `roundEditChanges`, which diffs one Round's text against the previous one, not a
  merge-base.

**Intraline emphasis: where the change lands.** `Emphasis(removed, added)` would diff the edit as
one stream:

1. Tokenise every line with the existing `Tokens`.
2. Join the lines with a line-break token that compares equal to any whitespace run.
3. Run one `DiffMainRunes`, then `DiffCleanupSemantic`.
4. Walk the result back to (row, rune range).

The output shape stays `[][]Range`, so `byNumber`, the view, the wire (`toEmphasisWire`) and the
TUI are untouched. Whitespace runs should be *normalised to one token*, not dropped as git drops
them. That keeps "foo bar" and "foobar" different.

Open design point: whether whole-edit mode replaces line pairing everywhere or only for prose.

- **For prose:** clearly better.
- **For code:** it would also handle a formatter re-wrapping a long call. But it could scatter
  emphasis across unrelated lines of a large rewrite, which is what the 40% pairing threshold
  guards against today.
- **A cautious rule:** decide per Modification. Use whole-edit when the *edit's* word similarity
  clears the existing threshold, and otherwise leave rows unemphasised. Or decide by file type, if
  #131 settles a notion of Document. "Prose in code reviews" is out of scope for #131, so gating
  on Markdown is the conservative choice.

**Coverage Ledger: must not change.** A normalised derivation that dropped reflow-only lines would
count lines as accounted for without showing them. The Absorption amendment (#85) rejected that
for whitespace-only lines, because it "would count lines as accounted for without ever showing
them, and a blank line can carry meaning in Markdown or YAML". `CONTEXT.md` puts it as "dbn never
counts a line as accounted for without showing it". The argument is stronger for reflow, because
in Markdown a line break can be structural (approach 5).
The ledger constrains completeness only (ADR-0003). A rewrapped paragraph gets shown anyway, since
an Excerpt shows the paragraph.

**Step budget and styling: the real levers.** The whole-edit diff yields a per-row fact: this row
carries no inserted or deleted token, only moved ones. That is a derived classification with the
same shape as `Derivation.Whitespace`. It could:

- **cost nothing against the Step budget**, following amendment #84's "what a Step shows and what
  it costs are no longer the same number"; and
- **be drawn quieter**, which overlaps direction 2 (Quiet shading).

It should hold only where both sides of the edit lie within Markdown paragraphs. Hard breaks and
block-starting lines are exceptions. It should also be computed where the ledger is built, not in
the view, if the budget is to use it. That means a text-reading step at post time, like the
whitespace check the deriver already does. Whether it belongs in the deriver (git adapter) or in
the core is a design choice. The deriver today reads line text only for whitespace, and the core
already resolves text for emphasis.

**Anchors and prose Comments (direction 7).** Whole-edit ranges give word-level positions per row.
A sentence or phrase Anchor can be expressed as (row, rune range) spans over existing
line-addressed rows. ADR-0005's rule that dbn must know what sits on every row still holds.

**A single-stream inline view (`[-old-]{+new+}`), like `--word-diff=plain`,** is the other display
option. It reads well for prose, but a row would then mix both sides. That conflicts with rows
being side-qualified source lines (ADR-0002, ADR-0005) and with Anchors crossing sides by row. It
is a display question for directions 3 and 7 and the frontend strategy, not a diffing one.

## Open questions this leaves for other tickets

- **Direction 5 (Prose-sized rules):** whether "reflow-only rows cost nothing" is enough, or
  whether documents want a different budget unit. Sentences changed is computable from the same
  whole-edit diff after unwrapping and UAX #29.
- **What a Document is:** by extension (`.md`, `.markdown`) or by content. This decides where
  whole-edit emphasis and the reflow-only classification switch on.
- **Direction 2 (Quiet shading):** how a reflow-only row is drawn.
- **Performance ceiling:** `DiffTimeout` defaults to 1 s. When it is reached, the unfinished part
  comes back as one delete plus one insert: valid, but coarse [S9]. Emphasis is memoised per round (`Session.editChanges`). A whole-document rewrite as
  one Modification is the case to measure.

## Sources

- [S1] git-diff documentation, Git 2.56.0: `--word-diff`, `--word-diff-regex`, `--color-words`,
  `-w`, `-b`, `--diff-algorithm`, `--anchored`, `--color-moved`.
  https://git-scm.com/docs/git-diff
- [S2] Experiment in this note: git 2.39.3 on a scratch repository (output above).
- [S3] git source, `diff.c` at `a018953688f1b10bddf91bff8747068f5f4746a4`: `diff_words_fill`,
  `diff_words_show`, `fn_out_diff_words_aux`, and the per-hunk minus/plus accumulation in
  `fn_out_consume`.
  https://github.com/git/git/blob/a018953688f1b10bddf91bff8747068f5f4746a4/diff.c
- [S4] git source, `userdiff.c` at the same commit: the built-in `markdown` driver (`PATTERNS("markdown", …)`).
  https://github.com/git/git/blob/a018953688f1b10bddf91bff8747068f5f4746a4/userdiff.c
- [S5] Unicode Standard Annex #29, *Unicode Text Segmentation*, revision 49 (2026-09-01), §5 Sentence Boundaries.
  https://www.unicode.org/reports/tr29/
- [S6] Semantic Line Breaks specification. https://sembr.org/
- [S7] GNU wdiff manual, 1.2.3. https://www.gnu.org/software/wdiff/manual/wdiff.html
- [S8] Neil Fraser, *Diff Strategies* (2006). https://neil.fraser.name/writing/diff/
- [S9] `sergi/go-diff` v1.4.0: `diffmatchpatch/diffmatchpatch.go` (`DiffTimeout`,
  `DiffEditCost` defaults) and `diffmatchpatch/diff.go` (`DiffMainRunes`, `DiffCleanupSemantic`,
  `DiffCleanupSemanticLossless`).
  https://github.com/sergi/go-diff/tree/57c41f4cb9849a2e83cdbd7644b31e6d7a7e2586/diffmatchpatch
- [S10] CommonMark Spec 0.31.2: §4.8 Paragraphs, §6.7 Hard line breaks, §6.8 Soft line breaks.
  https://spec.commonmark.org/0.31.2/
- [S11] `yuin/goldmark` v2 branch, `ast/ast.go` (`BlockNode.Source()`), at
  `a8d4ee5a0bd7d5493fdd1e339b4e75df92f8684c`.
  https://github.com/yuin/goldmark/blob/a8d4ee5a0bd7d5493fdd1e339b4e75df92f8684c/ast/ast.go
- [S12] delta, `src/cli.rs` (`--max-line-distance`, default 0.6) at
  `5ddd7fa66ee900b6783e80089174d2170e46f06f`.
  https://github.com/dandavison/delta/blob/5ddd7fa66ee900b6783e80089174d2170e46f06f/src/cli.rs
- [S13] `znkr.io/diff` README and `diff.go` at `97715c649b95d641745f74a481f513c22070bbe9`,
  including its comparative benchmark tables.
  https://github.com/znkr/diff/tree/97715c649b95d641745f74a481f513c22070bbe9
- [S14] `aymanbagabas/go-udiff` README. https://github.com/aymanbagabas/go-udiff
- Repository status (archived flag, last push, latest release, licence) for every library in the
  table: GitHub GraphQL API, queried 2026-09-29.
- dbn internals: `internal/git/deriver.go`, `internal/intraline/intraline.go`,
  `internal/review/{coverage,emphasis,view}.go`, ADR-0002 (with amendments #23, #68, #84, #85,
  #87), ADR-0003, ADR-0005 (with amendment #81), `CONTEXT.md`.

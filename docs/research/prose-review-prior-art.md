# Prior art in prose review

Research for #133, part of #131 (improving the document review experience).

**Question.** How do existing tools present *prose* for review, and what can dbn borrow? For
each tool: how additions and removals are shown on formatted text, how rewrapping is handled,
how comments anchor to prose, and what reviewers complain about.

**Method.** Only primary sources: official docs, specs, source code, commit messages and
first-party engineering or research write-ups. Complaints come from first-party issue trackers,
vendor forums and official mailing lists, and are labelled *user report*. Everything was
checked on 2026-09-29. Some quotes come through a web-fetch tool's extraction of the page
rather than raw source, and so may differ slightly from the page's wording. Two labels are
used throughout:
- **Inference**: reasoning from a source, not something the source says.
- **Unverified**: could not be confirmed from a primary source.

## The short answer

1. **Tools fall into three families.** How they cope with rewrapping, and whether comments can
   anchor, follows from the family.
   - **Render, then diff**: GitHub rich diff, MediaWiki's VisualEditor visual diff, pandiff,
     latexdiff. Rewrapping disappears. But the rendered view loses the link back to source
     lines, and with it inline comments.
   - **Tokenise, then diff**: `git --word-diff`, wdiff, dwdiff, Phabricator's prose engine,
     wikidiff2. The source stays on screen and rewraps are tolerated, as long as the diff is
     computed over a whole block rather than line by line.
   - **Record the edits**: Google Docs, Word, Overleaf, Ink & Switch's Upwelling and
     Patchwork. There is no diff to compute and no rewrapping, but this needs an editor that
     owns the document.
2. **Line-pairing highlighters are the weakest on prose.** delta, git's diff-highlight and
   dbn's current intraline all pair a removed line with an added one, and a reflowed paragraph
   breaks that pairing.
3. **Wholly new text gets no special help anywhere.** Wholly new text is the common case in
   #131. Gerrit's intraline only fires on replacements, and wikidiff2 applies "no
   highlighting" to whole added paragraphs.
4. **The top complaint about rendered diffs is anchoring.** GitHub users cannot comment on the
   rich diff, because comments attach to source lines. That is the cost ADR-0005 already names.
5. **The top complaint about track-changes UIs is noise and no overview.** Ink & Switch's
   answers:
   - hide deletions behind a hover glyph
   - a minibar showing where changes are
   - named groups of edits carrying a rationale
   - AI-written change summaries, which they rated best of everything they tried

   Grouped edits with a rationale are close to what a dbn Step already is.
6. **Two ways to anchor comments to prose.**
   - A character range: Gerrit, Word, Google Docs, Overleaf.
   - A quote plus surrounding context: the W3C Web Annotation TextQuoteSelector, and
     Hypothesis.

   Tools differ most on what happens when the anchored text is deleted: the comment is
   deleted, auto-resolved, shown as "original content deleted", or moved to an Orphans tab.

---

## 1. Render, then diff

### GitHub rich diff ("rendered prose diffs")

**Additions and removals.**
- Shipped in 2014: "Commits and pull requests including prose files now feature source and
  rendered views." Formatting changes get special treatment: "Non-text changes appear with a
  low-key dotted underline. Hover over the text to see what has changed." [GH-blog]
- Current docs: removed text "is struck out with a red background. The new text has a green
  background" (screenshot alt text).
- Attributes get a tooltip: "We provide a tooltip describing changes to attributes that,
  unlike words, would not otherwise be visible in the rendered document". For example
  `href: /octo-org-repo/… -> /octo-org/octo-repo/…`. [GH-docs]
- Embedded HTML "should always be reviewed in both the rendered and source views for
  completeness". [GH-docs]
- Large changes can fail to render ("When the changes take too long to analyze … you'll see an
  error message"). [GH-docs]

**Rewrapping.** The docs don't say. Rendered Markdown joins soft line breaks, so a pure rewrap
should show nothing. *Inference, unverified.*

**Comment anchoring.**
- "Commit comments can only be added to files within the *source* view, on a line-by-line
  basis." [GH-docs]
- The 2025 redesign of PR "Files changed" brought back "rich diff previews … for image and
  markdown files". Commenting in the preview is not mentioned. [GH-cl-0731]
- Suggested changes replace whole source lines: "The lines you selected are copied into the
  comment box". [GH-suggest] *Inference:* a one-word fix to an unwrapped paragraph means
  resending the whole paragraph.

**Complaints** (user reports).
- Comments made in source view don't show in the rich view: "comments are attached to line
  numbers, which are less relevant / accurate in the rich diff view". The reporter suggests a
  Google-Docs-style sidebar. [GH-160981]
- A request to comment on the rendered Markdown diff; a reply describes having to map rendered
  content back to raw lines by hand. [GH-186730]
- The rich diff fails on modified files with "unable to render the document prior to
  diffing". [GH-44477]
- A request for "a warning when comments exist only in source view". [GH-203750]

### GitLab

- Merge requests have no rendered Markdown diff. The only views are inline and side-by-side,
  plus a toggle to hide whitespace changes. [GL-changes]
- The feature request "Provide option to render markdown changes in merge request diffs" has
  been open since 2017. [GL-17000]
- Suggestions are anchored to lines and limited to 100 lines above and below. [GL-suggest]

### MediaWiki VisualEditor visual diff

- Diffs "the HTML of two documents" with diff-match-patch plus treeDiffer.js. [MW-visual]
- Additions, removals, new links and formatting are highlighted in place. "Other changes, such
  as changing the size of an image, are described in notes on the side". [MW-VEdiffs]
- Detects swapped paragraphs. [MW-VEdiffs]
- Can be toggled against the wikitext diff. [MW-VEdiffs]
- Known weakness: "complex changes to tables". [MW-VEdiffs]
- Wikis have no inline review comments, so anchoring doesn't arise.

### pandiff

- "Prose diffs for any document format supported by Pandoc". [pandiff]
- How it works (source): converts both sides to HTML with pandoc, runs `htmldiff`, and
  renders back with `--wrap=none`. So source line breaks never appear, and rewraps are
  invisible (*inference*).
- Outputs CriticMarkup, HTML, PDF or docx with Track Changes.
- Claims to respect "document structure, so won't produce broken markup like `wdiff`".
- Gives up above a `threshold` ("N% of the content has changed").

### latexdiff

- Finds "significant differences … (i.e., ignoring the number of white spaces and position of
  line breaks)", "on the level of individual words". [latexdiff]
- By default, "Added text … wavy-underlined and blue, discarded text … struck out and red".
  There are other styles, including change bars.
- Commands are compared "as one unit" except for text arguments.
- Figures and math have their own markup modes.
- Its FAQ lists failures on complex equations and citations inside `\mbox`.

---

## 2. Tokenise, then diff

### git

**Word diff.**
- `--word-diff[=plain|color|porcelain]`. Words are "delimited by whitespace" by default, and
  can be redefined with `--word-diff-regex` / `diff.wordRegex` / a gitattributes driver's
  `wordRegex`. git ships a built-in `markdown` diff driver: word regex `[^<>= \t]+`, hunk
  headers from ATX headings. [git-diff-options] [git-attributes] [git-userdiff]
- Word diff works per hunk: it takes "the same line-by-line diff … and comput[es] word-by-word
  changes within each hunk". [git-diff-options]
- **Tested locally** (git 2.39.3, by the research agent). Take a 3-line paragraph, rewrap it,
  and change one word:
  - Plain `git diff` and `-w` show all 3 lines removed and all 3 added.
  - `--word-diff` shows only `[-jumps-]{+leaps+}`, laid out in the new file's line breaks.
  - So within one hunk, a rewrap costs nothing. Text that moves to a different hunk is never
    matched (*inference*).

**Other options.**
- `-w` / `-b` compare "lines", not paragraphs. [git-diff-options]
- `--color-moved` matches whole lines, so it helps sentence-per-line text but not rewrapped
  paragraphs (*inference*). [git-diff-options]

**Line-pairing highlighters.**
- git's `contrib/diff-highlight`, used by diff-so-fancy, "will only highlight hunks in which
  the number of removed and added lines is the same, and it will pair lines within the hunk by
  position". [diff-highlight]
- delta pairs "homologous" lines by edit distance (`--max-line-distance`, default 0.6).
  [delta-arch] Its manual suggests `--word-diff-regex='\S+' --max-line-distance=1.0` to act
  "more similar to `git --word-diff`". [delta]
- *Inference:* a rewrap that changes the line count gets no word highlights from
  diff-highlight, and weak ones from delta.

### wdiff and dwdiff

- **wdiff** "works by creating two temporary files, one word per line, and then executes diff
  on these files". It is built for text "for which paragraphs have been refilled", taking
  "common words and whitespace … from new_file". [wdiff]
  - Markers `[-` and `{+` can be customised, e.g. to LaTeX or troff.
  - `-d` post-processes an existing unified diff.
- **dwdiff** also diffs words, with configurable delimiters. [dwdiff]
  - `--match-context` requires surrounding words to match too. It "improves the output … for
    large changes with frequently occurring words".
  - It can show old and new line numbers (`-L`).

### Phabricator / Phorge prose diff engine

The most complete tokenising design found. It was built for Phriction (the wiki) and task
descriptions, and is also used for Markdown cells in Jupyter notebooks under review.
[PhabJupyter]

**Hierarchical.** The engine works level by level, recursing only into blocks that changed:
"Level 0: Split into paragraphs", "Level 1: Split into sentences", "Level 2: Split into
words", "Level 3: Split into characters". [PhabEngine]

**Designed for readability, not minimal edit distance.**
- Commit messages record why. Character diffs "are technically correct (they minimize edit
  distance), but not how a human would diff them. A human can more easily parse an entire word
  change". Hence smoothing to whole words. [libphutil-commits]
- `reorderParts()`: "put all the "-" parts together first, then all the "+" parts together.
  This produces a more human-readable result than intermingling them." [PhabProseDiff]
- `getSummaryParts()` replaces unchanged text with "...". [PhabProseDiff]

**Iterative design.** Identical, purely added and purely removed paragraphs are set aside
first. What's left is smushed into blocks, and sentences are diffed within them.
[libphutil-commits]

**Limits.**
- An edit-distance matrix capped at 128 units per level. "If this text was too big to diff,
  don't try to subdivide it." [PhabEngine]
- On very large documents it can "render an "everything changed, good luck" diff".
  [PhabT7643]

**Display.** One-up and inline (`span.old` / `span.new` in `pre-wrap`), in Old / New / Diff
tabs with Diff selected. It shows the raw Remarkup source, not a rendered diff. [PhabDetailView]

**Complaints** (user reports).
- Before the rework: "results are particularly bad when a large paragraph has a small number of
  changes … I've added only one word … but the algorithm has butchered the diff". [PhabT7643]
- Wikimedia: task description diffs were "completely misidentified" because of 80-column
  wrapping. Fixed by the T7643 work. [WM-T78824]

### MediaWiki wikidiff2

**Paragraph = line.** In wikitext each paragraph is one line, so a line diff is a paragraph
diff. Changed lines then get a word-level diff (skipped above a complexity limit). [wikidiff2]

**Thresholds.**
- `change_threshold` 0.2: below it, a changed line is shown as a deleted line plus an added one.
- `moved_line_threshold` 0.4: above it, lines are shown as moved, with an arrow icon.
- `maxSplitSize` lets one old line be diffed against several new lines (a split paragraph).

**Whole paragraphs.** "Where whole paragraphs have been removed or inserted, no highlighting or
bolding is applied and the other side is blank." [MW-HelpDiff]

**Lessons from tuning moves.** Wikimedia Germany's 2017–18 moved-paragraph work paired moved
paragraphs "with unrelated text from accidentally aligned paragraphs". User reports called the
result "weird and less intuitive than before", and the threshold was "just a wild guess".
Their conclusion: "Thresholds need to be tweaked with care." [WMDE-wikidiff2]

### Code review tools on prose

**Gerrit.**
- Intraline edits exist "only … when … the DiffContent is a replace". [Gerrit-REST] So wholly
  added lines never get intraline marks.
- A character-level Myers diff. Edits ≤5 characters apart are joined, because "we tend to get
  better results by joining them together". [Gerrit-IntraLine]
- Four whitespace-ignore levels, all per line, so a reflow still shows as changed lines
  (*inference*). Long lines wrap at a "Columns" preference. [Gerrit-UI]
- Comments anchor to a character range (`start_line/start_character/end_line/end_character`).
  [Gerrit-REST]
- Comments are ported to later patch sets at "the next best position", which can fall back to
  a file or patch-set comment. [Gerrit-REST]
- Markdown is rendered only in comments, not in changed files. Chromium developers linked to
  Gitiles' rendered view instead (user report). [chromium-dev]

**Reviewable.**
- Comments are line-anchored. A discussion "will also appear in the diffs of other revisions,
  at the nearest corresponding line". A red marker means "the current context may be
  inaccurate", with a link to the original. [Reviewable-files]
- Soft-wraps long lines with a bar in the margin.
- Collapses whitespace-only line changes by default.
- Tracks review "of each file, at each revision, for each reviewer". A user asked for
  line-level review marks. [Reviewable-414]
- Rendered Markdown diffs are an open request. The maintainer's sketch is to "diff the raw
  Markdown, use unified.js to render it, then apply the delta styling in the right spots
  thanks to unified.js keeping track of source locations". [Reviewable-848]
- User report: small intraline changes are hard to spot. [Reviewable-161]

**Phabricator Differential.**
- Comments go on line ranges.
- "We are very aggressive about porting comments forward". Ported comments look "ghostly" and
  link back to their original context. [Phab-inlines]

**ReviewNB** (Jupyter).
- Markdown cells are shown as a rendered visual diff (inferred from a support issue).
- Comments at "the single-line or cell level". [ReviewNB]
- Alignment starts from git's diff at notebook level, then a per-cell algorithm (maintainer,
  in a support issue). [ReviewNB-43]

---

## 3. Record the edits (track changes)

### Google Docs

**Suggesting mode.** "You'll see your change in a new color. Anything you delete will be
crossed out." Suggestions are accepted or rejected one by one or all at once, and the document
can be previewed with or without them. [GDocs-suggest]
- The API has three views: inline, accepted preview and rejected preview. Only the inline view
  "provides the correct indexes". [GDocs-API]

**Compare documents** shows the differences "as 'Suggested Edits' in a new document". [GDocs-compare]

**Anchoring.**
- Comments attach to a highlighted selection. [GDocs-comments]
- The Drive API says anchors "are immutable, and their position relative to the content of a
  document cannot be guaranteed between revisions". [Drive-comments]
- When anchored text is deleted, the comment shows as "Original content deleted". The only
  first-party evidence is a user report about API-created comments. [gws-cli-169] UI behaviour
  is **unverified**.

**Rewrapping** does not arise: text is stored as paragraphs, and wrapping is only rendering.

### Microsoft Word

**Views.**
- Simple Markup: "a red line in the margin".
- All Markup: colours per reviewer, underlined insertions and struck-through deletions.
- No Markup.
- Original.

[Word-track]

**Other display options.** Deletions can go in balloons in the margin or inline. Formatting
changes are tracked as a separate category. [Word-options] Compare offers "character level or
the word level" granularity. [Word-compare]

**Anchoring.**
- A comment anchors to a range: `w:commentRangeStart` and `w:commentRangeEnd` in OOXML.
  [OOXML-comment]
- Deleting the anchored text deletes the comment. A Microsoft employee calls this "a feature
  limitation" (user report, Microsoft Q&A). [MSQA-5298432]

**Complaint**, quoted by Ink & Switch from a study participant: "I hate Track Changes! I'm
dyslexic. I find reading Track Changes to be nearly impossible." [Upwelling]

### CriticMarkup

**Syntax.** The spec now lives in the toolkit README; criticmarkup.com has lapsed. Five marks
are written inside the source: `{++ ++}`, `{-- --}`, `{~~ old ~> new ~~}`, `{>> comment <<}`
and `{== highlight ==}`. They render to `<ins>`, `<del>` and `<mark>`. [CriticMarkup]

**Caveats in the spec itself.**
- "Avoid Newlines in CriticMarkup … it is possible for you to break the syntax and output
  invalid HTML."
- "Wrap Markdown Tags Completely", because the processor "chokes" on marks that cut through
  emphasis.

**Comments** are free text placed next to a highlight. There is no author, date or thread.

**pandoc** has no CriticMarkup support; the request has been open since 2016. jgm's objection
is that "it is a source-file transformation rather than an AST transformation". [pandoc-2873]
Its `--track-changes` option applies only to the docx reader. [pandoc-manual]

### Overleaf

**Display** (source).
- Tracked insertions get a green background.
- Deletions are a zero-width dotted red marker at the deletion point, not struck-through text.
  [Overleaf-ranges]

**Anchoring.** Comments are live character-offset ranges on one evolving document. Each insert
and delete shifts, grows or shrinks them, by rules "consistent with Word". [Overleaf-tracker]

**Complaint** (user report): highlighted versus unhighlighted comments is "very subtle and hard
to spot (especially if color blind…)". [Overleaf-1277]

### Notion, Confluence, Draftable (brief)

**Confluence.**
- Page comparison shows green for added, red for deleted and blue for "Changed formatting".
  Unchanged sections collapse to an ellipsis. [Confluence-compare]
- Inline comments are auto-resolved when their text is deleted. Atlassian's own tracker has
  bugs where this fired wrongly. [CONFCLOUD-74118]

**Notion.** Suggestions appear in the page margin. Version history "won't capture every single
change". [Notion-suggest] [Notion-history]

**Draftable.**
- Redline view marks inserted, deleted and moved text, with moves in blue.
- "Show style changes" can be set to content, styles or both.
- Notes attach to a *change* in a change list, not to a text position. [Draftable]

### Ink & Switch: Upwelling and Patchwork (research prototypes)

**Upwelling** (2023). [Upwelling]
- Insertions get "a light background color", in a colour per author.
- Deletions are hidden behind a small glyph that shows the text on hover. This replaced
  strikethrough after the dyslexic participant's complaint.
- Edits are grouped into named drafts ("Reworking the introduction", "Copyedit Section 2").
  The lesson: "This grouping is particularly useful for collaborators who need to review what
  has changed and why". Keep drafts "fairly small and focused on a single subject".
- Criticises Google Docs for "conflat[ing] change tracking with review".

**Patchwork notebook** (2024). [Patchwork]
- Prose diffs are "very noisy, making it hard to review (e.g. suggested edits in Google Docs)"
  and "it can be hard to get a high-level glance of what's changed".
- Tried: margin before/after notes, word and sentence stats, "blobs", a "minibar" and
  per-section stats. Preferred: "the hover-to-show-deleted (full document) and the minibar
  (summary)".
- Edit groups let authors "retroactively supply rationale for particular edits, including
  grouping related edits together", for example "addition of Oxford commas throughout the
  doc".
- An AI bot that "leaves comments with edit rationale" was "helpful for understanding whether
  to accept a particular edit".
- AI-generated summaries of each edit were "more successful than any of our other diff
  visualizations so far".
- Still open: "the relationship between comments on the history and comments in the document
  itself."

**Peritext** (2022). [Peritext]
- Comment spans anchor to character IDs and do not grow when text is typed at their edges.
- Multiple comments can overlap.
- Deleted characters become tombstones, and anchors stay on them.

---

## 4. Anchoring prose that changes: W3C and Hypothesis

**W3C Web Annotation Data Model.** [W3C-anno]
- `TextQuoteSelector` copies the `exact` text, plus a `prefix` and `suffix` "to distinguish
  between multiple copies of the same sequence".
- `TextPositionSelector` (start and end offsets) is "very brittle with regards to changes to
  the resource".
- Several selectors can describe the same target "to maximize the chances that it will be
  discoverable later".

**Hypothesis client** (current source). [Hypothesis-src]
1. Try the range selector, then the position selector. Each only counts if the text found
   still equals the quote.
2. Otherwise, fuzzy-match the quote. Up to half the quote's length (max 256) may be in error.
   Candidates are scored by quote 50, prefix 20, suffix 20 and position 2 ("Used as a
   tie-breaker"). Context is captured at 32 characters either side.

**Orphans.** "If an annotation is anchored to a sentence in a paragraph and the whole paragraph
is deleted, then even the smartest algorithm isn't going to help." Orphans get their own tab.
Before that they were hidden, and annotations "seemed to be gone". [Hypothesis-orphans]

**What happens when anchored text is deleted, tool by tool:**

| Tool | Behaviour |
| --- | --- |
| Word | comment deleted |
| Confluence | comment auto-resolved (and complained about) |
| Google Docs | "Original content deleted" (unverified for UI comments) |
| Hypothesis | moved to an Orphans tab |
| Peritext | stays on tombstones |
| Gerrit | ported to the next best position, down to file or patch-set level |
| Phabricator | ported aggressively, shown "ghostly" |

---

## 5. Writing conventions that make prose diff well

- **Kernighan, *UNIX for Beginners*** (1978 2nd ed.): "Start each sentence on a new line. Make
  lines short, and break lines at natural places … Since most people change documents by
  rewriting phrases and adding, deleting and rearranging sentences, these precautions simplify
  any editing you have to do later." [Kernighan] The rationale is about editing, not diffs.
- **Brandon Rhodes, "Semantic Linefeeds"** (2012): a text file "becomes far easier to edit and
  version control". He shows a 5-line reflow diff shrinking to a 1-line diff. [Rhodes]
- **Semantic Line Breaks spec** (sembr.org) [SemBr]:
  - A break "MUST NOT alter the final rendered output", "MUST occur after a sentence", and
    "SHOULD occur after an independent clause".
  - "a change to one sentence or clause shows up as a change to one line, rather than as a
    reflow of the entire paragraph".
  - It still recommends `--word-diff`.
  - It advises converting "any new or revised text" rather than whole documents.
- **Asciidoctor recommended practices**: "put each sentence on its own line". It "prevents
  reflows" and lets you "easily swap sentences". [Asciidoctor]

---

## What dbn could borrow

These are directions for the #131 tickets to weigh, not decisions. They map to #131's numbered
directions.

1. **Diff prose over a whole Modification, not over line pairs** (direction 4).
   - dbn's intraline (ADR-0005 amendment) pairs lines at a 40% shared-word threshold, like
     delta. A rewrapped paragraph breaks that pairing.
   - git's per-hunk word diff, wdiff and Phabricator all tolerate rewraps by tokenising the
     whole block. A Modification is already dbn's "one edit, read whole", so it is the natural
     unit.
   - Phabricator's paragraph → sentence → word hierarchy is the most developed model, along
     with its readability rules: smoothing to whole words, removals grouped before additions,
     and joining nearby edits (Gerrit's ≤5 characters).
   - It still returns rune ranges, so ADR-0005's constraint holds.
   - WMDE's lesson applies to any threshold: tune it on real documents, not by guess.
2. **All-addition text needs its own treatment, because nobody else gives it one** (direction
   2).
   - Gerrit and wikidiff2 simply give it nothing extra. GitHub paints it all green.
   - Upwelling's "light background color" for insertions is the one deliberate design found.
     Its hover-to-reveal deletions and the dyslexia complaint show that heavy markup is
     actively harmful to reading prose.
3. **A rendered view must keep a source map, or it loses Anchors** (directions 3 and 7).
   - GitHub's rich diff shows the failure: its most-cited complaint is that comments can't be
     made or seen there.
   - Reviewable's maintainer sketch is the pattern that avoids it: diff the source, render
     with a renderer that tracks source positions, and style the rendered output from the
     source ranges.
   - GitHub's attribute tooltip, VisualEditor's side notes and Word's separate formatting
     category show that changes invisible when rendered (link targets, image sizes) need their
     own channel.
4. **A quote-plus-context Anchor would survive rewrapping and Revision Rounds** (direction 7).
   - dbn's Anchor already carries its text, so it is close to a W3C TextQuoteSelector.
   - Adding prefix and suffix context, with position as a tie-breaker (Hypothesis's
     weighting), would let a sentence or phrase Anchor re-find itself after edits.
   - When it can't, the prior art argues for a visible orphan state over silent deletion or
     auto-resolution. Silent deletion (Word) and auto-resolution (Confluence) are what users
     complain about.
5. **Sub-line coverage has precedent at paragraph and sentence level** (direction 5).
   - wikidiff2 treats one line as one paragraph.
   - Phabricator levels by paragraph, then sentence.
   - Reviewable users asked for review marks finer than a file.
   - Nothing found counts coverage by sentence. That part would be new.
6. **Summaries and a "where are the changes" strip beat inline markup for overview**
   (direction 6).
   - Patchwork preferred the minibar, plus per-section stats.
   - AI change summaries were their most successful visualisation.
   - Confluence collapses unchanged sections to an ellipsis.
   - The Authoring Agent's Brief and Step Explanations already fill the summary role.
     Patchwork's edit groups with a rationale are very close to Steps, which is independent
     support for dbn's premise applied to prose.
7. **Agent guidance can adopt semantic line breaks** (direction 1).
   - If the Authoring Agent writes new prose one sentence or clause per line (SemBr, Rhodes,
     Asciidoctor), line-based Excerpts, coverage and Anchors work on prose as they are.
     SemBr's own rule means the rendered output is unchanged.
   - The cost: it prescribes a style for the Reviewer's repository, and it doesn't help with
     existing hard-wrapped documents.

## Gaps and unverified items

- **GitHub:** rich diff behaviour on a pure rewrap (inferred, not documented).
- **Google Docs:** UI behaviour for comments whose text is deleted. The forum threads need
  JavaScript to load.
- **Overleaf:** whether deleted text is shown in the review panel.
- **ReviewNB:** exact rendering mechanics.
- **Gerrit:** issue 9588 (the tracker needs sign-in).
- **Reviewable:** intraline granularity is undocumented.
- **Patchwork:** the "character counts are the wrong metric" wording was not confirmed
  verbatim.

## Sources

- [GH-blog] https://github.blog/news-insights/product-news/rendered-prose-diffs/
- [GH-docs] https://docs.github.com/en/repositories/working-with-files/using-files/working-with-non-code-files
- [GH-cl-0731] https://github.blog/changelog/2025-07-31-pull-request-files-changed-public-preview-experience-july-31-updates/
- [GH-suggest] https://docs.github.com/en/pull-requests/how-tos/review-pull-requests/commenting-on-a-pull-request
- [GH-160981] https://github.com/orgs/community/discussions/160981
- [GH-186730] https://github.com/orgs/community/discussions/186730
- [GH-44477] https://github.com/orgs/community/discussions/44477
- [GH-203750] https://github.com/orgs/community/discussions/203750
- [GL-changes] https://docs.gitlab.com/user/project/merge_requests/changes/
- [GL-17000] https://gitlab.com/gitlab-org/gitlab/-/issues/17000
- [GL-suggest] https://docs.gitlab.com/user/project/merge_requests/reviews/suggestions/
- [git-diff-options] https://git-scm.com/docs/git-diff (source: `Documentation/diff-options.adoc`)
- [git-attributes] https://git-scm.com/docs/gitattributes
- [git-userdiff] https://github.com/git/git/blob/master/userdiff.c
- [diff-highlight] https://github.com/git/git/blob/master/contrib/diff-highlight/README
- [delta] https://github.com/dandavison/delta
- [delta-arch] https://github.com/dandavison/delta/blob/main/ARCHITECTURE.md
- [wdiff] https://www.gnu.org/software/wdiff/manual/wdiff.html
- [dwdiff] https://manpages.debian.org/unstable/dwdiff/dwdiff.1.en.html
- [difftastic] https://github.com/Wilfred/difftastic/tree/master/manual/src (Markdown not supported; falls back to line diff)
- [pandiff] https://github.com/davidar/pandiff
- [latexdiff] https://ctan.org/pkg/latexdiff ; https://github.com/ftilmann/latexdiff
- [PhabEngine] https://github.com/phacility/phabricator/blob/master/src/infrastructure/diff/prose/PhutilProseDifferenceEngine.php
- [PhabProseDiff] https://github.com/phacility/phabricator/blob/master/src/infrastructure/diff/prose/PhutilProseDiff.php
- [PhabDetailView] https://github.com/phacility/phabricator/blob/master/src/applications/transactions/view/PhabricatorApplicationTransactionDetailView.php
- [PhabJupyter] https://github.com/phacility/phabricator/blob/master/src/applications/files/document/PhabricatorJupyterDocumentEngine.php
- [libphutil-commits] https://github.com/phacility/libphutil/commit/b3aa54fb946054f0cda18b3a72bf17fa359c12a1 , …/a64328ab3d62005d9cd1cbccc61b8c0c914b2d98 , …/f36c31c991cad7f25a730cff2c2434beef21a2cf , …/086df1ba443c08c41a52e712c3bb59bcad718160
- [PhabT7643] https://secure.phabricator.com/T7643
- [Phab-inlines] https://github.com/phacility/phabricator/blob/master/src/docs/user/userguide/differential_inlines.diviner
- [WM-T78824] https://phabricator.wikimedia.org/T78824
- [wikidiff2] https://www.mediawiki.org/wiki/Wikidiff2
- [MW-HelpDiff] https://www.mediawiki.org/wiki/Help:Diff
- [WMDE-wikidiff2] https://www.mediawiki.org/wiki/Wikidiff2/How_we_improved_Wikidiff2
- [MW-VEdiffs] https://www.mediawiki.org/wiki/VisualEditor/Diffs
- [MW-visual] https://www.mediawiki.org/wiki/Visual_diffs
- [Gerrit-REST] https://gerrit-review.googlesource.com/Documentation/rest-api-changes.html
- [Gerrit-UI] https://gerrit-review.googlesource.com/Documentation/user-review-ui.html
- [Gerrit-IntraLine] https://github.com/GerritCodeReview/gerrit/blob/master/java/com/google/gerrit/server/patch/IntraLineLoader.java
- [chromium-dev] https://groups.google.com/a/chromium.org/g/chromium-dev/c/L0F20QinsLg
- [Reviewable-files] https://docs.reviewable.io/files
- [Reviewable-161] https://github.com/Reviewable/Reviewable/issues/161
- [Reviewable-414] https://github.com/Reviewable/Reviewable/issues/414
- [Reviewable-848] https://github.com/Reviewable/Reviewable/issues/848
- [ReviewNB] https://blog.reviewnb.com/commenting-and-discussion-on-jupyter-notebook/
- [ReviewNB-43] https://github.com/ReviewNB/support/issues/43
- [GDocs-suggest] https://support.google.com/docs/answer/6033474
- [GDocs-API] https://developers.google.com/workspace/docs/api/how-tos/suggestions
- [GDocs-compare] https://workspaceupdates.googleblog.com/2019/06/compare-docs.html
- [GDocs-comments] https://support.google.com/docs/answer/65129
- [Drive-comments] https://developers.google.com/workspace/drive/api/guides/manage-comments
- [gws-cli-169] https://github.com/googleworkspace/cli/issues/169
- [Word-track] https://support.microsoft.com/en-us/office/track-changes-in-word-197ba630-0f5f-4a8e-9a77-3712475e806a
- [Word-options] https://support.microsoft.com/en-us/word/track-changes-options
- [Word-compare] https://support.microsoft.com/en-us/office/compare-document-differences-using-the-legal-blackline-option-dbfc7351-4022-43a2-a0c4-54d1898702a0
- [OOXML-comment] https://learn.microsoft.com/en-us/dotnet/api/documentformat.openxml.wordprocessing.commentrangestart
- [MSQA-5298432] https://learn.microsoft.com/en-us/answers/questions/5298432
- [CriticMarkup] https://github.com/CriticMarkup/CriticMarkup-toolkit
- [pandoc-2873] https://github.com/jgm/pandoc/issues/2873
- [pandoc-manual] https://pandoc.org/MANUAL.html
- [Overleaf-ranges] https://github.com/overleaf/overleaf/blob/main/services/web/frontend/js/features/source-editor/extensions/ranges.ts
- [Overleaf-tracker] https://github.com/overleaf/overleaf/blob/main/libraries/ranges-tracker/index.cjs
- [Overleaf-1277] https://github.com/overleaf/overleaf/issues/1277
- [Confluence-compare] https://confluence.atlassian.com/doc/page-history-and-page-comparison-views-139379.html
- [CONFCLOUD-74118] https://jira.atlassian.com/browse/CONFCLOUD-74118
- [Notion-suggest] https://www.notion.com/help/suggested-edits
- [Notion-history] https://www.notion.com/help/duplicate-delete-and-restore-content
- [Draftable] https://help.draftable.com/hc/en-us/articles/19002235946265-Redline-comparisons ; https://help.draftable.com/hc/en-us/articles/32514479509785
- [Upwelling] https://www.inkandswitch.com/upwelling/
- [Patchwork] https://www.inkandswitch.com/patchwork/notebook/2024-version-control/
- [Peritext] https://www.inkandswitch.com/peritext/
- [W3C-anno] https://www.w3.org/TR/annotation-model/
- [Hypothesis-src] https://github.com/hypothesis/client (`src/annotator/anchoring/html.ts`, `match-quote.ts`)
- [Hypothesis-orphans] https://web.hypothes.is/blog/showing-orphaned-annotations/
- [Kernighan] https://wolfram.schneider.org/bsd/7thEdManVol2/beginners/beginners.pdf
- [Rhodes] https://rhodesmill.org/brandon/2012/one-sentence-per-line/
- [SemBr] https://sembr.org/
- [Asciidoctor] https://github.com/asciidoctor/asciidoctor.org/blob/main/docs/asciidoc-recommended-practices.adoc

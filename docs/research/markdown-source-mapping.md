# Mapping rendered Markdown back to source lines

Research for [#134](https://github.com/probertson/diff-by-numbers/issues/134), part of the
document-review map [#131](https://github.com/probertson/diff-by-numbers/issues/131).
Researched 2026-09-29 against goldmark v1.8.6 (and v2.1.6), glamour v1.0.0, and
gomarkdown `94fc73f6b1a3`.

**Question.** If a Step shows Markdown formatted, in the terminal or as HTML, can each
rendered block (paragraph, heading, list item, table row, code block) be traced back to the
source lines it came from? That would keep the Coverage Ledger exact and let an Anchor still
name real lines.

## Short answer

- **At block granularity, yes, reliably.** Since v1.8.0 (March 2026) goldmark gives every
  node a start offset (`Pos()`), and every leaf block's `Lines()` holds the byte segment of
  each source line it consumed. A block's source line range is the line of `Pos()` through
  the last line of any segment in its subtree. The only non-blank lines no node claims are
  pure syntax lines: the setext underline, the closing code fence and the table delimiter
  row. They always sit between a block's last content line and the next block, so a simple
  rule attributes them to the block above. goldmark has no end positions.
- **Inside a block, a line-exact mapping is available but not through glamour.** Inline
  `Text` nodes carry byte segments, so which source line each word came from is known
  exactly. glamour discards that when it word-wraps: by default it joins a paragraph's
  source lines with spaces and reflows them to the terminal width. dbn can keep line-exact
  rows only if it draws the rows itself from the goldmark AST.
- **HTML maps at block granularity with a ~30-line AST transformer**, the same technique as
  cmark's `data-sourcepos` and VS Code's preview. Code blocks and raw HTML blocks need extra
  handling because goldmark's HTML renderer emits no attributes on them.
- **Some constructs genuinely break a rendered-row-to-source-line mapping**, whatever the
  library. A link reference definition renders nothing, yet it changes how a link on another
  line renders. Raw HTML blocks are stripped or passed through opaquely. glamour also adds
  rows with no source of their own (link footnotes under tables).
- **An Excerpt's line range cannot be rendered on its own.** Parsed in isolation, a slice
  that cuts a block changes meaning: a closing fence opens a new code block, and table rows
  without their header become code. Rendering has to parse the whole file and then select
  the blocks that intersect the Excerpt.

## What goldmark exposes

goldmark's README lists "AST-based; preserves source position of nodes" among its features
[G1]. The concrete API:

- **`Node.Pos() int`**: "a position of this node in a source. If this node position is not
  defined, Pos returns -1" [G2]. Added to all nodes, inline ones included, in v1.8.0, with
  fixes for block and setext positions in v1.8.1 and v1.8.2 [G3]. It is a **byte offset
  into the source**. dbn converts it to a line by counting newlines. It is a start position
  only: goldmark has no end offset, and neither does the v2 line (v2.1.6's `ast` package also
  has only `Pos()`) [G4].
- **Block positions** are set when a block parser opens a node, at the line start plus the
  block's indent offset [G5]. `Paragraph`, `TextBlock` and `LinkReferenceDefinition`
  instead compute `Pos()` from their first line segment [G2].
- **`Node.Lines() *text.Segments`**, valid for block nodes, holds "text segments that hold
  positions in a source" [G2]. Each `text.Segment` is a `[Start, Stop)` byte range plus a
  `Padding` count of virtual spaces used for tab expansion [G6]. Container blocks
  (`Blockquote`, `List`, `ListItem`, `Table`, `TableRow`) have empty `Lines()` and only a
  `Pos()`, so their extent comes from their descendants.
- **Inline nodes**: `Text` has a `Segment` byte range and soft/hard line-break flags.
  `Emphasis`, `Link`, `CodeSpan` and the rest have a start `Pos()` [G7][G8]. An inline
  node's extent is the union of its descendant `Text` segments.
- **GFM tables** (extension): `Table`, `TableRow` and `TableCell` each get a `Pos()`, and
  every cell has a `Lines()` segment for its content [G9]. One source row maps to one
  `TableRow`.
- **Link reference definitions** have been a real AST node, `LinkReferenceDefinition`, since
  v1.8.0, with `Lines()` covering every line of the definition [G3][G10]. Before v1.8.0 they
  were consumed silently and left no trace in the tree.

### Probe results

A throwaway program parsed an edge-case document with goldmark v1.8.6 + GFM. It printed
every node's start line and `Lines()`, then listed the source lines no node claimed.
Condensed:

| Source | Node | Start (`Pos`) | Lines claimed |
|---|---|---|---|
| L1 `# ATX heading` | Heading | L1 | L1 |
| L3–L5 setext heading (two text lines + `===`) | Heading | L3 | L3, L4 (**not** the L5 underline) |
| L7–L9 paragraph with a reference link and emphasis across a line break | Paragraph | L7 | L7, L8, L9. `Emphasis` starts L8; its `Text` children are on L8 and L9 |
| L11 `> quote`, L12 lazy continuation (no `>`) | Blockquote > Paragraph | L11 | L11, L12 (the lazy line is claimed correctly) |
| L14–L19 list, loose second item, nested list | List > ListItem > Paragraph / TextBlock | L14, L16, L19 | every content line, including the indented continuation L15 |
| L21–L23 fenced code block | FencedCodeBlock | L21 (the `Info` segment is on L21) | L22 only (**not** the L23 closing fence) |
| L25 indented code | CodeBlock | L25 | L25 |
| L27–L29 `<div>…</div>` | HTMLBlock | L27 | L27–L29 |
| L31–L34 table | Table > TableHeader / TableRow > TableCell | L31, L33, L34 | L31, L33, L34 (**not** the L32 delimiter row) |
| L36 `---` | ThematicBreak | L36 | none (a `Pos` only) |
| L38–L39 `[ref]: url` + title on the next line | LinkReferenceDefinition | L38 | L38, L39 |

Unclaimed lines were all blank except three syntax lines: **L5** (setext underline), **L23**
(closing fence) and **L32** (table delimiter row). A second probe with leading tabs (list
item, paragraph in item, blockquote) put every block on its correct line. goldmark v2.1.5's
fix for "wrong block node position with leading tabs" [G4] looks column-level. It did not
change line numbers in this probe.

**The extent rule this supports.** A top-level block covers the lines from `line(Pos())` to
`max(last line of any Lines() segment or HTMLBlock.ClosureLine in its subtree)`. It then
extends down through any non-blank lines before the next sibling's start. That extension is
what picks up the underline, the closing fence and the delimiter row. The rule matters
because those lines can be Changed Lines. `===` → `---` changes a heading's level, and
editing a delimiter row changes a column's alignment. For nested blocks, apply the same rule
within the parent.

## Where mapping breaks

Each case the ticket named, plus others the probes found:

- **Setext headings**: the text lines map. The underline is not in `Lines()` (goldmark
  replaces the heading's lines with the preceding paragraph's lines when closing it [G11]).
  The extent rule recovers it, as syntax that renders as the heading's level, not as a row.
- **Lazy continuation lines**: they map fine. goldmark includes the lazy line in the
  paragraph's `Lines()` (probe L12). CommonMark defines them as continuation text of the open
  paragraph [S1]. The only oddity is visual: rendered, the line sits inside the quote bar,
  though its source line has no `>`.
- **Reference links**: the `Link` node is on its own line and carries a `Reference` with the
  label and link type [G8]. The destination and title come from the `LinkReferenceDefinition`,
  which can sit anywhere in the document ("before or after the links that use them" [S2]).
  The break runs both ways:
  1. The definition's lines are a block that **renders nothing** (0 rows in glamour, no
     element in HTML). A change to a definition line has nothing of its own to show.
  2. That change still alters how *other* blocks render. `Link.Reference` holds the label,
     not a pointer to the definition node. Linking the two means normalizing labels the way
     CommonMark matches them [S2].
- **Tables**: the mapping is clean at row level: one `TableRow` per source row, and one cell
  segment per cell. The delimiter row renders as a rule or as alignment, not as a row of its
  own. glamour adds more than the source holds (next section).
- **HTML blocks**: `Lines()` covers every line. But glamour runs the text through
  bluemonday's `StrictPolicy` and keeps only the text [L6][L7]: the `<div>` and `</div>`
  lines produce no row. goldmark's HTML renderer replaces raw HTML with
  `<!-- raw HTML omitted -->` unless `WithUnsafe()` is set [G12]. Even when passed through,
  a raw HTML block gets no attributes from the renderer (below).
- **Fenced code**: the body maps line for line. The opening fence's `Info` string is on the
  `Pos()` line, and the closing fence is recovered only by the extent rule.
- **Inline constructs spanning lines**: emphasis, links and code spans can open on one source
  line and close on the next (probe: `*emphasis` L8 … `across lines*` L9). Mapping rendered
  text to lines therefore has to work at `Text`-segment granularity, not per inline element.
- **Excerpt slices**: parsing only an Excerpt's lines changes the parse. In the probe, the
  slice `fmt.Println("hi")` / `` ``` `` / two table body rows rendered as a paragraph followed
  by a code block holding the table rows. The closing fence opened a new fence, and the rows
  had no header or delimiter to make them a table [S3][S4]. A missing definition also turns
  `[x][ref]` into literal text.
- **Front matter** (not in the ticket): goldmark core has no front matter support. A leading
  `---` block parses as a thematic break or a setext heading. Handling it needs an extension
  (such as `goldmark-meta`) or stripping those lines, which then render nothing, like a
  link reference definition.

## Terminal rendering: glamour

glamour v1.0.0 renders the whole document with its own goldmark instance (GFM +
`DefinitionList`, auto heading IDs) and a custom `ANSIRenderer` [L1]. Its public API takes
bytes and returns one styled string (`Render`, `RenderBytes`), with **no position
information and no per-node hook** [L1].

How the output loses the mapping:

- **Nested block buffers.** Each block renders into a buffer that its parent later
  post-processes with margins, indentation and word-wrap before flushing [L2][L3].
- **Paragraph reflow.** A `Text` with a soft line break writes `"\n"` [L4]. The paragraph
  then passes through `muesli/reflow/wordwrap` with `KeepNewlines` set from the
  `PreserveNewLines` option, false by default [L5]. With it false, reflow replaces every
  newline with a space and rewraps to the width [R1]. In the probe, the three-line paragraph
  still rendered as three rows, but with the line breaks in different places.
- **Children of inline elements render as one token.** Children of `Link`, `Emphasis`,
  `CodeSpan` and table cells are rendered by their parent (`isChild`) [L2], so the
  per-segment boundaries inside them never reach the output.
- **Injected content.** Links render as text plus URL. With `InlineTableLinks` off, links
  inside a table get footnote markers, and a footnote list prints below the table [L8]. In
  the probe that list line (`[1]: x https://example.com`) had no source line of its own; it
  came from L34 and L38. Tables are drawn by `lipgloss/table`, which pads and wraps cells, so
  one source row can take several terminal rows [L8].
- **`WithPreservedNewLines()`** keeps source line breaks as row breaks (probe: each paragraph
  source line started a new row). Width wraps are still added on top, and a wrap looks the
  same as a source break in the output, so recovering "row N came from line M" would still
  be inference from text.

**What does work:** glamour's `ansi.NewRenderer` is exported, and goldmark's
`renderer.Render(w, source, node)` accepts any document. So dbn can parse once with goldmark,
move each top-level block into a fresh `ast.Document`, and render the blocks one at a time.
In the probe this produced the same block contents as a whole-document render, except the
blank-line spacing between blocks. Every rendered chunk can then be tagged with its block's
source line range, which gives a **block-level** mapping with glamour's look. Reference links
still resolve, because goldmark resolves them at parse time. This is still output from an
external renderer, the cost ADR-0005 records: dbn knows which *block* a row belongs to, but
not which *line*.

## HTML rendering

goldmark's HTML renderer writes `data-*` attributes on every element whose renderer calls
`RenderAttributes`, whatever the per-element filter [G13]. An AST transformer
(`parser.WithASTTransformers`) that sets `data-sourcepos="start-end"` from the extent rule
produced this in the probe:

```html
<h1 data-sourcepos="3-4">Setext heading
spanning two lines</h1>
<p data-sourcepos="7-9">A paragraph with a <a href="https://example.com" …>reference link</a> and …</p>
<blockquote data-sourcepos="11-12"><p data-sourcepos="11-12">…</p></blockquote>
<li data-sourcepos="16-19"> … <p data-sourcepos="18-18">loose paragraph in item two</p> …
<tr data-sourcepos="34-34"><td data-sourcepos="34-34">a2 <a …>x</a></td>…
<pre><code class="language-go">fmt.Println(&quot;hi&quot;)
</code></pre>
<!-- raw HTML omitted -->
```

- Attributes were emitted on headings, paragraphs, blockquotes, lists, list items, thematic
  breaks, tables, rows and cells, and on inline links, emphasis, code spans and images [G13].
- **They were not emitted on** `CodeBlock`, `FencedCodeBlock`, `HTMLBlock` or `TextBlock`
  (the text of a tight list item, whose `<li>` carries the range instead). Covering code
  blocks means registering a replacement `NodeRenderer` for those kinds at higher priority,
  or wrapping them. VS Code hit the same gap for raw HTML and inserts a marker `<div>` before
  each HTML block [P3].
- **Inside a block**, the HTML keeps soft breaks as `\n` in text, but the browser reflows
  them. Line-level (or phrase-level) mapping inside a paragraph would need a custom `Text`
  renderer that wraps each source line's segments in an element. That is possible, because
  goldmark knows every `Text` segment, but inline elements that span lines (`<em>` across
  L8–L9) mean the wrappers must nest inside them.

## Other Go parsers

- **gomarkdown/markdown**: `ast.Leaf` and `ast.Container` have no position fields, only
  `Literal`/`Content` bytes and attributes [O1]. No source mapping is possible without a
  fork.
- **blackfriday** (gomarkdown's ancestor, unmaintained): same model, no positions [O2].
- **goldmark v2** (v2.0.0 July 2026, v2.1.6 current): the same `Pos()`-only model, with
  simplified `Parse`/`Render` signatures [G4]. glamour v1.0.0 depends on goldmark v1.7.x,
  which has no `Pos()` on inline nodes and no `LinkReferenceDefinition`, so dbn would pull in
  goldmark ≥ v1.8.0 itself (Go's minimum version selection would then raise glamour's copy
  as well) [L9][G3].

goldmark is the only maintained Go parser with source positions, and it is the one glamour
already uses.

## Precedent

The mature parsers in other languages settle on the same granularity:

- **cmark** (the CommonMark reference implementation) has start *and* end line and column for
  every node, and `CMARK_OPT_SOURCEPOS` writes `data-sourcepos="l:c-l:c"` on **block
  elements** [P1].
- **markdown-it** gives each block token `map: [line_begin, line_end]` [P2]. Inline tokens
  have none.
- **VS Code's Markdown preview** uses markdown-it's `map[0]` to put `data-line` on every
  block token for editor and preview scroll sync. It adds a marker element for HTML blocks,
  whose renderer ignores attributes [P3].

None of them maps rendered rows or words back to source lines below block level.
Block-level line ranges are the industry-standard ceiling for "render, then map back"; finer
mapping belongs to renderers that own the layout.

## Implications for dbn

These follow from the evidence. They are not decisions; the display and anchoring tickets
under #131 make those.

1. **The mapping can be exact only if dbn owns the rows.** This is ADR-0005's constraint
   again: an external renderer returns styled text, and dbn cannot map its rows back to
   lines. goldmark fits the ADR's amendment, which permits borrowing algorithms that return
   *ranges*: it returns an AST of byte ranges. glamour returns styled text.
2. **Three approaches the evidence supports, from least to most rendering:**
   - **Source-preserving styling** ("semi-formatted"): one row per source line, as today.
     goldmark's AST supplies rune ranges to style: heading lines bold, emphasis italic, the
     syntax markers (`#`, `*`, `` ` ``, `>`, fence lines, the delimiter row) dimmed. Coverage
     and Anchors are untouched, because rows *are* source lines. It fits the existing
     `LineWire.Emphasis` rune-range seam [D1]. No construct breaks it; a link reference
     definition just shows as its own line.
   - **dbn-drawn reflow**: dbn lays out each block itself, wrapping `Text` segments to the
     width. Every rendered row then knows the exact source lines (and bytes) it holds, and a
     row can hold parts of two source lines. Coverage stays exact. An Anchor over rows names
     the union of their lines, or a byte range for phrase-level Comments (direction 7). The
     cost: dbn reimplements the rendering of every construct it supports.
   - **glamour per block**: each top-level block rendered by glamour and tagged with its
     line range. Mapping is block-level only: coverage and Anchors widen to whole blocks.
     Its extra rows (table link footnotes) and dropped lines (HTML tags, definitions, fences)
     need special handling.
3. **Whatever the display, some Changed Lines have no rendered row**: link reference
   definitions, and front matter unless an extension handles it. Setext underlines, closing
   fences and delimiter rows are syntax, so rendering shows their effect but no row. A
   rendered view needs a fallback for these (show the source line, or attribute it to the
   block it affects), or they will be counted as covered without being seen.
4. **Render from the whole file, then select blocks.** The unit to render is a block that
   intersects the Excerpt, parsed in the context of the whole file (the new side, or the old
   side for a before-side). An Excerpt that ends mid-block has to be widened to the block
   boundary or shown as source.
5. **A modification has two ASTs.** Rendering the before-side and after-side of a changed
   block means parsing both file versions and aligning blocks between them. That belongs to
   the prose-diffing direction (#131 direction 4), not to source mapping.

## Reproducing

The probes lived in a scratch module outside the repo: goldmark v1.8.6, glamour v1.0.0 and
an edge-case document covering every construct above. `probe` walked the AST and printed
`Pos()`, `Lines()` and the unclaimed lines. `glam` rendered each top-level block alone
through `ansi.NewRenderer` and compared the result with a whole-document render. `html` added
a `data-sourcepos` AST transformer. None of it is committed; the tables and snippets above
are its output.

## Sources

goldmark (github.com/yuin/goldmark, tag v1.8.6 unless noted):

- [G1] README, feature list: <https://github.com/yuin/goldmark/blob/v1.8.6/README.md>
- [G2] `ast/ast.go` (`Node.Pos`, `Lines`, `pos`), `ast/block.go` (`Paragraph.Pos`, `TextBlock.Pos`, `LinkReferenceDefinition.Pos`):
  <https://github.com/yuin/goldmark/blob/v1.8.6/ast/ast.go#L56-L159>,
  <https://github.com/yuin/goldmark/blob/v1.8.6/ast/block.go>
- [G3] Release notes v1.8.0 ("add position information to all nodes … link reference
  definition nodes are represented as a new node type"), v1.8.1 ("fix: block positions"),
  v1.8.2 ("fix: setext headings positions"): <https://github.com/yuin/goldmark/releases/tag/v1.8.0>
- [G4] goldmark v2 release notes (v2.0.0-beta.3, v2.1.5 "wrong block node position with
  leading tabs") and `ast` package at v2.1.6: <https://github.com/yuin/goldmark/releases>
- [G5] `parser/parser.go` block opening, `node.SetPos(blockPos.Start + max(pc.BlockOffset(), 0))`:
  <https://github.com/yuin/goldmark/blob/v1.8.6/parser/parser.go#L993-L997>
- [G6] `text/segment.go` (`Segment{Start, Stop, Padding, ForceNewline}`):
  <https://github.com/yuin/goldmark/blob/v1.8.6/text/segment.go#L12-L36>
- [G7] `parser/parser.go` inline position assignment:
  <https://github.com/yuin/goldmark/blob/v1.8.6/parser/parser.go#L1224-L1229>
- [G8] `ast/inline.go` (`Link.Reference *ReferenceLink`, `ReferenceLink{Type, Value}`):
  <https://github.com/yuin/goldmark/blob/v1.8.6/ast/inline.go#L394-L444>
- [G9] `extension/table.go` (table, row and cell `SetPos`):
  <https://github.com/yuin/goldmark/blob/v1.8.6/extension/table.go#L160-L222>
- [G10] `parser/link_ref.go` (definitions inserted as nodes):
  <https://github.com/yuin/goldmark/blob/v1.8.6/parser/link_ref.go#L16-L58>
- [G11] `parser/setext_headings.go` `Close` (heading takes the paragraph's lines):
  <https://github.com/yuin/goldmark/blob/v1.8.6/parser/setext_headings.go#L78-L104>
- [G12] README, `html.WithUnsafe` option: <https://github.com/yuin/goldmark/blob/v1.8.6/README.md>
- [G13] `renderer/html/html.go` `RenderAttributes` (`data-` prefix always passes) and the
  per-node renderers that call it:
  <https://github.com/yuin/goldmark/blob/v1.8.6/renderer/html/html.go#L726-L751>

glamour (github.com/charmbracelet/glamour, tag v1.0.0) and reflow:

- [L1] `glamour.go` `NewTermRenderer`, `Render`, `RenderBytes`:
  <https://github.com/charmbracelet/glamour/blob/v1.0.0/glamour.go#L70-L100>
- [L2] `ansi/renderer.go` `renderNode`, `isChild`:
  <https://github.com/charmbracelet/glamour/blob/v1.0.0/ansi/renderer.go#L93-L152>
- [L3] `ansi/blockelement.go` `Finish` (wordwrap and margins applied to the child buffer):
  <https://github.com/charmbracelet/glamour/blob/v1.0.0/ansi/blockelement.go#L32-L66>
- [L4] `ansi/elements.go` `Text` element (soft break → `"\n"`):
  <https://github.com/charmbracelet/glamour/blob/v1.0.0/ansi/elements.go#L170-L185>
- [L5] `ansi/paragraph.go` `Finish` (`flow.KeepNewlines = ctx.options.PreserveNewLines`):
  <https://github.com/charmbracelet/glamour/blob/v1.0.0/ansi/paragraph.go#L37-L60>
- [L6] `ansi/elements.go` `HTMLBlock` / `RawHTML` via `SanitizeHTML`:
  <https://github.com/charmbracelet/glamour/blob/v1.0.0/ansi/elements.go#L413-L428>
- [L7] `ansi/context.go` (`bluemonday.StrictPolicy()` stripper):
  <https://github.com/charmbracelet/glamour/blob/v1.0.0/ansi/context.go#L17-L38>
- [L8] `ansi/table.go` (`lipgloss/table`, `Finish` → `printTableLinks`) and `ansi/table_links.go`:
  <https://github.com/charmbracelet/glamour/blob/v1.0.0/ansi/table.go#L39-L141>
- [L9] `go.mod` (goldmark requirement): <https://github.com/charmbracelet/glamour/blob/v1.0.0/go.mod>
- [R1] muesli/reflow v0.3.0 `wordwrap/wordwrap.go` (`if !w.KeepNewlines { … Replace(…, "\n", " ") }`):
  <https://github.com/muesli/reflow/blob/v0.3.0/wordwrap/wordwrap.go#L95-L99>

Other parsers:

- [O1] gomarkdown `ast/node.go` (`Leaf`, `Container`):
  <https://github.com/gomarkdown/markdown/blob/master/ast/node.go>
- [O2] blackfriday v2 `node.go` (`Node` has type, links, `Literal` and per-kind data, no
  positions): <https://github.com/russross/blackfriday/blob/v2/node.go>

Specifications:

- [S1] CommonMark 0.31.2, paragraph continuation text / lazy continuation:
  <https://spec.commonmark.org/0.31.2/#paragraph-continuation-text>
- [S2] CommonMark 0.31.2, link reference definitions and label matching:
  <https://spec.commonmark.org/0.31.2/#link-reference-definitions>
- [S3] CommonMark 0.31.2, fenced code blocks: <https://spec.commonmark.org/0.31.2/#fenced-code-blocks>
- [S4] GFM spec, tables extension (header row and delimiter row required):
  <https://github.github.com/gfm/#tables-extension->

Precedent:

- [P1] cmark `src/cmark.h` (`cmark_node_get_end_line`, `CMARK_OPT_SOURCEPOS` "on all block
  elements") and `src/html.c` `S_render_sourcepos`:
  <https://github.com/commonmark/cmark/blob/master/src/cmark.h>,
  <https://github.com/commonmark/cmark/blob/master/src/html.c>
- [P2] markdown-it `src/token.ts` (`map: [line_begin, line_end]`):
  <https://github.com/markdown-it/markdown-it/blob/master/src/token.ts>
- [P3] VS Code `extensions/markdown-language-features/src/markdownEngine.ts`
  (`pluginSourceMap`, `data-line`, `html_block` marker):
  <https://github.com/microsoft/vscode/blob/main/extensions/markdown-language-features/src/markdownEngine.ts>

dbn:

- [D1] `internal/daemon/view.go` `LineWire.Emphasis` (rune ranges the TUI only styles), and
  [ADR-0005](../adr/0005-own-the-renderer-in-mvp.md).

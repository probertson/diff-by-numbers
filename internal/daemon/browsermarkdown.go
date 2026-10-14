package daemon

import (
	"bytes"
	"html/template"
	"sort"
	"strconv"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/review"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// documentRow is one source row of a Document stretch the page renders: its
// text, the block it belongs to (0 for none) and that block's place on the
// page (placeOfBlock), whether a Comment covers it, and whether the TUI's
// cursor is on it.
type documentRow struct {
	text      string
	block     int
	place     string
	commented bool
	cursor    bool
	// treatment is how an edit changed the row's block: a new block is marked
	// as the TUI's gutter marks it.
	treatment string
	// widened marks a row dbn widened the Excerpt by, so that no block is
	// read in part: drawn dimmed, as context.
	widened bool
	// tracked is, for a row of an edited or removed block, how the page draws
	// it as track changes; nil for a row rendered as Markdown.
	tracked *trackedLine
	// diagram marks a row of a Mermaid diagram the stretch shows whole, which
	// the page draws as a picture (#191).
	diagram bool
	// change is, for a row of an edited or removed Mermaid diagram the stretch
	// shows whole, the diagram's source before and after, which the page draws
	// as pictures above the edit (#193); nil for any other row.
	change *diagramChange
}

// classes is the class attribute a block's element carries for its first
// row, or "" for none.
func (r documentRow) classes() string {
	var names []string
	if r.tracked != nil {
		names = append(names, "tracked")
	}
	if r.commented {
		names = append(names, "commented")
	}
	if r.cursor {
		names = append(names, "cursor")
	}
	return strings.Join(names, " ")
}

// renderDocument renders a stretch of a Markdown Document as HTML with the
// parse dbn reads Documents with (CommonMark plus GFM tables), so tables are
// tables. Raw HTML is shown as text, never passed through. An image is drawn
// from where images finds it, or as its alt text with why it is not shown.
//
// Each element that starts a block carries the block's number as data-block,
// and its place as data-place, where the page finds the block the TUI's cursor
// is on (#187). A block a
// Comment covers carries the class "commented", and the cursor's "cursor". A
// new, edited or removed block carries its treatment as data-treatment; an
// edited or removed one is drawn as track changes, an edited table row as a
// row of its table.
func renderDocument(rows []documentRow, images documentImagesAt) template.HTML {
	if html, ok := renderStretch(rows, images, true); ok {
		return html
	}
	// An edited table row the parse did not find in a table (one an Excerpt
	// shows without its header) is drawn on its own instead.
	html, _ := renderStretch(rows, images, false)
	return html
}

// renderStretch renders a stretch as Markdown, with each edited or removed
// block in it drawn as track changes. With tablesInline, an edited table row
// stays in the Markdown, so its table stays one table with the row inside; it
// reports false if any such row was not parsed as a table row.
func renderStretch(rows []documentRow, images documentImagesAt, tablesInline bool) (template.HTML, bool) {
	inline := func(row documentRow) bool {
		return row.tracked == nil || (tablesInline && row.tracked.kind == string(review.BlockTableRow))
	}
	var out strings.Builder
	for len(rows) > 0 {
		if !inline(rows[0]) {
			n := blockLength(rows)
			out.WriteString(string(renderTracked(rows[:n], false)))
			rows = rows[n:]
			continue
		}
		n := 1
		for n < len(rows) && inline(rows[n]) {
			n++
		}
		html, ok := renderMarkdown(rows[:n], images)
		if !ok {
			return "", false
		}
		out.WriteString(string(html))
		rows = rows[n:]
	}
	return template.HTML(out.String()), true // each part is safe HTML
}

// blockLength is how many rows from the first belong to its block.
func blockLength(rows []documentRow) int {
	n := 1
	for n < len(rows) && rows[n].block == rows[0].block {
		n++
	}
	return n
}

// renderMarkdown renders rows of a Document as Markdown, drawing an edited
// table row among them as track changes across a row of its table. It reports
// false if such a row was not parsed as a table row.
func renderMarkdown(rows []documentRow, images documentImagesAt) (template.HTML, bool) {
	var source strings.Builder
	starts := make([]int, 0, len(rows))
	tracked := map[int]template.HTML{}
	for i, row := range rows {
		starts = append(starts, source.Len())
		source.WriteString(row.text)
		source.WriteByte('\n')
		if _, seen := tracked[row.block]; row.tracked != nil && !seen {
			tracked[row.block] = trackedLines(rows[i:i+blockLength(rows[i:])], false)
		}
	}
	content := []byte(source.String())
	rowAt := func(offset int) documentRow {
		return rows[sort.Search(len(starts), func(i int) bool { return starts[i] > offset })-1]
	}

	// A parser is made for each render rather than shared, as the review core
	// does, so concurrent pages never share its state.
	draw := &drawnSafely{images: images, rowAt: rowAt, tracked: tracked, drawn: map[int]bool{}}
	md := goldmark.New(
		goldmark.WithExtensions(extension.NewTable(tableAlignment)),
		goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(draw, 100))),
	)
	document := md.Parser().Parse(text.NewReader(content))
	tagBlocks(document, rowAt)

	var out bytes.Buffer
	if err := md.Renderer().Render(&out, content, document); err != nil {
		// Rendering into memory does not fail; should it, the text is still
		// worth reading.
		return template.HTML("<pre>" + template.HTMLEscapeString(source.String()) + "</pre>"), true
	}
	// Raw HTML was escaped by drawnSafely.
	return template.HTML(out.String()), len(draw.drawn) == len(tracked)
}

// renderPlain renders a stretch of a Document that is prose but not Markdown:
// each block a paragraph of its lines, with no Markdown read into them.
func renderPlain(rows []documentRow) template.HTML {
	var out strings.Builder
	for i := 0; i < len(rows); {
		if rows[i].tracked != nil {
			n := blockLength(rows[i:])
			out.WriteString(string(renderTracked(rows[i:i+n], true)))
			i += n
			continue
		}
		j := i
		var words []string
		for ; j < len(rows) && rows[j].block == rows[i].block; j++ {
			line := template.HTMLEscapeString(strings.TrimSpace(rows[j].text))
			if rows[j].widened {
				line = `<span class="widened">` + line + "</span>"
			}
			words = append(words, line)
		}
		out.WriteString(`<p data-block="` + strconv.Itoa(rows[i].block) + `" data-place="` +
			template.HTMLEscapeString(rows[i].place) + `"`)
		if rows[i].treatment == string(review.TreatmentNew) {
			out.WriteString(` data-treatment="new"`)
		}
		if classes := rows[i].classes(); classes != "" {
			out.WriteString(` class="` + classes + `"`)
		}
		out.WriteString(">" + strings.Join(words, " ") + "</p>\n")
		i = j
	}
	return template.HTML(out.String())
}

// blockElements are the nodes that start a block of a Document as dbn
// divides it (internal/review blocks.go).
var blockElements = map[ast.NodeKind]bool{
	ast.KindParagraph:       true,
	ast.KindHeading:         true,
	ast.KindListItem:        true,
	ast.KindFencedCodeBlock: true,
	ast.KindCodeBlock:       true,
	ast.KindHTMLBlock:       true,
	east.KindTableRow:       true,
	east.KindTableHeader:    true,
}

// tagBlocks marks each element that starts a block with its number, its place,
// its treatment, and the classes its first row gives it (tracked, commented,
// cursor). An element inside one already marked with the same block is left
// alone, so a list item's paragraph is not marked a second time.
func tagBlocks(document ast.Node, rowAt func(offset int) documentRow) {
	var marked []ast.Node
	var blocks []int
	_ = ast.Walk(document, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			if len(marked) > 0 && marked[len(marked)-1] == n {
				marked, blocks = marked[:len(marked)-1], blocks[:len(blocks)-1]
			}
			return ast.WalkContinue, nil
		}
		if !blockElements[n.Kind()] {
			return ast.WalkContinue, nil
		}
		offset, ok := firstOffset(n)
		if !ok {
			return ast.WalkContinue, nil
		}
		row := rowAt(offset)
		if row.block == 0 || (len(blocks) > 0 && blocks[len(blocks)-1] == row.block) {
			return ast.WalkContinue, nil
		}
		n.SetAttributeString("data-block", strconv.Itoa(row.block))
		n.SetAttributeString("data-place", row.place)
		if row.treatment != "" && row.treatment != string(review.TreatmentContext) {
			n.SetAttributeString("data-treatment", row.treatment)
		}
		if classes := row.classes(); classes != "" {
			n.SetAttributeString("class", classes)
		}
		marked, blocks = append(marked, n), append(blocks, row.block)
		return ast.WalkContinue, nil
	})
}

// firstOffset is where a node's source starts: its first line, or for a node
// that keeps no lines of its own (a table row), its first text.
func firstOffset(n ast.Node) (int, bool) {
	if t, ok := n.(*ast.Text); ok {
		return t.Segment.Start, true
	}
	if n.Type() == ast.TypeBlock && n.Lines().Len() > 0 {
		return n.Lines().At(0).Start, true
	}
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if offset, ok := firstOffset(child); ok {
			return offset, true
		}
	}
	return 0, false
}

// drawnSafely overrides how goldmark draws what a Document could use to run
// or fetch something in the Reviewer's browser: raw HTML is shown as text, and
// an image is drawn only from the daemon, never from a remote address. It
// also draws a table row an edit changed as track changes (tracked, by
// block), noting each one it drew.
type drawnSafely struct {
	images  documentImagesAt
	rowAt   func(offset int) documentRow
	tracked map[int]template.HTML
	drawn   map[int]bool
}

func (d *drawnSafely) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindRawHTML, renderRawHTMLAsText)
	reg.Register(ast.KindHTMLBlock, renderHTMLBlockAsText)
	reg.Register(ast.KindImage, d.renderImage)
	reg.Register(ast.KindText, d.renderText)
	reg.Register(east.KindTableHeader, d.renderTableHeader)
	reg.Register(east.KindTableRow, d.renderTableRow)
	reg.Register(ast.KindFencedCodeBlock, d.renderFencedCode)
}

// htmlFuncs are goldmark's own renderers, for what is drawn as it would be.
var htmlFuncs = func() registered {
	funcs := registered{}
	html.NewRenderer().RegisterFuncs(funcs)
	return funcs
}()

// renderText draws text on a line dbn widened the Excerpt by dimmed, as
// context.
func (d *drawnSafely) renderText(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Text)
	if !entering || n.IsRaw() || !d.rowAt(n.Segment.Start).widened {
		return htmlFuncs[ast.KindText](w, source, node, entering)
	}
	_, _ = w.WriteString(`<span class="widened">`)
	html.DefaultWriter.Write(w, n.Segment.Value(source))
	_, _ = w.WriteString("</span>")
	switch {
	case n.HardLineBreak():
		_, _ = w.WriteString("<br>\n")
	case n.SoftLineBreak():
		_ = w.WriteByte('\n')
	}
	return ast.WalkContinue, nil
}

// tableAlignment draws a column's alignment as an align attribute rather
// than goldmark's inline style, which the page's policy refuses (pagePolicy).
var tableAlignment = extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute)

// tableFuncs are goldmark's own table renderers, for the rows no edit changed.
var tableFuncs = func() registered {
	funcs := registered{}
	extension.NewTableHTMLRenderer(tableAlignment).RegisterFuncs(funcs)
	return funcs
}()

type registered map[ast.NodeKind]renderer.NodeRendererFunc

func (r registered) Register(kind ast.NodeKind, f renderer.NodeRendererFunc) { r[kind] = f }

// trackedRowOf is the track changes to draw for a table row (or header) an
// edit changed, and its block, if it is one.
func (d *drawnSafely) trackedRowOf(n ast.Node) (template.HTML, int, bool) {
	offset, ok := firstOffset(n)
	if !ok {
		return "", 0, false
	}
	block := d.rowAt(offset).block
	lines, ok := d.tracked[block]
	return lines, block, ok
}

// writeTrackedRow draws a table row an edit changed as one <tr>: its source
// lines as track changes, across all of the table's columns.
func writeTrackedRow(w util.BufWriter, n ast.Node, attributes ast.Node, lines template.HTML) {
	columns := 1
	if table, ok := n.Parent().(*east.Table); ok {
		columns = max(len(table.Alignments), 1)
	}
	_, _ = w.WriteString("<tr")
	if attributes != nil {
		html.RenderAttributes(w, attributes, nil)
	}
	_, _ = w.WriteString(`><td class="source-row" colspan="` + strconv.Itoa(columns) + `">`)
	_, _ = w.WriteString(string(lines))
	_, _ = w.WriteString("</td></tr>\n")
}

func (d *drawnSafely) renderTableHeader(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	lines, block, ok := d.trackedRowOf(n)
	if !ok {
		return tableFuncs[east.KindTableHeader](w, source, n, entering)
	}
	if !entering {
		return ast.WalkContinue, nil
	}
	d.drawn[block] = true
	_, _ = w.WriteString("<thead")
	html.RenderAttributes(w, n, nil)
	_, _ = w.WriteString(">\n")
	writeTrackedRow(w, n, nil, lines)
	_, _ = w.WriteString("</thead>\n")
	if n.NextSibling() != nil {
		_, _ = w.WriteString("<tbody>\n")
	}
	return ast.WalkSkipChildren, nil
}

func (d *drawnSafely) renderTableRow(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	lines, block, ok := d.trackedRowOf(n)
	if !ok {
		return tableFuncs[east.KindTableRow](w, source, n, entering)
	}
	if !entering {
		return ast.WalkContinue, nil
	}
	d.drawn[block] = true
	writeTrackedRow(w, n, n, lines)
	if n.Parent().LastChild() == n {
		_, _ = w.WriteString("</tbody>\n")
	}
	return ast.WalkSkipChildren, nil
}

// renderFencedCode draws a fence dbn marked as a Mermaid diagram as a diagram
// container holding its source as authored, which the page's script has
// Mermaid draw (page.js); any other fence is drawn as goldmark draws it. The
// container carries the block's attributes, as the fence would have.
func (d *drawnSafely) renderFencedCode(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	offset, ok := firstOffset(node)
	if !ok || !d.rowAt(offset).diagram {
		return htmlFuncs[ast.KindFencedCodeBlock](w, source, node, entering)
	}
	if !entering {
		return ast.WalkContinue, nil
	}
	class := "diagram"
	if classes, ok := node.AttributeString("class"); ok {
		class += " " + classes.(string)
	}
	_, _ = w.WriteString(`<figure class="` + template.HTMLEscapeString(class) + `"`)
	html.RenderAttributes(w, node, onlyData) // the block's data attributes
	_, _ = w.WriteString(`><pre class="diagram-source"><code>`)
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		_, _ = w.Write(util.EscapeHTML(line.Value(source)))
	}
	_, _ = w.WriteString("</code></pre></figure>\n")
	return ast.WalkSkipChildren, nil
}

// onlyData lets html.RenderAttributes write a node's data attributes alone.
var onlyData = util.NewBytesFilter()

func renderRawHTMLAsText(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkSkipChildren, nil
	}
	segments := n.(*ast.RawHTML).Segments
	for i := 0; i < segments.Len(); i++ {
		segment := segments.At(i)
		_, _ = w.Write(util.EscapeHTML(segment.Value(source)))
	}
	return ast.WalkSkipChildren, nil
}

func renderHTMLBlockAsText(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	block := n.(*ast.HTMLBlock)
	_, _ = w.WriteString(`<pre class="raw-html"`)
	if block.Attributes() != nil {
		html.RenderAttributes(w, block, nil)
	}
	_ = w.WriteByte('>')
	lines := block.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		_, _ = w.Write(util.EscapeHTML(line.Value(source)))
	}
	if block.HasClosure() {
		_, _ = w.Write(util.EscapeHTML(block.ClosureLine.Value(source)))
	}
	_, _ = w.WriteString("</pre>\n")
	return ast.WalkContinue, nil
}

// renderImage draws an image the daemon serves as an image, and any other as
// its alt text with why it is not shown: the remote address it was never
// fetched from, or that it is missing or outside the repository.
func (d *drawnSafely) renderImage(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkSkipChildren, nil
	}
	image := n.(*ast.Image)
	alt := util.EscapeHTML([]byte(plainTextOf(image, source)))
	drawing := d.images.draw(string(image.Destination))
	if drawing.source != "" {
		_, _ = w.WriteString(`<img src="`)
		_, _ = w.Write(util.EscapeHTML([]byte(drawing.source)))
		_, _ = w.WriteString(`" alt="`)
		_, _ = w.Write(alt)
		_, _ = w.WriteString(`">`)
		return ast.WalkSkipChildren, nil
	}
	_, _ = w.WriteString(`<span class="image">[image: `)
	_, _ = w.Write(alt)
	_, _ = w.WriteString(" — ")
	_, _ = w.Write(util.EscapeHTML([]byte(drawing.why)))
	_, _ = w.WriteString("]</span>")
	return ast.WalkSkipChildren, nil
}

// plainTextOf is the text inside a node, its markup read away.
func plainTextOf(n ast.Node, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Text:
			b.Write(n.Segment.Value(source))
		case *ast.String:
			b.Write(n.Value)
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

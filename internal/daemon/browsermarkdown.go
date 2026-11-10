package daemon

import (
	"bytes"
	"html/template"
	"io"
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
	// diagram is, for a row of a Mermaid diagram, the diagram's whole source:
	// the page draws a new or unchanged one (#191), and an edited or removed
	// one "before" and "after" above its edit (#193); nil for any other row.
	// cutInto is the Excerpt showing only part of the diagram, which is drawn
	// whole all the same.
	diagram *DiagramSourcesWire
	cutInto bool
	// frontmatter is a row of the Document's front matter, which the page
	// draws as its fields (drawFrontMatter).
	frontmatter bool
	// notes are the Comments whose last row this is, drawn beneath the row's
	// block in the flow of the Document, as the TUI draws them.
	notes []commentData
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
	// A stretch whose edited or removed blocks the parse did not all find
	// where dbn put them (an edited table row an Excerpt shows without its
	// header, say) has each drawn on its own instead.
	html, _ := renderStretch(rows, images, false)
	return html
}

// renderStretch renders a stretch as Markdown, with each edited or removed
// block in it drawn as track changes. inPlace keeps every block in the one
// Markdown read, so the structure around an edited or removed block stays
// whole (the list it is an item of, the table it is a row of, the quote it is
// in); it reports false if any such block was not found where dbn put it.
// Without inPlace, each is drawn on its own between the Markdown either side.
func renderStretch(rows []documentRow, images documentImagesAt, inPlace bool) (template.HTML, bool) {
	inline := func(row documentRow) bool {
		return row.tracked == nil || inPlace
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

// renderMarkdown renders rows of a Document as Markdown, drawing each edited
// or removed block among them as track changes where the parse put it: a table
// row across a row of its table, a list item as an item of its list, anything
// else in its place. It reports false if such a block was not found there.
func renderMarkdown(rows []documentRow, images documentImagesAt) (template.HTML, bool) {
	var source strings.Builder
	starts := make([]int, 0, len(rows))
	tracked := map[int][]documentRow{}
	for i, row := range rows {
		starts = append(starts, source.Len())
		switch {
		case !standsIn(row):
			source.WriteString(row.text)
			source.WriteByte('\n')
		case i == 0 || rows[i-1].block != row.block:
			source.WriteString(standIn(row))
			source.WriteByte('\n')
		}
		if _, seen := tracked[row.block]; row.tracked != nil && !seen {
			tracked[row.block] = rows[i : i+blockLength(rows[i:])]
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
	marked := tagBlocks(document, rowAt)
	inPlace := drawInPlace(marked, rows, content)
	placeNotes(document, marked, rows)

	var out bytes.Buffer
	if err := md.Renderer().Render(&out, content, document); err != nil {
		// Rendering into memory does not fail; should it, the text is still
		// worth reading.
		return template.HTML("<pre>" + template.HTMLEscapeString(source.String()) + "</pre>"), true
	}
	// Raw HTML was escaped by drawnSafely.
	for _, row := range rows {
		if (row.tracked != nil || standsIn(row)) && !inPlace[row.block] && !draw.drawn[row.block] {
			return template.HTML(out.String()), false
		}
	}
	return template.HTML(out.String()), true
}

// standsIn reports whether a block is read into the Markdown as one line
// standing in for it, and drawn in that line's place: an edited or removed
// block other than a list item or a table row, whose own text its list or
// table is read from, a diagram an Excerpt cuts into, and front matter. Read
// as they stand,
// the part of a block an Excerpt shows could read as something else (part of
// a fenced block, without its fences, as prose), and an edited block's lines
// are drawn from the review's runs anyway.
func standsIn(row documentRow) bool {
	if (row.diagram != nil && row.cutInto) || (row.frontmatter && row.tracked == nil) {
		return true
	}
	return row.tracked != nil && row.tracked.kind != string(review.BlockListItem) && row.tracked.kind != string(review.BlockTableRow)
}

// standIn is the line a block is read as: its first row's indent and quote
// markers, so it falls where the block does, then its stand-in word.
func standIn(row documentRow) string {
	lead := len(row.text) - len(strings.TrimLeft(row.text, " \t>"))
	return row.text[:lead] + standInWord(row.block)
}

// standInWord names a block in the Markdown it stands in for.
func standInWord(block int) string { return "dbnstandin" + strconv.Itoa(block) }

// drawStandIn draws a block in the place of the line that stood in for it: an
// edited or removed one as track changes; new or unchanged front matter as
// its fields; a new or unchanged diagram an Excerpt cuts into as the whole
// diagram, as a fenced one shown whole is.
func drawStandIn(rows []documentRow) template.HTML {
	first := rows[0]
	switch {
	case first.tracked != nil:
		return drawTracked(rows, false)
	case first.frontmatter:
		return drawFrontMatter(rows)
	}
	var out strings.Builder
	writeDiagram(&out, first.classes(), func() {
		out.WriteString(` data-block="` + strconv.Itoa(first.block) + `" data-place="` + template.HTMLEscapeString(first.place) + `"`)
		if first.treatment != "" && first.treatment != string(review.TreatmentContext) {
			out.WriteString(` data-treatment="` + template.HTMLEscapeString(first.treatment) + `"`)
		}
	}, first.diagram.After)
	return template.HTML(out.String())
}

// linesOf is the text of a block element's own lines.
func linesOf(n ast.Node, source []byte) string {
	var b strings.Builder
	for i := 0; i < n.Lines().Len(); i++ {
		segment := n.Lines().At(i)
		b.Write(segment.Value(source))
	}
	return b.String()
}

// drawInPlace draws the blocks among rows that the Markdown does not draw as
// they are, where the parse put them, the elements tagBlocks marked for them
// (marked): an edited or removed list item stays an item of its list, its
// bullet and any list nested in it kept, with its own text drawn as track
// changes; a block read as a stand-in line (standsIn) is drawn in that line's
// place, and marked updated to it, so long as the line stood alone. An edited
// table row is left to its table (renderTableRow). It returns the blocks it
// drew.
func drawInPlace(marked map[int]ast.Node, rows []documentRow, source []byte) map[int]bool {
	drawn := map[int]bool{}
	for i := 0; i < len(rows); {
		n := blockLength(rows[i:])
		block := rows[i : i+n]
		i += n
		first := block[0]
		element, ok := marked[first.block]
		if !ok {
			continue
		}
		switch {
		case standsIn(first):
			// A stand-in line read into the block before it (a paragraph's
			// lazy continuation, say) is not where the block stands.
			if strings.TrimSpace(linesOf(element, source)) != standInWord(first.block) {
				continue
			}
			drawing := &drawnNode{html: drawStandIn(block)}
			element.Parent().ReplaceChild(element.Parent(), element, drawing)
			marked[first.block] = drawing
		case first.tracked != nil && element.Kind() == ast.KindListItem:
			replaceItemText(element, &drawnNode{html: trackedItemLines(block)})
			classes, _ := element.AttributeString("class")
			class, _ := classes.(string)
			element.SetAttributeString("class", strings.TrimSpace(class+" "+first.tracked.kind+" flow"))
		default:
			continue
		}
		drawn[first.block] = true
	}
	return drawn
}

// replaceItemText puts text in place of a list item's own text: what comes
// before any list nested in it, or any block of the Document's own in it.
func replaceItemText(item, text ast.Node) {
	for child := item.FirstChild(); child != nil && child.Kind() != ast.KindList; child = item.FirstChild() {
		if _, own := child.AttributeString("data-block"); own {
			break
		}
		item.RemoveChild(item, child)
	}
	if first := item.FirstChild(); first != nil {
		item.InsertBefore(item, first, text)
		return
	}
	item.AppendChild(item, text)
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
		out.WriteString(string(notesOf(rows[i:j])))
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
// cursor), and returns the element it marked for each block. An element inside
// one already marked with the same block is left alone, so a list item's
// paragraph is not marked a second time.
func tagBlocks(document ast.Node, rowAt func(offset int) documentRow) map[int]ast.Node {
	found := map[int]ast.Node{}
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
		if _, ok := found[row.block]; !ok {
			found[row.block] = n
		}
		return ast.WalkContinue, nil
	})
	return found
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
	tracked map[int][]documentRow
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
	reg.Register(ast.KindCodeBlock, renderCode)
	reg.Register(kindDrawn, renderDrawn)
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

// trackedRowOf is the rows of a table row (or header) an edit changed or
// removed, and its block, if it is one.
func (d *drawnSafely) trackedRowOf(n ast.Node) ([]documentRow, int, bool) {
	offset, ok := firstOffset(n)
	if !ok {
		return nil, 0, false
	}
	block := d.rowAt(offset).block
	rows, ok := d.tracked[block]
	return rows, block, ok
}

// writeTrackedRow draws a table row an edit changed or removed as one <tr>,
// cell by cell, each cell's words struck and added in that cell. A row whose
// cells the edit changed, which may then not be its table's columns, is drawn
// as its source lines instead, across all of the columns.
func writeTrackedRow(w util.BufWriter, n ast.Node, attributes ast.Node, rows []documentRow) {
	table, _ := n.Parent().(*east.Table)
	columns := 1
	if table != nil {
		columns = max(len(table.Alignments), 1)
	}
	_, _ = w.WriteString("<tr")
	if attributes != nil {
		html.RenderAttributes(w, attributes, nil)
	}
	_ = w.WriteByte('>')
	cells, ok := rowCells(rows, columns)
	if !ok || table == nil {
		_, _ = w.WriteString(`<td class="source-row" colspan="` + strconv.Itoa(columns) + `">`)
		_, _ = w.WriteString(string(trackedLines(rows, false)))
		_, _ = w.WriteString("</td></tr>\n")
		return
	}
	tag := "td"
	if n.Kind() == east.KindTableHeader {
		tag = "th"
	}
	for i, cell := range cells {
		_, _ = w.WriteString("<" + tag)
		if alignment := table.Alignments[i]; alignment != east.AlignNone {
			_, _ = w.WriteString(` align="` + alignment.String() + `"`)
		}
		_, _ = w.WriteString(">" + string(trackedCell(cell)) + "</" + tag + ">")
	}
	_, _ = w.WriteString("</tr>\n")
}

func (d *drawnSafely) renderTableHeader(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	rows, block, ok := d.trackedRowOf(n)
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
	writeTrackedRow(w, n, nil, rows)
	_, _ = w.WriteString("</thead>\n")
	if n.NextSibling() != nil {
		_, _ = w.WriteString("<tbody>\n")
	}
	return ast.WalkSkipChildren, nil
}

func (d *drawnSafely) renderTableRow(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	rows, block, ok := d.trackedRowOf(n)
	if !ok {
		return tableFuncs[east.KindTableRow](w, source, n, entering)
	}
	if !entering {
		return ast.WalkContinue, nil
	}
	d.drawn[block] = true
	writeTrackedRow(w, n, n, rows)
	if n.Parent().LastChild() == n {
		_, _ = w.WriteString("</tbody>\n")
	}
	return ast.WalkSkipChildren, nil
}

// renderFencedCode draws a fence dbn marked as a Mermaid diagram as a diagram
// container holding its source as dbn read it, which the page's script has
// Mermaid draw (page.js); any other fence is drawn as code (renderCode). The
// container carries the block's attributes, as the fence would have.
func (d *drawnSafely) renderFencedCode(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	offset, ok := firstOffset(node)
	if !ok || d.rowAt(offset).diagram == nil {
		return renderCode(w, source, node, entering)
	}
	if !entering {
		return ast.WalkContinue, nil
	}
	classes, _ := node.AttributeString("class")
	class, _ := classes.(string)
	writeDiagram(w, class, func() { html.RenderAttributes(w, node, onlyData) }, d.rowAt(offset).diagram.After)
	return ast.WalkSkipChildren, nil
}

// renderCode draws a code block, fenced or indented, as goldmark draws it, but
// with the block's attributes on its pre, which goldmark leaves off: without
// them the page finds no element at the block the TUI's cursor is on.
func renderCode(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		_, _ = w.WriteString("</code></pre>\n")
		return ast.WalkContinue, nil
	}
	_, _ = w.WriteString("<pre")
	html.RenderAttributes(w, node, nil)
	_, _ = w.WriteString("><code")
	if fence, ok := node.(*ast.FencedCodeBlock); ok {
		if language := fence.Language(source); language != nil {
			_, _ = w.WriteString(` class="language-`)
			html.DefaultWriter.Write(w, language)
			_ = w.WriteByte('"')
		}
	}
	_ = w.WriteByte('>')
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		html.DefaultWriter.RawWrite(w, line.Value(source))
	}
	return ast.WalkContinue, nil
}

// writeDiagram writes a diagram container: a figure of class "diagram" and
// classes, its attributes written by attributes, holding the diagram's source
// as authored, which the page's script has Mermaid draw (page.js).
func writeDiagram(w interface {
	io.Writer
	io.StringWriter
}, classes string, attributes func(), source string) {
	class := "diagram"
	if classes != "" {
		class += " " + classes
	}
	_, _ = w.WriteString(`<figure class="` + template.HTMLEscapeString(class) + `"`)
	attributes()
	_, _ = w.WriteString(`><pre class="diagram-source"><code>`)
	_, _ = w.Write(util.EscapeHTML([]byte(source)))
	_, _ = w.WriteString("</code></pre></figure>\n")
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

// notesOf draws the notes rows carry, in order, as the page draws a Comment's
// note.
func notesOf(rows []documentRow) template.HTML {
	var out bytes.Buffer
	for _, row := range rows {
		for _, note := range row.notes {
			// Drawing into memory, with a template parsed when the package
			// loaded, does not fail.
			_ = pageTemplates.ExecuteTemplate(&out, "comment", note)
		}
	}
	return template.HTML(out.String()) // the template escapes what it draws
}

// kindDrawn is a node of HTML dbn drew itself, put into the flow of the
// Document: a block's notes, or a block drawn as track changes.
var kindDrawn = ast.NewNodeKind("Drawn")

type drawnNode struct {
	ast.BaseBlock
	html template.HTML
}

func (n *drawnNode) Kind() ast.NodeKind { return kindDrawn }

func (n *drawnNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

func renderDrawn(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString(string(node.(*drawnNode).html))
	}
	return ast.WalkSkipChildren, nil
}

// placeNotes puts each block's notes after the element tagBlocks marked for it
// (marked): in a list item, under its own text and before any list nested in
// it, so the list stays one list; for a table row, which cannot hold them,
// after its table; otherwise after the element. A block with no element found
// has its notes at the end, so none is lost.
func placeNotes(document ast.Node, marked map[int]ast.Node, rows []documentRow) {
	afterTable := map[ast.Node]ast.Node{} // the last notes put after each table
	for i := 0; i < len(rows); {
		n := blockLength(rows[i:])
		html := notesOf(rows[i : i+n])
		element, ok := marked[rows[i].block]
		i += n
		if html == "" {
			continue
		}
		notes := &drawnNode{html: html}
		switch table := tableOf(element); {
		case !ok:
			document.AppendChild(document, notes)
		case element.Kind() == ast.KindListItem:
			underItemText(element, notes)
		case table != nil:
			after, seen := afterTable[table]
			if !seen {
				after = table
			}
			table.Parent().InsertAfter(table.Parent(), after, notes)
			afterTable[table] = notes
		default:
			element.Parent().InsertAfter(element.Parent(), element, notes)
		}
	}
}

// tableOf is the table an element is in, or nil.
func tableOf(element ast.Node) ast.Node {
	for n := element; n != nil; n = n.Parent() {
		if n.Kind() == east.KindTable {
			return n
		}
	}
	return nil
}

// underItemText puts notes in a list item, before any list nested in it.
func underItemText(item, notes ast.Node) {
	for child := item.FirstChild(); child != nil; child = child.NextSibling() {
		if child.Kind() == ast.KindList {
			item.InsertBefore(item, child, notes)
			return
		}
	}
	item.AppendChild(item, notes)
}

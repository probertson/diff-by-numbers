package daemon

import (
	"bytes"
	"html/template"
	"sort"
	"strconv"
	"strings"

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
// text, the block it belongs to (0 for none), whether a Comment covers it,
// and whether the TUI's cursor is on it.
type documentRow struct {
	text      string
	block     int
	commented bool
	cursor    bool
}

// classes is the class attribute a block's element carries for its first
// row, or "" for none.
func (r documentRow) classes() string {
	var names []string
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
// so the page can find the block the TUI's cursor is on (#187). A block a
// Comment covers carries the class "commented", and the cursor's "cursor".
func renderDocument(rows []documentRow, images documentImagesAt) template.HTML {
	var source strings.Builder
	starts := make([]int, 0, len(rows))
	for _, row := range rows {
		starts = append(starts, source.Len())
		source.WriteString(row.text)
		source.WriteByte('\n')
	}
	content := []byte(source.String())

	// A parser is made for each render rather than shared, as the review core
	// does, so concurrent pages never share its state.
	md := goldmark.New(
		goldmark.WithExtensions(extension.Table),
		goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(drawnSafely{images: images}, 100))),
	)
	document := md.Parser().Parse(text.NewReader(content))
	tagBlocks(document, func(offset int) documentRow {
		return rows[sort.Search(len(starts), func(i int) bool { return starts[i] > offset })-1]
	})

	var out bytes.Buffer
	if err := md.Renderer().Render(&out, content, document); err != nil {
		// Rendering into memory does not fail; should it, the text is still
		// worth reading.
		return template.HTML("<pre>" + template.HTMLEscapeString(source.String()) + "</pre>")
	}
	return template.HTML(out.String()) // raw HTML was escaped by drawnSafely
}

// renderPlain renders a stretch of a Document that is prose but not Markdown:
// each block a paragraph of its lines, with no Markdown read into them.
func renderPlain(rows []documentRow) template.HTML {
	var out strings.Builder
	for i := 0; i < len(rows); {
		j := i
		var words []string
		for ; j < len(rows) && rows[j].block == rows[i].block; j++ {
			words = append(words, strings.TrimSpace(rows[j].text))
		}
		out.WriteString(`<p data-block="` + strconv.Itoa(rows[i].block) + `"`)
		if classes := rows[i].classes(); classes != "" {
			out.WriteString(` class="` + classes + `"`)
		}
		out.WriteString(">" + template.HTMLEscapeString(strings.Join(words, " ")) + "</p>\n")
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

// tagBlocks marks each element that starts a block with its number, and the
// classes its first row gives it (commented, cursor). An element inside one already
// marked with the same block is left alone, so a list item's paragraph is
// not marked a second time.
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
// an image is drawn only from the daemon, never from a remote address.
type drawnSafely struct {
	images documentImagesAt
}

func (d drawnSafely) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindRawHTML, renderRawHTMLAsText)
	reg.Register(ast.KindHTMLBlock, renderHTMLBlockAsText)
	reg.Register(ast.KindImage, d.renderImage)
}

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
func (d drawnSafely) renderImage(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
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

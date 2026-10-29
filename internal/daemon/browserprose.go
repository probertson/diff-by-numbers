package daemon

import (
	"bytes"
	"html/template"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// renderProse renders what an agent writes around the code (a Step's
// explanation, an Agent Question, a Chapter's intro) as Markdown. The page is
// set in a proportional face, where Markdown's markers would stand out; the
// TUI shows the same text as written, as Markdown is read in a terminal. Each
// line break the agent wrote is kept, as the page kept them before. As in a
// Document, raw HTML is shown as text, never passed through, and an image is
// shown as its alt text, never fetched.
func renderProse(text string) template.HTML {
	return renderMarkdownProse(text, true)
}

// renderMarkdownProse renders prose as renderProse does, keeping each line
// break written (hardWraps) or joining lines into paragraphs, as Markdown
// itself does.
func renderMarkdownProse(text string, hardWraps bool) template.HTML {
	options := []renderer.Option{renderer.WithNodeRenderers(util.Prioritized(proseSafely{}, 100))}
	if hardWraps {
		options = append(options, html.WithHardWraps())
	}
	md := goldmark.New(
		goldmark.WithExtensions(extension.NewTable(tableAlignment)),
		goldmark.WithRendererOptions(options...),
	)
	var out bytes.Buffer
	if err := md.Convert([]byte(text), &out); err != nil {
		// Rendering into memory does not fail; should it, the text is still
		// worth reading.
		return template.HTML("<p>" + template.HTMLEscapeString(text) + "</p>")
	}
	// Raw HTML was escaped by proseSafely.
	return template.HTML(out.String())
}

// proseSafely overrides how goldmark draws what prose could use to run or
// fetch something in the Reviewer's browser.
type proseSafely struct{}

func (proseSafely) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindRawHTML, renderRawHTMLAsText)
	reg.Register(ast.KindHTMLBlock, renderHTMLBlockAsText)
	reg.Register(ast.KindImage, renderImageAsAltText)
}

// renderImageAsAltText draws an image as its alt text: prose belongs to no
// file, so there is nowhere in the review to draw it from.
func renderImageAsAltText(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkSkipChildren, nil
	}
	_, _ = w.WriteString(`<span class="image">[image: `)
	_, _ = w.Write(util.EscapeHTML([]byte(plainTextOf(n, source))))
	_, _ = w.WriteString("]</span>")
	return ast.WalkSkipChildren, nil
}

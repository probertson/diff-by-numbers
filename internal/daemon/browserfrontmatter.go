package daemon

import (
	"html/template"
	"regexp"
	"strconv"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// A Document's front matter is drawn on the page as its fields, not as the
// YAML it is written in: front matter often holds a Document's own prose (a
// summary, a description), written as Markdown. dbn reads only what it needs
// to draw: each top-level key, with a plain value, a multi-line value (| or
// >), or anything else (a nested map or list) shown as written.

// frontMatterField is one top-level field of a Document's front matter, or a
// line dbn does not read as one (key "").
type frontMatterField struct {
	key string
	// value is a plain value, unquoted.
	value string
	// lines are a multi-line value's lines (prose), or a value dbn does not
	// read (raw), as written.
	lines []string
	prose bool
	raw   bool
}

var (
	frontMatterKey   = regexp.MustCompile(`^([A-Za-z0-9_][A-Za-z0-9_.-]*):(?:[ \t]+(.*?))?[ \t]*$`)
	frontMatterBlock = regexp.MustCompile(`^[|>][-+0-9]*(?:[ \t]+#.*)?$`)
)

// frontMatterFields reads front matter's lines, its fences left out.
func frontMatterFields(lines []string) []frontMatterField {
	var fields []frontMatterField
	for _, line := range lines {
		last := len(fields) - 1
		if m := frontMatterKey.FindStringSubmatch(line); m != nil {
			field := frontMatterField{key: m[1]}
			switch value := m[2]; {
			case frontMatterBlock.MatchString(value):
				field.prose = true
			case value == "":
				field.raw = true
			default:
				field.value = unquoteFrontMatter(value)
			}
			fields = append(fields, field)
			continue
		}
		if last >= 0 && (fields[last].prose || fields[last].raw) && (strings.TrimSpace(line) == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			fields[last].lines = append(fields[last].lines, line)
			continue
		}
		if last >= 0 && fields[last].key == "" {
			fields[last].lines = append(fields[last].lines, line)
			continue
		}
		fields = append(fields, frontMatterField{raw: true, lines: []string{line}})
	}
	return fields
}

// unquoteFrontMatter is a plain YAML value as it reads: without the quotes
// around it, and with a double-quoted value's escapes read.
func unquoteFrontMatter(value string) string {
	switch {
	case len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"':
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
		return value[1 : len(value)-1]
	case len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'':
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	}
	return value
}

// dedented is lines with the indent they share taken off.
func dedented(lines []string) []string {
	indent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		n := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent < 0 || n < indent {
			indent = n
		}
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		if len(line) >= indent && indent > 0 {
			line = line[indent:]
		}
		out[i] = line
	}
	return out
}

// isFence reports whether a line fences front matter.
func isFence(line string) bool {
	trimmed := strings.TrimRight(line, " \t")
	return trimmed == "---" || trimmed == "..."
}

// drawFrontMatter draws front matter, its rows given in order, as its fields
// between its fences, muted: each key a label, a plain value as text, a
// multi-line value rendered as Markdown, and anything else as written. The
// block carries its number, place and treatment, as any block does.
func drawFrontMatter(rows []documentRow) template.HTML {
	first := rows[0]
	var lines []string
	for _, row := range rows {
		lines = append(lines, row.text)
	}
	opening, closing := "", ""
	if len(lines) > 0 && isFence(lines[0]) {
		opening, lines = lines[0], lines[1:]
	}
	if len(lines) > 0 && isFence(lines[len(lines)-1]) {
		closing, lines = lines[len(lines)-1], lines[:len(lines)-1]
	}

	var out strings.Builder
	out.WriteString(`<div class="` + strings.TrimSpace("frontmatter "+first.classes()) + `" data-block="` +
		strconv.Itoa(first.block) + `" data-place="` + template.HTMLEscapeString(first.place) + `"`)
	if first.treatment != "" && first.treatment != string(review.TreatmentContext) {
		out.WriteString(` data-treatment="` + template.HTMLEscapeString(first.treatment) + `"`)
	}
	out.WriteString(">")
	if opening != "" {
		out.WriteString(`<div class="fence">` + template.HTMLEscapeString(opening) + `</div>`)
	}
	out.WriteString("<dl>")
	for _, field := range frontMatterFields(lines) {
		out.WriteString("<dt>" + template.HTMLEscapeString(field.key) + "</dt>")
		switch {
		case field.prose:
			out.WriteString(`<dd class="prose">` + string(renderMarkdownProse(strings.Join(dedented(field.lines), "\n"), false)) + "</dd>")
		case field.raw:
			out.WriteString("<dd><pre>" + template.HTMLEscapeString(strings.Join(field.lines, "\n")) + "</pre></dd>")
		default:
			out.WriteString("<dd>" + template.HTMLEscapeString(field.value) + "</dd>")
		}
	}
	out.WriteString("</dl>")
	if closing != "" {
		out.WriteString(`<div class="fence">` + template.HTMLEscapeString(closing) + `</div>`)
	}
	out.WriteString("</div>\n")
	return template.HTML(out.String())
}

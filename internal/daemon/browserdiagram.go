package daemon

import (
	"bytes"
	"compress/gzip"
	"embed"
	"html/template"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// Mermaid diagrams on the Browser Renderer (#191). dbn never parses Mermaid:
// the review package marks a Document's diagrams, the page holds each one's
// source in a diagram container (renderFencedCode), and Mermaid's library,
// embedded here and served by the daemon, draws it in the browser.
//
// Mermaid draws by putting a <style> into the document it draws in, and style
// attributes on what it draws, which the page's policy refuses (pagePolicy).
// So it runs in a frame of its own, the diagrams frame, whose policy allows
// inline style and nothing else of the kind; it holds nothing but Mermaid and
// what Mermaid draws. The page's script hands the frame each diagram's source
// and shows the drawing it gets back as an image, whose own styles (the
// author's included) apply inside it without the page allowing any.

// mermaidVersion is the release of Mermaid embedded in dbn. Upgrading it is a
// deliberate change in a release (mermaid/NOTICE says how).
const mermaidVersion = "12.2.0"

// mermaidAssets are Mermaid's single-file browser bundle, gzipped, with its
// licence and a notice of where it came from.
//
//go:embed mermaid
var mermaidAssets embed.FS

var mermaidLibrary = func() []byte {
	content, err := mermaidAssets.ReadFile("mermaid/mermaid.min.js.gz")
	if err != nil {
		panic(err)
	}
	return content
}()

// diagramsPath is where the diagrams frame is served, and mermaidPath the
// library it loads. The library's path names its version, so a browser may
// keep it for good.
const (
	diagramsPath = "/browser/diagrams"
	mermaidPath  = "/browser/mermaid-" + mermaidVersion + ".min.js"
)

var diagramsFrame = template.Must(template.New("diagrams").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>dbn diagrams</title>
</head>
<body>
<script src="{{.}}"></script>
</body>
</html>
`))

// diagramsPolicy is the diagrams frame's Content-Security-Policy, for the
// frame as served from origin: it runs Mermaid's library, named by its whole
// URL, and no other script, even one dbn serves, and allows the inline style
// Mermaid draws with. It loads nothing else, and only dbn's own pages may
// frame it. ('self' would name the origin alone, so any script dbn serves; a
// source naming a path cannot be written without the origin.)
func diagramsPolicy(origin string) string {
	return "default-src 'none'; script-src " + origin + mermaidPath + "; style-src 'unsafe-inline'; " +
		"base-uri 'none'; form-action 'none'; frame-ancestors 'self'"
}

// requestHost is a Host header that can be written into a policy as it is: a
// name or address, and a port. A browser sends nothing else.
var requestHost = regexp.MustCompile(`^(?:[A-Za-z0-9.-]+|\[[0-9A-Fa-f:.]+\])(?::[0-9]+)?$`)

// originOf is the origin a request was sent to, as a policy names it; false
// when its Host cannot be written into one.
func originOf(r *http.Request) (string, bool) {
	if !requestHost.MatchString(r.Host) {
		return "", false
	}
	scheme := "http://"
	if r.TLS != nil {
		scheme = "https://"
	}
	return scheme + r.Host, true
}

// diagramRoutes adds the diagrams frame and Mermaid's library to the daemon's
// mux. Neither belongs to a review.
func (d *Daemon) diagramRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+diagramsPath, func(w http.ResponseWriter, r *http.Request) {
		origin, ok := originOf(r)
		if !ok {
			http.Error(w, "dbn cannot serve the diagrams frame to this host", http.StatusBadRequest)
			return
		}
		var out bytes.Buffer
		if err := diagramsFrame.Execute(&out, mermaidPath); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", diagramsPolicy(origin))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = out.WriteTo(w)
	})
	mux.HandleFunc("GET "+mermaidPath, serveMermaid)
}

// serveMermaid sends Mermaid's library as it is stored, gzipped, to a browser
// that accepts that, and decompressed to anything else.
func serveMermaid(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Vary", "Accept-Encoding")
	if acceptsGzip(r.Header.Get("Accept-Encoding")) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(mermaidLibrary)
		return
	}
	library, err := gzip.NewReader(bytes.NewReader(mermaidLibrary))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = io.Copy(w, library)
}

// acceptsGzip reports whether an Accept-Encoding header accepts gzip: names
// it without refusing it with a quality of zero. Every browser names it.
func acceptsGzip(header string) bool {
	for _, coding := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(coding, ";")
		if strings.TrimSpace(name) != "gzip" {
			continue
		}
		return quality(params) > 0
	}
	return false
}

// quality is a coding's quality value, read from its parameters: 1 when they
// give none, and 0 when it cannot be read.
func quality(params string) float64 {
	for _, param := range strings.Split(params, ";") {
		key, value, ok := strings.Cut(param, "=")
		if !ok || strings.TrimSpace(key) != "q" {
			continue
		}
		q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return 0
		}
		return q
	}
	return 1
}

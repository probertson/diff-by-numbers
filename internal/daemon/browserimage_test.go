package daemon_test

import (
	"bytes"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// pngBytes is an image's content: a PNG header (which makes git see a binary
// file) and a payload telling one image, or one version of it, from another.
func pngBytes(payload string) []byte {
	return append([]byte("\x89PNG\r\n\x1a\n\x00"), payload...)
}

// onADocumentWithImages posts a Round whose one Step shows a new Markdown
// Document, docs/story.md, whole, and moves onto it. unchanged files are
// committed on main and left alone; added ones are new on the feature branch,
// so they are in the Change Set (and Acknowledged, as binary files must be).
// Paths are relative to the repository root.
func onADocumentWithImages(t *testing.T, baseURL, document string, unchanged, added map[string][]byte) (reviewID, root string) {
	t.Helper()
	root = t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(file string, content []byte) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("README", []byte("base\n"))
	for file, content := range unchanged {
		write(file, content)
	}
	git("init", "-q", "-b", "main")
	git("add", ".")
	git("commit", "-qm", "initial")
	git("checkout", "-q", "-b", "feature")
	write("docs/story.md", []byte(document))
	var acknowledged []any
	for file, content := range added {
		write(file, content)
		acknowledged = append(acknowledged, file)
	}
	git("add", ".")

	step := map[string]any{
		"name": "The story", "explanation": "all of it",
		"excerpts": []any{map[string]any{
			"repository": root, "file": "docs/story.md", "side": "new",
			"first_line": 1, "last_line": strings.Count(document, "\n"),
		}},
	}
	if len(acknowledged) > 0 {
		step["acknowledgements"] = []any{map[string]any{
			"repository": root, "files": acknowledged, "reason": "the mockups",
		}}
	}
	posted := postRound(t, baseURL, map[string]any{
		"label":        "LABEL-the-review",
		"brief":        map[string]any{"goal": "write the story", "background": "none yet"},
		"repositories": []any{map[string]any{"root": root, "base": "main"}},
		"steps":        []any{step},
	})
	httpPost(t, reviewURL(baseURL, posted.ReviewID)+"/advance")
	return posted.ReviewID, root
}

var imageSource = regexp.MustCompile(`<img[^>]* src="([^"]*)"`)

// imagesOn is the address of each image the page draws, in order.
func imagesOn(out string) []string {
	var sources []string
	for _, match := range imageSource.FindAllStringSubmatch(out, -1) {
		sources = append(sources, html.UnescapeString(match[1]))
	}
	return sources
}

// fetchImage is what the daemon answers for an image the page draws.
func fetchImage(t *testing.T, baseURL, source string) (int, []byte) {
	t.Helper()
	response, err := http.Get(baseURL + source)
	if err != nil {
		t.Fatalf("GET %s: %v", source, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s: %v", source, err)
	}
	return response.StatusCode, body
}

// An image the Change Set adds is what's under review, so it comes from the
// Round Snapshot, however the file on disk has moved since.
func TestAnImageInTheChangeSetIsServedFromTheSnapshot(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	posted := pngBytes("as posted")
	reviewID, root := onADocumentWithImages(t, server.URL,
		"# Checkout\n\n![the new checkout](mock.png)\n",
		nil, map[string][]byte{"docs/mock.png": posted})
	if err := os.WriteFile(filepath.Join(root, "docs", "mock.png"), pngBytes("edited since"), 0o644); err != nil {
		t.Fatal(err)
	}

	sources := imagesOn(stop(t, server.URL, reviewID))
	if len(sources) != 1 {
		t.Fatalf("expected the page to draw one image, got %q", sources)
	}
	status, body := fetchImage(t, server.URL, sources[0])

	if status != http.StatusOK || !bytes.Equal(body, posted) {
		t.Errorf("expected the image as posted, got %d %q", status, body)
	}
}

// An image the Change Set doesn't touch isn't in the Snapshot's say, so it is
// read from disk beside the Document, as it stands now.
func TestAnUnchangedImageBesideTheDocumentIsServedFromDisk(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID, root := onADocumentWithImages(t, server.URL,
		"# Checkout\n\n![today's checkout](current.png)\n",
		map[string][]byte{"docs/current.png": pngBytes("committed")}, nil)
	onDisk := pngBytes("on disk now")
	if err := os.WriteFile(filepath.Join(root, "docs", "current.png"), onDisk, 0o644); err != nil {
		t.Fatal(err)
	}

	sources := imagesOn(stop(t, server.URL, reviewID))
	if len(sources) != 1 {
		t.Fatalf("expected the page to draw one image, got %q", sources)
	}
	status, body := fetchImage(t, server.URL, sources[0])

	if status != http.StatusOK || !bytes.Equal(body, onDisk) {
		t.Errorf("expected the image as it is on disk, got %d %q", status, body)
	}
}

// The page can't be used to read files outside the Document's repository,
// whether a path climbs out of it or a symlink inside it leads out.
func TestAnImagePathEscapingTheRepositoryIsRefused(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	// Beside the repository: every t.TempDir() of a test shares one parent.
	outside := filepath.Join(filepath.Dir(t.TempDir()), "secret.png")
	if err := os.WriteFile(outside, pngBytes("SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	reviewID, root := onADocumentWithImages(t, server.URL,
		"# Checkout\n\n![climbing out](../../secret.png)\n\n![through a link](linked.png)\n",
		nil, nil)
	if err := os.Symlink(outside, filepath.Join(root, "docs", "linked.png")); err != nil {
		t.Fatal(err)
	}

	out := html.UnescapeString(stop(t, server.URL, reviewID))

	if sources := imagesOn(out); len(sources) != 0 {
		t.Errorf("expected no image drawn from outside the repository, got %q", sources)
	}
	for _, want := range []string{
		"climbing out — image outside the repository: ../../secret.png",
		"through a link — image outside the repository: linked.png",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the page to say %q, got:\n%s", want, out)
		}
	}
	for _, asked := range []string{"..%2Fsecret.png", "docs/linked.png"} {
		status, body := fetchImage(t, server.URL,
			daemon.PagePath(reviewID)+"/image/"+asked+"?repository="+url.QueryEscape(root))

		if status != http.StatusNotFound || bytes.Contains(body, []byte("SECRET")) {
			t.Errorf("expected %s refused, got %d %q", asked, status, body)
		}
	}
}

// Only images are served: an HTML file served from the daemon's own address
// could run a script beside the Reviewer's endpoints.
func TestAFileThatIsNotAnImageIsNotServed(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID, root := onADocumentWithImages(t, server.URL,
		"# Checkout\n\n![the prototype](prototype.html)\n",
		map[string][]byte{"docs/prototype.html": []byte("<script>steal()</script>")}, nil)

	out := html.UnescapeString(stop(t, server.URL, reviewID))
	status, body := fetchImage(t, server.URL,
		daemon.PagePath(reviewID)+"/image/docs/prototype.html?repository="+url.QueryEscape(root))

	if sources := imagesOn(out); len(sources) != 0 {
		t.Errorf("expected no image drawn for a file that is not one, got %q", sources)
	}
	if want := "the prototype — not an image: prototype.html"; !strings.Contains(out, want) {
		t.Errorf("expected the page to say %q, got:\n%s", want, out)
	}
	if status != http.StatusNotFound || bytes.Contains(body, []byte("steal")) {
		t.Errorf("expected the file refused, got %d %q", status, body)
	}
}

// An image that can't be found is named, so the Reviewer can ask the agent
// about it instead of wondering.
func TestAMissingImageShowsItsAltTextAndThatItWasNotFound(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID, _ := onADocumentWithImages(t, server.URL,
		"# Checkout\n\n![the flow diagram](flow/missing.png)\n", nil, nil)

	out := html.UnescapeString(stop(t, server.URL, reviewID))

	if sources := imagesOn(out); len(sources) != 0 {
		t.Errorf("expected no image drawn for a missing one, got %q", sources)
	}
	if want := "the flow diagram — image not found: flow/missing.png"; !strings.Contains(out, want) {
		t.Errorf("expected the page to say %q, got:\n%s", want, out)
	}
}

// A remote image is never fetched, by the daemon or by the page: a Review
// stays local, and nobody learns what the Reviewer is reading.
func TestARemoteImageShowsItsAltTextAndURLAndIsNeverRequested(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	var requested atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requested.Add(1)
		_, _ = w.Write(pngBytes("remote"))
	}))
	defer remote.Close()
	reviewID, _ := onADocumentWithImages(t, server.URL,
		"# Checkout\n\n![the hosted logo]("+remote.URL+"/logo.png)\n", nil, nil)

	out := html.UnescapeString(stop(t, server.URL, reviewID))

	if sources := imagesOn(out); len(sources) != 0 {
		t.Errorf("expected no image drawn for a remote one, got %q", sources)
	}
	if want := "the hosted logo — " + remote.URL + "/logo.png"; !strings.Contains(out, want) {
		t.Errorf("expected the page to say %q, got:\n%s", want, out)
	}
	if got := requested.Load(); got != 0 {
		t.Errorf("expected the remote image never requested, got %d requests", got)
	}
}

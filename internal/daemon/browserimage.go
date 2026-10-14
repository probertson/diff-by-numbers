package daemon

import (
	"errors"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/git"
	"github.com/probertson/diff-by-numbers/internal/review"
)

// The images a Document refers to (#139). One in the Change Set is served from
// the Round Snapshot, so it matches what is under review; any other is read
// from disk relative to the Document, tracked or not. Nothing outside the
// Document's repository is ever read, and a remote image is never fetched: a
// Review stays local.

var (
	errImageOutside  = errors.New("outside the repository")
	errImageNotFound = errors.New("image not found")
	errNotAnImage    = errors.New("not an image")
)

// documentImages finds the images a review's Documents refer to.
type documentImages struct {
	reviewID string
	files    review.RoundFiles
}

// imagesOf reads, under the lock, where a review's files are read from.
func (d *Daemon) imagesOf(session *review.Session) documentImages {
	images := documentImages{reviewID: session.ReviewID()}
	_ = d.locked(func() error {
		images.files = session.RoundFiles()
		return nil
	})
	return images
}

// imageFile is an image found, by its path in its repository: in a Round
// Snapshot (tree), or at a path on disk (disk).
type imageFile struct {
	repository, file string
	tree, disk       string
}

func (f imageFile) read() ([]byte, error) {
	if f.disk != "" {
		return os.ReadFile(f.disk)
	}
	return git.ReadBlobContent(f.repository, f.tree, f.file)
}

// find locates an image by its path in a repository under review, refusing
// one that leads outside it, by ".." or through a symlink.
func (d documentImages) find(repository, file string) (imageFile, error) {
	if !d.files.Holds(repository) {
		return imageFile{}, errImageNotFound
	}
	file, ok := confined(file)
	if !ok {
		return imageFile{}, errImageOutside
	}
	if imageType(file) == "" {
		return imageFile{}, errNotAnImage
	}
	if tree, ok := d.files.Snapshot(repository, file); ok {
		if _, err := git.BlobID(repository, tree, file); err != nil {
			return imageFile{}, errImageNotFound
		}
		return imageFile{repository: repository, tree: tree, file: file}, nil
	}
	disk, err := onDiskInside(repository, file)
	if err != nil {
		return imageFile{}, err
	}
	return imageFile{repository: repository, file: file, disk: disk}, nil
}

// confined cleans a path relative to a repository's root, and reports false
// for one that climbs out of it.
func confined(file string) (string, bool) {
	cleaned := path.Clean(file)
	if path.IsAbs(cleaned) || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}

// onDiskInside is where a file of a repository is on disk, with every symlink
// followed, refusing one that leads outside the repository.
func onDiskInside(repository, file string) (string, error) {
	root, err := filepath.EvalSymlinks(repository)
	if err != nil {
		return "", errImageNotFound
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(file)))
	if err != nil {
		return "", errImageNotFound
	}
	within, err := filepath.Rel(root, resolved)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", errImageOutside
	}
	if info, err := os.Stat(resolved); err != nil || !info.Mode().IsRegular() {
		return "", errImageNotFound
	}
	return resolved, nil
}

// imageType is the media type an image is served as, by its extension, or ""
// for a file that is not an image. Only images are served, so a Document
// cannot have the daemon serve a page that would run in its origin.
func imageType(file string) string {
	kind := mime.TypeByExtension(path.Ext(file))
	if !strings.HasPrefix(kind, "image/") {
		return ""
	}
	return kind
}

// imageURL is where the page asks for an image found in a repository.
func (d documentImages) imageURL(repository, file string) string {
	return PagePath(d.reviewID) + "/image/" + (&url.URL{Path: file}).EscapedPath() +
		"?repository=" + url.QueryEscape(repository)
}

// serveImage serves an image a page draws: GET /reviews/{review}/page/image/{file...}?repository=root.
func (d *Daemon) serveImage(session *review.Session, w http.ResponseWriter, r *http.Request) {
	found, err := d.imagesOf(session).find(r.URL.Query().Get("repository"), r.PathValue("file"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	content, err := found.read()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", imageType(found.file))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// An SVG opened on its own could carry a script; nothing served here runs.
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(content)
}

// documentImagesAt is where one Document's images are found: its repository,
// and the Document's own path, which an image's relative path is read from.
type documentImagesAt struct {
	documentImages
	repository, document string
}

func (d documentImages) at(repository, document string) documentImagesAt {
	return documentImagesAt{documentImages: d, repository: repository, document: document}
}

// imageDrawing is what the page draws for an image a Document refers to: the
// address to show it from, or why it is shown as text instead.
type imageDrawing struct {
	source string
	why    string
}

// draw decides how an image written in the Document is drawn.
func (d documentImagesAt) draw(destination string) imageDrawing {
	written, err := url.Parse(destination)
	if err != nil || written.Scheme != "" || written.Host != "" {
		return imageDrawing{why: destination} // remote, or unreadable: never fetched
	}
	file := written.Path
	if !strings.HasPrefix(file, "/") {
		file = path.Join(path.Dir(d.document), file)
	}
	file = strings.TrimPrefix(file, "/")
	found, err := d.find(d.repository, file)
	switch {
	case errors.Is(err, errImageOutside):
		return imageDrawing{why: "image outside the repository: " + destination}
	case errors.Is(err, errNotAnImage):
		return imageDrawing{why: "not an image: " + destination}
	case err != nil:
		return imageDrawing{why: "image not found: " + destination}
	}
	return imageDrawing{source: d.imageURL(found.repository, found.file)}
}

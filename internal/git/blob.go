package git

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ReadBlob returns the lines of a file as a tree holds it. A Round's
// after-side is read this way from its Round Snapshot, so what the Reviewer
// sees is what was posted however the working tree has moved since.
func ReadBlob(root, tree, file string) ([]string, error) {
	content, err := runGit(root, "cat-file", "blob", tree+":"+file)
	if err != nil {
		return nil, fmt.Errorf("could not read %s from tree %s: %w", file, shortRev(tree), err)
	}
	return splitGitLines(content), nil
}

// BlobID returns the id of the blob a tree holds for a file.
func BlobID(root, tree, file string) (string, error) {
	out, err := runGit(root, "rev-parse", "--verify", "--quiet", tree+":"+file)
	if err != nil {
		return "", fmt.Errorf("%s is not in tree %s: %w", file, shortRev(tree), err)
	}
	return strings.TrimSpace(out), nil
}

// HashFile returns the blob id a working-tree file would be stored as. git
// applies the same conversions it did when the snapshot was written — line
// endings, clean filters — so a file is compared with its snapshot the way git
// would compare it, and a checkout's conversion is not mistaken for an edit.
func HashFile(root, file string) (string, error) {
	out, err := runGit(root, "hash-object", "--", filepath.FromSlash(file))
	if err != nil {
		return "", fmt.Errorf("could not read %s: %w", file, err)
	}
	return strings.TrimSpace(out), nil
}

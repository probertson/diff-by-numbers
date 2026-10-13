// Package skills embeds the agent-facing skills a dbn release ships with, so
// the binary can compare an installed copy against the one it was built from.
//
// It lives here, beside the skill files, because go:embed can only reach into
// its own directory tree — a package under cmd/ or internal/ cannot embed a
// path above itself.
package skills

import (
	"embed"
	"io/fs"
)

//go:embed dbn-review
var dbnReview embed.FS

// DbnReviewFiles is every file in skills/dbn-review as of this build — SKILL.md
// and the reference files beside it — rooted at that directory, so a name in it
// is the file's path within an installed copy of the skill.
var DbnReviewFiles = mustSub(dbnReview, "dbn-review")

// mustSub roots fsys at dir. fs.Sub fails only on a malformed path, and the
// path here is a constant, so a failure is a bug caught on the first run.
func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

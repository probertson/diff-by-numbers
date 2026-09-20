// Package skills embeds the agent-facing skills a dbn release ships with, so
// the binary can compare an installed copy against the one it was built from.
//
// It lives here, beside the skill files, because go:embed can only reach into
// its own directory tree — a package under cmd/ or internal/ cannot embed a
// path above itself. Embedding from here also makes "the embedded copy matches
// skills/dbn-review/SKILL.md" true by construction rather than by test: the
// compiler reads the same file the repository ships.
package skills

import _ "embed"

// DbnReview is skills/dbn-review/SKILL.md as of this build.
//
//go:embed dbn-review/SKILL.md
var DbnReview string

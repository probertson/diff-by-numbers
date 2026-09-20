package git

import (
	"fmt"
	"strings"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// Snapshot records the repository's working tree as a git tree object and
// returns its id.
//
// It is what makes a Revision Round know what the previous round actually showed.
// Matching rounds by line content could not tell one `}` from another, so in
// any braced language a large share of the Change Set could never be scoped
// out however little the agent touched. A tree records where every line stood,
// so the next round can be matched positionally instead.
//
// The tree is built through the same throwaway index the untracked-file
// derivation uses: copy the real index, `git add -A` into the copy — which
// stages modifications, deletions and untracked files while still honouring
// .gitignore — and write a tree from it. The real index is never written.
//
// The resulting tree is unreferenced, so git's own gc collects it in time, the
// same as anything `git stash create` leaves behind.
func (Deriver) Snapshot(root string) (string, error) {
	index, cleanup, err := copyIndex(root)
	if err != nil {
		return "", err
	}
	defer cleanup()

	env := []string{"GIT_INDEX_FILE=" + index}
	if _, err := runGitEnv(root, env, "add", "-A"); err != nil {
		return "", fmt.Errorf("could not snapshot the working tree of %s: %w", root, err)
	}
	tree, err := runGitEnv(root, env, "write-tree")
	if err != nil {
		return "", fmt.Errorf("could not write a tree for %s: %w", root, err)
	}
	return strings.TrimSpace(tree), nil
}

// The git adapter is both halves of what a Session needs: it derives the Change
// Set, and it snapshots and compares rounds. Asserted here so a signature drift
// is a build failure rather than a Revision Round that silently stops scoping.
var (
	_ review.Deriver     = Deriver{}
	_ review.Snapshotter = Deriver{}
)

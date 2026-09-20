package skills_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/probertson/diff-by-numbers/skills"
)

// Embedding from inside skills/ already makes this true by construction — the
// compiler reads the very file the repository ships. The test is here for the
// one way it could stop being true: someone repoints the //go:embed directive,
// or moves the skill and leaves a stale copy behind for it to find. Neither
// shows up as a build failure, and both would ship a binary that quietly
// compares every install against the wrong text.
func TestTheEmbeddedSkillIsTheOneTheRepositoryShips(t *testing.T) {
	onDisk, err := os.ReadFile(filepath.Join("dbn-review", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}

	if skills.DbnReview != string(onDisk) {
		t.Errorf("the embedded dbn-review skill is not skills/dbn-review/SKILL.md\n"+
			"embedded %d bytes, on disk %d bytes", len(skills.DbnReview), len(onDisk))
	}
}

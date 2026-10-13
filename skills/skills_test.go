package skills_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

// The stale-skill check compares an install against every embedded file, so a
// file the embed misses is one no install is ever checked for. go:embed skips
// names starting with "_" without a word, so a file named that way fails here
// rather than going unchecked. Names starting with "." are skipped on both
// sides: they are an editor's or the OS's, not the skill's.
func TestTheEmbeddedSkillCarriesEveryFileInItsDirectory(t *testing.T) {
	onDisk := map[string]string{}
	err := filepath.WalkDir("dbn-review", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("dbn-review", path)
		if err != nil {
			return err
		}
		onDisk[filepath.ToSlash(rel)] = string(content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	embedded := map[string]string{}
	err = fs.WalkDir(skills.DbnReviewFiles, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, err := fs.ReadFile(skills.DbnReviewFiles, name)
		if err != nil {
			return err
		}
		embedded[name] = string(content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for name, content := range onDisk {
		got, ok := embedded[name]
		if !ok {
			t.Errorf("skills/dbn-review/%s is not embedded", name)
			continue
		}
		if got != content {
			t.Errorf("the embedded %s is not skills/dbn-review/%s", name, name)
		}
	}
	for name := range embedded {
		if _, ok := onDisk[name]; !ok {
			t.Errorf("%s is embedded but is not in skills/dbn-review/", name)
		}
	}
}

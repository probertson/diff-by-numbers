package skills_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/skills"
)

// The stale-skill check compares an install against every embedded file, so the
// embed must hold exactly what skills/dbn-review/ ships, file for file. Nothing
// in the build checks that. go:embed skips names starting with "_" without a
// word, so a file named that way would ship unchecked; and a repointed
// //go:embed directive, or a skill moved with a stale copy left behind, would
// compare every install against the wrong text. Each fails here. go:embed skips
// names starting with "." too, and so does this test: they are an editor's or
// the OS's, not the skill's.
func TestTheEmbeddedSkillIsExactlyItsDirectory(t *testing.T) {
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

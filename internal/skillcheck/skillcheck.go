// Package skillcheck tells a Reviewer when their installed copy of the
// dbn-review skill has fallen behind the binary.
//
// The skill ships separately from dbn — through the Claude Code plugin
// marketplace or `npx skills add` — so the two drift apart on their own. A
// newer skill may call MCP tools an older daemon does not have, or an older
// skill may miss a tool the daemon now offers. `dbn update` replaces the
// binary; this reports on the half it cannot replace.
package skillcheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Config is one run of the check.
type Config struct {
	// Home is the home directory to inspect. Injectable so a test can lay out a
	// fake one.
	Home string
	// Embedded is the dbn-review skill this build shipped with, rooted at the
	// skill's own directory: SKILL.md and every file beside it. Every installed
	// copy is compared against all of it.
	Embedded fs.FS
}

// genericReminder closes every run. dbn only knows the two locations it
// installs to itself; a skill copied into a project, or into another agent's
// skill directory, is invisible to it, so it always says this much rather than
// implying the copies it did check were all of them.
const genericReminder = "Installed the dbn-review skill for another agent or in a project? " +
	"Update it the way you installed it (e.g. `npx skills add probertson/diff-by-numbers/skills/dbn-review`)."

// Report writes what it found about the installed dbn-review skills. It never
// fails: a missing, malformed or unreadable install is skipped, because a
// freshness hint is not worth failing `dbn update` over.
func Report(cfg Config, out io.Writer) {
	// One line however many scopes the plugin is installed at: they are all
	// updated by the same command, so saying it twice would only be noise.
	//
	// The command is the terminal one, since the Reviewer is at a terminal
	// running `dbn update`. In a session, /plugin update only opens the plugin
	// panel (#127). Updated from a terminal, a session already open is not told
	// its skill changed, so it needs /reload-plugins; a new one loads it anyway.
	// Every command printed here is in backticks, so it reads as one to copy.
	for _, dir := range pluginSkillDirs(cfg.Home) {
		if matches, known := compare(dir, cfg.Embedded); known && !matches {
			fmt.Fprintln(out, "Your dbn-review skill (Claude Code plugin) is out of date: "+
				"run `claude plugin update dbn@diff-by-numbers`, "+
				"then `/reload-plugins` in any Claude Code session that's already open.")
			break
		}
	}
	if matches, known := compare(userSkillDir(cfg.Home), cfg.Embedded); known && !matches {
		fmt.Fprintln(out, "Your dbn-review skill (~/.claude/skills) is out of date: "+
			"run `npx skills add probertson/diff-by-numbers/skills/dbn-review`")
	}
	fmt.Fprintln(out, genericReminder)
}

// userSkillDir is the directory `npx skills add` puts the skill in for every
// agent that reads the user skill directory.
func userSkillDir(home string) string {
	return filepath.Join(home, ".claude", "skills", "dbn-review")
}

// mainFile is the file whose presence says the skill is installed at all.
const mainFile = "SKILL.md"

// compare reads the installed copy in dir and says whether it matches what
// shipped, file for file. A shipped file that differs or is missing makes the
// copy stale; a file only the copy has is not dbn's to judge. known is false
// when there is nothing to compare — no main file, so no install — or when no
// file read was stale but one that is there could not be read, since it might
// be the one that differs. Such a copy is never reported as stale.
func compare(dir string, embedded fs.FS) (matches, known bool) {
	if _, err := os.Stat(filepath.Join(dir, mainFile)); err != nil {
		return false, false
	}
	stale, unreadable := false, false
	err := fs.WalkDir(embedded, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		want, err := fs.ReadFile(embedded, name)
		if err != nil {
			return err
		}
		installed, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			stale = true
		case err != nil:
			// Keep going: a later file may still show the copy is stale.
			unreadable = true
		case !bytes.Equal(installed, want):
			stale = true
		}
		return nil
	})
	switch {
	case err != nil:
		// The embedded skill itself could not be read: nothing to compare against.
		return false, false
	case stale:
		return false, true
	case unreadable:
		return false, false
	default:
		return true, true
	}
}

// pluginName is how Claude Code keys this plugin: the plugin's own name, then
// the marketplace it was installed from.
const pluginName = "dbn@diff-by-numbers"

// pluginSkillDirs finds where Claude Code unpacked the dbn plugin's skill
// directory, if it did.
//
// installed_plugins.json is an undocumented Claude Code internal, so every step
// here is best-effort: a missing file, an unexpected shape or an unreadable
// path yields no directories rather than an error. A plugin can be installed at
// more than one scope, so this returns every copy it finds and leaves the
// caller to decide how much to say about them.
func pluginSkillDirs(home string) []string {
	raw, err := os.ReadFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"))
	if err != nil {
		return nil
	}
	var doc struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}

	var dirs []string
	for _, install := range doc.Plugins[pluginName] {
		if install.InstallPath == "" {
			continue
		}
		dirs = append(dirs, filepath.Join(install.InstallPath, "skills", "dbn-review"))
	}
	return dirs
}

package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// diffWithUntracked runs a git diff that also sees untracked, non-ignored files.
//
// Plain `git diff <commit>` covers tracked files only, so a file the Reviewer
// created but has not `git add`ed contributes no Changed Lines: it rendered as
// unchanged context, the counter read 0/0, and coverage passed over it in
// silence. The glossary defines the Change Set as the range "plus its working
// tree", and to a Reviewer a new file is plainly part of that.
//
// The mechanism is a throwaway index. Copy the real one, register the untracked
// paths in the copy with intent-to-add, and run the caller's diff against that.
// git then describes them exactly as it describes a newly added tracked file —
// a whole-file hunk for text, "Binary files … differ" for binary, and a single
// rename for a file moved without git — so parseUnifiedZero needs no changes.
//
// The real index is never written: GIT_INDEX_FILE points every git call at the
// copy, and both temporary files live outside the repository, where they cannot
// themselves show up as untracked. `git add -N` does write one empty blob into
// the repository's object store, which is dangling and collected by git's own
// gc, the same as anything `git stash create` leaves behind.
//
// The args are always a diff command; the parameter is open only so the two
// call sites can keep their own flags.
func diffWithUntracked(root string, args ...string) (string, error) {
	paths, err := untrackedPaths(root)
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		// Nothing to add, so nothing to copy: the overwhelmingly common case
		// stays a single git call against the real index, read-only as ever.
		return runGit(root, args...)
	}

	index, cleanupIndex, err := copyIndex(root)
	if err != nil {
		return "", err
	}
	defer cleanupIndex()

	// A pathspec file rather than arguments: a repository with thousands of
	// untracked files would otherwise overflow the argument list and break
	// derivation outright. --pathspec-file-nul consumes ls-files' -z output as
	// it stands, so no path needs quoting or unquoting on the way through.
	pathspec, cleanupPathspec, err := writeTemp("dbn-pathspec-", literalPathspecs(paths))
	if err != nil {
		return "", err
	}
	defer cleanupPathspec()

	env := []string{"GIT_INDEX_FILE=" + index}
	if _, err := runGitEnv(root, env, "add", "-N",
		"--pathspec-from-file="+pathspec, "--pathspec-file-nul"); err != nil {
		return "", fmt.Errorf("could not account for untracked files in %s: %w", root, err)
	}
	return runGitEnv(root, env, args...)
}

// untrackedPaths lists the files git knows nothing about and has not been told
// to ignore, NUL-separated exactly as git emits them.
func untrackedPaths(root string) ([]byte, error) {
	out, err := runGit(root, "-c", "core.quotePath=false", "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("could not list untracked files in %s: %w", root, err)
	}
	return []byte(out), nil
}

// literalPathspecs marks every path as literal.
//
// git reads a pathspec beginning with a colon as magic, so an untracked file
// innocently named ":notes.txt" is not a path to add but a malformed directive,
// and git refuses the whole command — which would fail derivation for the entire
// repository over one oddly named file. --pathspec-file-nul turns off quoting,
// not magic, so the paths have to say plainly that they are paths.
func literalPathspecs(nulSeparated []byte) []byte {
	var marked [][]byte
	for _, path := range bytes.Split(nulSeparated, []byte{0}) {
		if len(path) == 0 { // the trailing NUL git ends its output with
			continue
		}
		marked = append(marked, append([]byte(":(literal)"), path...))
	}
	if len(marked) == 0 {
		return nil
	}
	return append(bytes.Join(marked, []byte{0}), 0)
}

// copyIndex duplicates the repository's index into a temporary file and returns
// its path, along with a function that removes it.
//
// A missing index is an error rather than something to work around: starting
// from an empty index would make every tracked file look deleted, deriving a
// Change Set that is wrong in the most alarming possible direction.
func copyIndex(root string) (path string, cleanup func(), err error) {
	// rev-parse rather than .git/index: in a worktree the index lives under the
	// worktree's own git directory, and a linked worktree's .git is a file.
	located, err := runGit(root, "rev-parse", "--git-path", "index")
	if err != nil {
		return "", nil, fmt.Errorf("could not locate the git index for %s: %w", root, err)
	}
	real := strings.TrimSpace(located)
	if !filepath.IsAbs(real) {
		real = filepath.Join(root, real)
	}

	content, err := os.ReadFile(real)
	if err != nil {
		return "", nil, fmt.Errorf("could not read the git index at %s: %w", real, err)
	}
	return writeTemp("dbn-index-", content)
}

// writeTemp puts content in a temporary file outside the repository and returns
// its path with a cleanup function. Outside matters: a temporary file inside the
// repository would be untracked, and so would feed itself back into the very
// listing this exists to serve.
func writeTemp(prefix string, content []byte) (path string, cleanup func(), err error) {
	file, err := os.CreateTemp("", prefix)
	if err != nil {
		return "", nil, fmt.Errorf("could not create a temporary file: %w", err)
	}
	name := file.Name()
	remove := func() { _ = os.Remove(name) }

	if _, err := file.Write(content); err != nil {
		file.Close()
		remove()
		return "", nil, fmt.Errorf("could not write %s: %w", name, err)
	}
	if err := file.Close(); err != nil {
		remove()
		return "", nil, fmt.Errorf("could not close %s: %w", name, err)
	}
	return name, remove, nil
}

// runGitEnv runs git with extra environment variables, which is how the
// throwaway index is put in front of git without touching the real one.
//
// stdout and stderr are captured separately, unlike a CombinedOutput call: every
// caller parses the result, and a git warning on stderr would otherwise be
// spliced into that. Here it would land in a pathspec file and fail the command
// with "did not match any files". Errors still quote stderr, which is where git
// says what went wrong.
func runGitEnv(root string, env []string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

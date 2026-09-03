package workingtree_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/review"
	"github.com/probertson/diff-by-numbers/internal/workingtree"
)

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolvesARangeFromARealFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/fetch.ts", "one\ntwo\nthree\nfour\nfive\n")
	resolver := workingtree.NewResolver()

	lines, err := resolver.Resolve(review.Excerpt{
		Repository: root, File: "src/fetch.ts", Side: review.NewSide, FirstLine: 2, LastLine: 4,
	})

	if err != nil {
		t.Fatalf("expected the range to resolve, got %v", err)
	}
	want := []review.Line{{Number: 2, Text: "two"}, {Number: 3, Text: "three"}, {Number: 4, Text: "four"}}
	if len(lines) != len(want) {
		t.Fatalf("expected %d lines, got %d: %v", len(want), len(lines), lines)
	}
	for i, line := range lines {
		if line != want[i] {
			t.Errorf("line %d: expected %+v, got %+v", i, want[i], line)
		}
	}
}

func TestResolvesAFileWithNoTrailingNewline(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "notes.txt", "first\nlast-no-newline")
	resolver := workingtree.NewResolver()

	lines, err := resolver.Resolve(review.Excerpt{
		Repository: root, File: "notes.txt", Side: review.NewSide, FirstLine: 1, LastLine: 2,
	})

	if err != nil {
		t.Fatalf("expected the range to resolve, got %v", err)
	}
	if len(lines) != 2 || lines[1].Text != "last-no-newline" {
		t.Errorf("expected the final unterminated line to survive, got %v", lines)
	}
}

func TestARangeBeyondTheEndOfTheFileIsAProblemNotAPartialAnswer(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "short.txt", "one\ntwo\n")
	resolver := workingtree.NewResolver()

	_, err := resolver.Resolve(review.Excerpt{
		Repository: root, File: "short.txt", Side: review.NewSide, FirstLine: 1, LastLine: 5,
	})

	if err == nil {
		t.Fatal("expected a range beyond the end of the file to be refused")
	}
	if !strings.Contains(err.Error(), "2") || !strings.Contains(err.Error(), "5") {
		t.Errorf("expected the error to name the file's length and the requested range, got %q", err)
	}
}

func TestAMissingFileIsNamedInTheProblem(t *testing.T) {
	root := t.TempDir()
	resolver := workingtree.NewResolver()

	_, err := resolver.Resolve(review.Excerpt{
		Repository: root, File: "gone.ts", Side: review.NewSide, FirstLine: 1, LastLine: 1,
	})

	if err == nil {
		t.Fatal("expected a missing file to be refused")
	}
	if !strings.Contains(err.Error(), "gone.ts") {
		t.Errorf("expected the error to name the file, got %q", err)
	}
}

func TestTheOldSideIsHonestlyUnavailableForNow(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/fetch.ts", "one\n")
	resolver := workingtree.NewResolver()

	_, err := resolver.Resolve(review.Excerpt{
		Repository: root, File: "src/fetch.ts", Side: review.OldSide, FirstLine: 1, LastLine: 1,
	})

	if err == nil {
		t.Fatal("expected the old side to be refused rather than faked from the working tree")
	}
}

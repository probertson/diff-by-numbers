package daemon_test

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// documentsRepo is a Change Set mixing Documents with code: Markdown added under
// both extensions and in upper case, Markdown deleted, a code file, and renames
// both ways, with and without edits, so each file is judged by the right path.
func documentsRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prose := "First line of a long enough body that git sees a rename.\nSecond line.\nThird line.\nFourth line.\nFifth line.\n"
	write("retired.md", "# Retired\n\nNo longer needed.\n")
	write("plan.txt", prose)
	write("notes.md", prose)
	write("draft.txt", prose)
	git("init", "-q", "-b", "main")
	git("add", ".")
	git("commit", "-qm", "initial")
	git("checkout", "-q", "-b", "feature")
	write("spec.md", "# Spec\n\nWhat it does.\n")
	write("guide.markdown", "# Guide\n\nHow to use it.\n")
	write("README.MD", "# Read me\n")
	write("main.go", "package main\n")
	git("rm", "-q", "retired.md")
	git("mv", "plan.txt", "plan.md")
	git("mv", "notes.md", "notes.txt")
	git("mv", "draft.txt", "draft.md")
	write("draft.md", strings.Replace(prose, "Third line.", "Third line, edited.", 1))
	git("add", "-A")
	return root
}

func TestDescribeChangesMarksMarkdownFilesAsDocuments(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := documentsRepo(t)

	got := decodeResult[described](t, callTool(t, server.URL, "describe_changes", map[string]any{
		"repositories": []any{map[string]any{"root": root, "base": "main"}},
	}))

	if len(got.Problems) != 0 || len(got.Repositories) != 1 {
		t.Fatalf("expected one repository described, got %+v", got)
	}
	// A Document carries "document": true; every other file leaves the field out
	// altogether rather than sending false.
	want := map[string]bool{
		"spec.md":        true,  // added Markdown
		"guide.markdown": true,  // the other Markdown extension
		"README.MD":      true,  // Markdown in upper case
		"retired.md":     true,  // deleted, judged by the path it had
		"plan.md":        true,  // renamed into Markdown
		"draft.md":       true,  // renamed into Markdown and edited
		"notes.txt":      false, // renamed out of Markdown
		"main.go":        false, // code
	}
	seen := map[string]bool{}
	for _, file := range got.Repositories[0].Files {
		seen[file.Path] = true
		isDocument, known := want[file.Path]
		if !known {
			t.Errorf("unexpected file %q (%s)", file.Path, file.Status)
			continue
		}
		switch {
		case isDocument && (file.Document == nil || !*file.Document):
			t.Errorf("%s (%s): expected \"document\": true, got %v", file.Path, file.Status, file.Document)
		case !isDocument && file.Document != nil:
			t.Errorf("%s (%s): expected no document field, got %v", file.Path, file.Status, *file.Document)
		}
	}
	for path := range want {
		if !seen[path] {
			t.Errorf("expected %s to be described", path)
		}
	}
}

func TestDescribeChangesReportsWhatEachDocumentCostsOverMCP(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := documentsRepo(t)

	got := decodeResult[described](t, callTool(t, server.URL, "describe_changes", map[string]any{
		"repositories": []any{map[string]any{"root": root, "base": "main"}},
	}))

	if len(got.Problems) != 0 || len(got.Repositories) != 1 {
		t.Fatalf("expected one repository described, got %+v", got)
	}
	files := map[string]describedFile{}
	for _, file := range got.Repositories[0].Files {
		files[file.Path] = file
	}
	// "# Spec" and "What it does." added; "# Retired" and "No longer needed."
	// deleted.
	blocks := []describedBlock{{Lines: [2]int{1, 1}, Cost: 1}, {Lines: [2]int{3, 3}, Cost: 3}}
	for path, want := range map[string]describedFile{
		"spec.md":    {Cost: intPointer(4), NewBlocks: blocks},
		"retired.md": {Cost: intPointer(4), OldBlocks: blocks},
		"main.go":    {},
	} {
		file := files[path]
		if !reflect.DeepEqual(file.Cost, want.Cost) || !reflect.DeepEqual(file.NewBlocks, want.NewBlocks) || !reflect.DeepEqual(file.OldBlocks, want.OldBlocks) {
			t.Errorf("%s: expected cost %v, new blocks %+v, old blocks %+v; got %v, %+v, %+v",
				path, deref(want.Cost), want.NewBlocks, want.OldBlocks, deref(file.Cost), file.NewBlocks, file.OldBlocks)
		}
	}
}

func TestBothToolsTakeAClassificationListOverMCP(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	// The lockfile's three lines are one paragraph of three words.
	classifications := []any{map[string]any{"file": "LOCKFILE", "document": true}}
	round := minimalRound(root)
	round["classifications"] = classifications

	got := decodeResult[described](t, callTool(t, server.URL, "describe_changes", map[string]any{
		"repositories":    []any{map[string]any{"root": root, "base": "main"}},
		"classifications": classifications,
	}))
	posted := decodeResult[postOutcome](t, callTool(t, server.URL, "post_round", round))

	if len(got.Problems) != 0 || len(got.Repositories) != 1 {
		t.Fatalf("expected one repository described, got %+v", got)
	}
	for _, file := range got.Repositories[0].Files {
		if file.Path != "LOCKFILE" {
			continue
		}
		want := []describedBlock{{Lines: [2]int{1, 3}, Cost: 3}}
		if file.Document == nil || !*file.Document || !reflect.DeepEqual(file.NewBlocks, want) {
			t.Errorf("expected the lockfile described as a Document with blocks %+v, got document %v, blocks %+v", want, file.Document, file.NewBlocks)
		}
	}
	if !posted.Accepted {
		t.Errorf("expected the post with a classification to be accepted, got %s", posted.summary())
	}
}

func TestBothToolsReportAClassificationOfAFileOutsideTheChangeSetAsAProblemOverMCP(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	classifications := []any{map[string]any{"file": "SPEC.txt", "document": true}}
	round := minimalRound(root)
	round["classifications"] = classifications

	described := decodeResult[described](t, callTool(t, server.URL, "describe_changes", map[string]any{
		"repositories":    []any{map[string]any{"root": root, "base": "main"}},
		"classifications": classifications,
	}))
	posted := decodeResult[postOutcome](t, callTool(t, server.URL, "post_round", round))

	if !(postOutcome{Problems: described.Problems}).has("malformed_classification") {
		t.Errorf("expected describe_changes to report a malformed_classification problem, got %+v", described.Problems)
	}
	if posted.Accepted || !posted.has("malformed_classification") {
		t.Errorf("expected post_round to refuse with a malformed_classification problem, got %s", posted.summary())
	}
}

func intPointer(n int) *int { return &n }

// deref shows a pointer's value, or nil for a field left out.
func deref(n *int) any {
	if n == nil {
		return nil
	}
	return *n
}

func TestDescribeChangesSaysWhatADocumentIs(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()

	description := describedFileFieldDescription(t, listedTools(t, server.URL), "document")

	for _, want := range []string{"Document", "prose", "document guidance"} {
		if !strings.Contains(description, want) {
			t.Errorf("expected the document field's description to say %q, got %q", want, description)
		}
	}
}

// describedFileFieldDescription reads the description describe_changes's output
// schema gives one field of a described file: what the agent is told it means.
func describedFileFieldDescription(t *testing.T, tools []*mcp.Tool, field string) string {
	t.Helper()
	for _, tool := range tools {
		if tool.Name != "describe_changes" {
			continue
		}
		encoded, err := json.Marshal(tool.OutputSchema)
		if err != nil {
			t.Fatalf("could not encode describe_changes's output schema: %v", err)
		}
		var schema struct {
			Properties struct {
				Repositories struct {
					Items struct {
						Properties struct {
							Files struct {
								Items struct {
									Properties map[string]struct {
										Description string `json:"description"`
									} `json:"properties"`
								} `json:"items"`
							} `json:"files"`
						} `json:"properties"`
					} `json:"items"`
				} `json:"repositories"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(encoded, &schema); err != nil {
			t.Fatalf("could not decode describe_changes's output schema: %v", err)
		}
		property, ok := schema.Properties.Repositories.Items.Properties.Files.Items.Properties[field]
		if !ok {
			t.Fatalf("a described file has no %q field in the output schema", field)
		}
		return property.Description
	}
	t.Fatal("describe_changes is not among the tools")
	return ""
}

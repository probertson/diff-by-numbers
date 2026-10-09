package shim

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMain points every dbn process these tests start at an empty config
// directory, so the developer's own config file can't change what a test sees.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "dbn-config")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestWithAnInvalidConfigFileEveryToolReportsItUntilTheFileIsFixed(t *testing.T) {
	bin := buildDBN(t)
	port := freePort(t)
	root := featureRepo(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "dbn", "config.toml")
	writeConfig(t, path, "port = 70000\n")
	// exec keeps the last of duplicate variables, so this overrides TestMain's.
	env := append(shimEnv(port), "XDG_CONFIG_HOME="+dir)
	ctx := context.Background()

	session := connectShim(t, ctx, bin, env)
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if !hasTool(tools.Tools, "post_round") {
		t.Fatalf("the shim with a broken config serves no post_round; tools: %v", toolNames(tools.Tools))
	}
	refused, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "post_round", Arguments: minimalRound(root)})
	if err != nil {
		t.Fatalf("post_round: %v", err)
	}
	if !refused.IsError || !strings.Contains(text(refused), path) || !strings.Contains(text(refused), "line 1") {
		t.Errorf("post_round with a broken config returned %q (error: %v), want the config error naming %s and the line", text(refused), refused.IsError, path)
	}
	if listening(port) {
		t.Errorf("the shim started a daemon on port %d despite the broken config", port)
	}

	writeConfig(t, path, "update-check = false\n")
	outcome := post(t, ctx, session, minimalRound(root))

	if !outcome.Accepted {
		t.Fatalf("after the file was fixed, the post was not accepted: %s", outcome.summary())
	}
	t.Cleanup(func() { concludeAndWait(port, env, outcome.ReviewID) })
}

func writeConfig(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hasTool(tools []*mcp.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func toolNames(tools []*mcp.Tool) []string {
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name
	}
	return names
}

// text is a tool result's text content, joined.
func text(result *mcp.CallToolResult) string {
	var parts []string
	for _, content := range result.Content {
		if t, ok := content.(*mcp.TextContent); ok {
			parts = append(parts, t.Text)
		}
	}
	return strings.Join(parts, "\n")
}

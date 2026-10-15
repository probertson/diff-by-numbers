package daemon_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// onAStepExplained posts a Round whose one Step is explained, and asks, as
// given, and moves onto it.
func onAStepExplained(t *testing.T, baseURL, explanation, question string) string {
	t.Helper()
	root := featureRepo(t)
	round := roundWithRepository(root, map[string]any{"root": root, "base": "main"})
	step := round["steps"].([]any)[0].(map[string]any)
	step["explanation"] = explanation
	step["questions"] = []any{map[string]any{"text": question}}
	posted := postRound(t, baseURL, round)
	httpPost(t, reviewURL(baseURL, posted.ReviewID)+"/advance")
	return posted.ReviewID
}

// The page is prose in a proportional face, where an agent's Markdown would
// stand out as markers, so a Step's explanation and its Agent Questions are
// rendered (the TUI shows them as written).
func TestAStepsExplanationAndQuestionsAreRenderedAsMarkdown(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onAStepExplained(t, server.URL,
		"Retries now use `backoff()`, which is **capped**.\n\n- first\n- second",
		"Should the cap be *configurable*?")

	out := stop(t, server.URL, reviewID)

	for _, want := range []string{
		"<code>backoff()</code>", "<strong>capped</strong>", "<li>first</li>", "<em>configurable</em>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %s in the Step's header, got:\n%s", want, out)
		}
	}
}

// Rendered or not, nothing an agent writes runs or fetches anything in the
// Reviewer's browser.
func TestAnExplanationsRawHTMLAndImagesAreShownAsText(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	reviewID := onAStepExplained(t, server.URL,
		"Look: <script>alert(1)</script> and ![a chart](https://example.com/chart.png)",
		"Fine?")

	out := stop(t, server.URL, reviewID)

	if strings.Contains(out, "<script>") || strings.Contains(out, "example.com/chart.png\"") || strings.Contains(out, "<img") {
		t.Errorf("expected the raw HTML and the remote image shown as text, got:\n%s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") || !strings.Contains(out, "a chart") {
		t.Errorf("expected the raw HTML escaped and the image's alt text kept, got:\n%s", out)
	}
}

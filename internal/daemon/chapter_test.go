package daemon_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// chapteredRound is roundWithRepository's one Step, posted in a Chapter of its
// own rather than as a flat list.
func chapteredRound(root string) map[string]any {
	walkthrough := roundWithRepository(root, map[string]any{"root": root, "base": "main"})
	steps := walkthrough["steps"]
	delete(walkthrough, "steps")
	walkthrough["chapters"] = []any{map[string]any{
		"name":  "NAME-the-chapter",
		"intro": "INTRO-what-it-covers",
		"steps": steps,
	}}
	return walkthrough
}

func viewWire(t *testing.T, baseURL, reviewID string) daemon.ViewWire {
	t.Helper()
	response, err := http.Get(reviewURL(baseURL, reviewID) + "/view")
	if err != nil {
		t.Fatalf("GET /view: %v", err)
	}
	defer response.Body.Close()
	var view daemon.ViewWire
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		t.Fatalf("decode /view: %v", err)
	}
	return view
}

func TestARoundInChaptersIsAcceptedOverTheWire(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)

	posted := postRound(t, server.URL, chapteredRound(root))
	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/chapter/1")

	view := viewWire(t, server.URL, posted.ReviewID)
	if len(view.Chapters) != 1 {
		t.Fatalf("expected the one Chapter posted, got %+v", view.Chapters)
	}
	chapter := view.Chapters[0]
	if chapter.Name != "NAME-the-chapter" || chapter.Intro != "INTRO-what-it-covers" ||
		chapter.FirstStep != 1 || chapter.LastStep != 1 {
		t.Errorf("expected the Chapter as posted, holding Step 1, got %+v", chapter)
	}
	if view.ChapterIntro != 1 || view.Step != nil {
		t.Errorf("expected going to Chapter 1 to land on its intro, got intro %d", view.ChapterIntro)
	}
}

func TestARoundGivingBothShapesIsRefusedOverTheWire(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	walkthrough := chapteredRound(root)
	walkthrough["steps"] = roundWithRepository(root, map[string]any{"root": root, "base": "main"})["steps"]

	outcome := decodeResult[postOutcome](t, callTool(t, server.URL, "post_round", walkthrough))

	if outcome.Accepted || !outcome.has("steps_and_chapters") {
		t.Errorf("expected a steps_and_chapters problem, got %s", outcome.summary())
	}
}

func TestThePostRoundSchemaDescribesChapters(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()

	schema := postRoundSchema(t, server.URL)

	chapters := schema.Properties["chapters"]
	if !strings.Contains(chapters.Description, "Chapter") {
		t.Errorf("expected chapters to be described, got %q", chapters.Description)
	}
	for _, field := range []string{"name", "intro", "steps"} {
		if chapters.Items.Properties[field].Description == "" {
			t.Errorf("expected a Chapter's %q to be described", field)
		}
	}
	for _, required := range schema.Required {
		if required == "steps" || required == "chapters" {
			t.Errorf("a Round gives steps or chapters, so neither can be required, but %q is", required)
		}
	}
}

type schemaProperty struct {
	Description string `json:"description"`
	Items       struct {
		Properties map[string]schemaProperty `json:"properties"`
	} `json:"items"`
}

type roundSchema struct {
	Properties map[string]schemaProperty `json:"properties"`
	Required   []string                  `json:"required"`
}

// postRoundSchema is post_round's input schema, as the agent reads it over MCP.
func postRoundSchema(t *testing.T, serverURL string) roundSchema {
	t.Helper()
	for _, tool := range listedTools(t, serverURL) {
		if tool.Name != "post_round" {
			continue
		}
		encoded, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("could not encode post_round's schema: %v", err)
		}
		var schema roundSchema
		if err := json.Unmarshal(encoded, &schema); err != nil {
			t.Fatalf("could not decode post_round's schema: %v", err)
		}
		return schema
	}
	t.Fatal("post_round is not among the tools")
	return roundSchema{}
}

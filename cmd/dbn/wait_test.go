package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// `dbn wait` is driven the way an Authoring Agent's harness drives it: a real
// invocation against a real daemon over a real repository, with the Reviewer's
// acts arriving over the Reviewer's own endpoints. What it prints is the
// contract: one line, naming what happened and what to call next.

// reviewRepo makes a repository whose feature branch adds one line to app.ts,
// the smallest change a Round can be posted over.
func reviewRepo(t *testing.T) string {
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
	path := filepath.Join(root, "app.ts")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("init", "-q", "-b", "main")
	git("add", ".")
	git("commit", "-qm", "initial")
	git("checkout", "-q", "-b", "feature")
	if err := os.WriteFile(path, []byte("a\nb\nc\nADDED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// roundOver is a Round covering everything reviewRepo changed. With revises or
// replaces set, it continues or replaces the review of that id.
func roundOver(root string, extra map[string]any) map[string]any {
	round := map[string]any{
		"brief":        map[string]any{"goal": "scope every query by tenant", "approach": "thread the id through"},
		"repositories": []any{map[string]any{"root": root, "base": "main"}},
		"steps": []any{map[string]any{
			"name":        "the change",
			"explanation": "why",
			"excerpts":    []any{map[string]any{"file": "app.ts", "side": "new", "first_line": 1, "last_line": 4}},
		}},
	}
	for key, value := range extra {
		round[key] = value
	}
	return round
}

type posted struct {
	Accepted    bool   `json:"accepted"`
	ReviewID    string `json:"review_id"`
	WaitCommand string `json:"wait_command"`
	Problems    []struct {
		Detail string `json:"detail"`
	} `json:"problems"`
}

// postOver posts a Round over MCP, as the Authoring Agent does.
func postOver(t *testing.T, baseURL string, round map[string]any) posted {
	t.Helper()
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: baseURL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("could not connect to the daemon: %v", err)
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "post_round", Arguments: round})
	if err != nil {
		t.Fatalf("post_round: %v", err)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out posted
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatal(err)
	}
	if !out.Accepted {
		t.Fatalf("expected the Round to be accepted, got %+v", out.Problems)
	}
	return out
}

// reviewer sends one of the Reviewer's acts to the review of this id.
func reviewer(t *testing.T, baseURL, id, act string, body any) {
	t.Helper()
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		payload = bytes.NewReader(encoded)
	}
	response, err := http.Post(baseURL+"/reviews/"+id+"/"+act, "application/json", payload)
	if err != nil {
		t.Fatalf("POST %s: %v", act, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		answer, _ := io.ReadAll(response.Body)
		t.Fatalf("POST %s answered %s: %s", act, response.Status, answer)
	}
}

// raise puts a Comment on the added line, which is on Step 1.
func raise(t *testing.T, baseURL, id, note string) {
	t.Helper()
	reviewer(t, baseURL, id, "goto/1", nil)
	reviewer(t, baseURL, id, "comment", map[string]any{
		"excerpt_index": 0,
		"start":         map[string]any{"line": 4},
		"end":           map[string]any{"line": 4},
		"note":          note,
	})
}

// daemonOn serves a daemon for the test and returns its URL and port.
func daemonOn(t *testing.T, d *daemon.Daemon) (string, int) {
	t.Helper()
	server := httptest.NewServer(d.Handler())
	t.Cleanup(server.Close)
	return server.URL, portOf(t, server.URL)
}

func portOf(t *testing.T, raw string) int {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

// waiting is a `dbn wait` running in the background, as the harness runs it.
type waiting struct {
	out  *bytes.Buffer
	done chan error
}

// boundedFor is the test whose waits are bounded, so a second wait in the same
// test does not swap waitContext out from under the first.
var boundedFor *testing.T

// startWait runs `dbn wait` for the review, bounded so a wait that never
// returns fails the test rather than hanging it.
func startWait(t *testing.T, port int, id string) waiting {
	t.Helper()
	if boundedFor != t {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		previous := waitContext
		waitContext = func() context.Context { return ctx }
		boundedFor = t
		t.Cleanup(func() {
			cancel()
			waitContext = previous
			boundedFor = nil
		})
	}

	w := waiting{out: &bytes.Buffer{}, done: make(chan error, 1)}
	go func() { w.done <- run([]string{"wait", id, "-port", strconv.Itoa(port)}, w.out) }()
	return w
}

// line is what the wait printed once it returned.
func (w waiting) line(t *testing.T) string {
	t.Helper()
	select {
	case err := <-w.done:
		if err != nil {
			t.Fatalf("dbn wait failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("dbn wait did not return")
	}
	return w.out.String()
}

func TestAWaitEndsWhenTheReviewerHandsOffWithComments(t *testing.T) {
	baseURL, port := daemonOn(t, daemon.New())
	review := postOver(t, baseURL, roundOver(reviewRepo(t), map[string]any{"label": "auth refactor"}))
	wait := startWait(t, port, review.ReviewID)
	raise(t, baseURL, review.ReviewID, "rename this")

	reviewer(t, baseURL, review.ReviewID, "finish", nil)

	want := "Review " + review.ReviewID + " (auth refactor) was handed off with 1 Comment. " +
		"Call fetch_results with review_id " + review.ReviewID + ", work the Comments, then post a Revision Round.\n"
	if got := wait.line(t); got != want {
		t.Errorf("expected\n%q\ngot\n%q", want, got)
	}
}

// asking is roundOver with Agent Questions on its one Step.
func asking(root string, extra map[string]any, questions ...string) map[string]any {
	round := roundOver(root, extra)
	var asked []any
	for _, text := range questions {
		asked = append(asked, map[string]any{"text": text})
	}
	round["steps"].([]any)[0].(map[string]any)["questions"] = asked
	return round
}

// answerQuestion puts the Reviewer's Answer to an Agent Question.
func answerQuestion(t *testing.T, baseURL, id string, question int, text string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"answer": text})
	request, err := http.NewRequest(http.MethodPut, baseURL+"/reviews/"+id+"/answer/"+strconv.Itoa(question), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("answering answered %s", response.Status)
	}
}

func TestAWaitNamesAnswersAndUnansweredQuestionsAlongsideComments(t *testing.T) {
	baseURL, port := daemonOn(t, daemon.New())
	review := postOver(t, baseURL, asking(reviewRepo(t), map[string]any{"label": "auth refactor"}, "3 or 5?", "keep the name?"))
	wait := startWait(t, port, review.ReviewID)
	raise(t, baseURL, review.ReviewID, "rename this")
	answerQuestion(t, baseURL, review.ReviewID, 1, "3")

	reviewer(t, baseURL, review.ReviewID, "finish", nil)

	want := "Review " + review.ReviewID + " (auth refactor) was handed off with 1 Comment, 1 Answer and 1 unanswered question. " +
		"Call fetch_results with review_id " + review.ReviewID + ", work the Comments and questions, then post a Revision Round.\n"
	if got := wait.line(t); got != want {
		t.Errorf("expected\n%q\ngot\n%q", want, got)
	}
}

func TestAWaitOnAnsweredQuestionsLeavesTheAgentToConcludeOrRevise(t *testing.T) {
	baseURL, port := daemonOn(t, daemon.New())
	review := postOver(t, baseURL, asking(reviewRepo(t), map[string]any{"label": "auth refactor"}, "3 or 5?", "keep the name?"))
	wait := startWait(t, port, review.ReviewID)
	answerQuestion(t, baseURL, review.ReviewID, 1, "3")
	answerQuestion(t, baseURL, review.ReviewID, 2, "yes")

	reviewer(t, baseURL, review.ReviewID, "finish", nil)

	want := "Review " + review.ReviewID + " (auth refactor) was handed off with 2 Answers. " +
		"Call fetch_results with review_id " + review.ReviewID + " and read them: conclude if no Answer calls for a change, otherwise post a Revision Round.\n"
	if got := wait.line(t); got != want {
		t.Errorf("expected\n%q\ngot\n%q", want, got)
	}
}

func TestAWaitEndsWhenTheReviewerHandsOffWithNothingRaised(t *testing.T) {
	baseURL, port := daemonOn(t, daemon.New())
	review := postOver(t, baseURL, roundOver(reviewRepo(t), map[string]any{"label": "auth refactor"}))
	wait := startWait(t, port, review.ReviewID)

	reviewer(t, baseURL, review.ReviewID, "finish", nil)

	want := "Review " + review.ReviewID + " (auth refactor) was handed off with nothing raised; the review is concluded. " +
		"Call fetch_results with review_id " + review.ReviewID + " to release it.\n"
	if got := wait.line(t); got != want {
		t.Errorf("expected\n%q\ngot\n%q", want, got)
	}
}

func TestAWaitEndsWhenTheReviewerDismissesTheReview(t *testing.T) {
	baseURL, port := daemonOn(t, daemon.New())
	review := postOver(t, baseURL, roundOver(reviewRepo(t), map[string]any{"label": "auth refactor"}))
	wait := startWait(t, port, review.ReviewID)
	raise(t, baseURL, review.ReviewID, "wrong approach entirely")

	reviewer(t, baseURL, review.ReviewID, "dismiss", nil)

	want := "Review " + review.ReviewID + " (auth refactor) was dismissed by the Reviewer. " +
		"Call fetch_results with review_id " + review.ReviewID + " to see what was raised.\n"
	if got := wait.line(t); got != want {
		t.Errorf("expected\n%q\ngot\n%q", want, got)
	}
}

// A daemon that restarted has lost every review it held. Waiting on one of
// those would retry forever, so an answering daemon that does not know the id
// ends the wait.
func TestAWaitEndsWhenTheDaemonNoLongerKnowsTheReview(t *testing.T) {
	_, port := daemonOn(t, daemon.New())

	wait := startWait(t, port, "a1b2")

	want := "Review a1b2 is no longer known to dbn; the daemon was probably restarted. Tell the Reviewer.\n"
	if got := wait.line(t); got != want {
		t.Errorf("expected\n%q\ngot\n%q", want, got)
	}
}

// stillWaiting asserts the wait has not returned, over long enough for a daemon
// holding polls briefly to have answered several of them "not yet".
func (w waiting) stillWaiting(t *testing.T, after string) {
	t.Helper()
	select {
	case err := <-w.done:
		t.Fatalf("the wait ended after %s, printing %q (err %v)", after, w.out.String(), err)
	case <-time.After(300 * time.Millisecond):
	}
}

// briefPolls is a daemon that answers a poll "not yet" quickly, so a test sees
// a wait carry on across polls rather than inside one.
func briefPolls() *daemon.Daemon { return daemon.New(daemon.WithWaitHold(20 * time.Millisecond)) }

// Only the four events end a wait. Everything else is the Reviewer working, or
// the agent correcting its own Round, and nothing the agent can act on.
func TestAWaitCarriesOnThroughEverythingButTheEvents(t *testing.T) {
	root := reviewRepo(t)
	baseURL, port := daemonOn(t, briefPolls())
	review := postOver(t, baseURL, roundOver(root, map[string]any{"label": "auth refactor"}))
	wait := startWait(t, port, review.ReviewID)

	reviewer(t, baseURL, review.ReviewID, "goto/1", nil)
	reviewer(t, baseURL, review.ReviewID, "back", nil)
	wait.stillWaiting(t, "the Reviewer moved through the Round")
	raise(t, baseURL, review.ReviewID, "rename this")
	wait.stillWaiting(t, "the Reviewer raised a Comment")
	postOver(t, baseURL, roundOver(root, map[string]any{"replaces": review.ReviewID}))
	wait.stillWaiting(t, "the agent replaced the Round")

	reviewer(t, baseURL, review.ReviewID, "finish", nil)

	if got := wait.line(t); !strings.Contains(got, "was handed off with 1 Comment") {
		t.Errorf("expected the Hand Off after all that to end the wait, got %q", got)
	}
}

// A Reviewer who hands off and takes it back has handed nothing to the agent,
// so a wait started once the Round is reopened waits for the next Hand Off.
func TestAWaitStartedAfterAReopenWaitsForTheNextHandOff(t *testing.T) {
	baseURL, port := daemonOn(t, briefPolls())
	review := postOver(t, baseURL, roundOver(reviewRepo(t), map[string]any{"label": "auth refactor"}))
	raise(t, baseURL, review.ReviewID, "rename this")
	reviewer(t, baseURL, review.ReviewID, "finish", nil)
	reviewer(t, baseURL, review.ReviewID, "reopen", nil)

	wait := startWait(t, port, review.ReviewID)

	wait.stillWaiting(t, "the Reviewer reopened the Round")

	reviewer(t, baseURL, review.ReviewID, "finish", nil)

	if got := wait.line(t); !strings.Contains(got, "was handed off with 1 Comment") {
		t.Errorf("expected the second Hand Off to end the wait, got %q", got)
	}
}

// An agent slow to start its wait must not miss the Hand Off it was waiting for.
func TestAWaitStartedAfterTheHandOffReturnsAtOnce(t *testing.T) {
	baseURL, port := daemonOn(t, daemon.New())
	review := postOver(t, baseURL, roundOver(reviewRepo(t), map[string]any{"label": "auth refactor"}))
	raise(t, baseURL, review.ReviewID, "rename this")
	reviewer(t, baseURL, review.ReviewID, "finish", nil)

	wait := startWait(t, port, review.ReviewID)

	if got := wait.line(t); !strings.Contains(got, "was handed off with 1 Comment") {
		t.Errorf("expected the Hand Off already made, got %q", got)
	}
}

// The wait after a Revision Round is for that round's Hand Off, not the one
// the agent has already answered.
func TestAWaitStartedAfterARevisionRoundIgnoresTheEarlierHandOff(t *testing.T) {
	root := reviewRepo(t)
	baseURL, port := daemonOn(t, briefPolls())
	review := postOver(t, baseURL, roundOver(root, map[string]any{"label": "auth refactor"}))
	raise(t, baseURL, review.ReviewID, "rename this")
	reviewer(t, baseURL, review.ReviewID, "finish", nil)
	postOver(t, baseURL, roundOver(root, map[string]any{
		"revises":      review.ReviewID,
		"dispositions": []any{map[string]any{"comment_id": 1, "status": "answered", "response": "the name is the domain's"}},
	}))

	wait := startWait(t, port, review.ReviewID)

	wait.stillWaiting(t, "a Revision Round was posted over an earlier Hand Off")

	reviewer(t, baseURL, review.ReviewID, "finish", nil)

	if got := wait.line(t); !strings.Contains(got, "was handed off with nothing raised") {
		t.Errorf("expected the Revision Round's own Hand Off, got %q", got)
	}
}

// Two waits on one review, started by mistake, must both end: a wait left
// hanging is a background command the agent never hears from.
func TestTwoWaitsOnOneReviewBothEnd(t *testing.T) {
	baseURL, port := daemonOn(t, daemon.New())
	review := postOver(t, baseURL, roundOver(reviewRepo(t), map[string]any{"label": "auth refactor"}))
	first := startWait(t, port, review.ReviewID)
	second := startWait(t, port, review.ReviewID)
	time.Sleep(100 * time.Millisecond) // both polls open before the Hand Off

	reviewer(t, baseURL, review.ReviewID, "finish", nil)

	for _, wait := range []waiting{first, second} {
		if got := wait.line(t); !strings.Contains(got, "was handed off with nothing raised") {
			t.Errorf("expected both waits to end on the Hand Off, got %q", got)
		}
	}
}

// Only a daemon that answers ends a wait. One not up yet — not started, or
// restarting under `dbn update` — is asked again until it is.
func TestAWaitRetriesUntilTheDaemonAnswers(t *testing.T) {
	previous := waitRetry
	waitRetry = 20 * time.Millisecond
	t.Cleanup(func() { waitRetry = previous })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close() // nothing on the port, for now
	wait := startWait(t, port, "a1b2")

	wait.stillWaiting(t, "asking a port with nothing on it")
	listener, err = net.Listen("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("could not take the port back: %v", err)
	}
	server := httptest.NewUnstartedServer(daemon.New().Handler())
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)

	if got := wait.line(t); !strings.Contains(got, "Review a1b2 is no longer known to dbn") {
		t.Errorf("expected the daemon that came up to end the wait, got %q", got)
	}
}

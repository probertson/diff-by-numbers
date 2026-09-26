package daemon_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// askingOnStep1 is minimalRound with Agent Questions on its first Step.
func askingOnStep1(root string, questions ...string) map[string]any {
	round := minimalRound(root)
	var asked []any
	for _, text := range questions {
		asked = append(asked, map[string]any{"text": text})
	}
	round["steps"].([]any)[0].(map[string]any)["questions"] = asked
	return round
}

// answer puts the Reviewer's Answer to an Agent Question, as the TUI does.
func answer(t *testing.T, reviewURL string, id int, text string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"answer": text})
	request, err := http.NewRequest(http.MethodPut, reviewURL+"/answer/"+strconv.Itoa(id), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("PUT answer: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(response.Body)
		t.Fatalf("PUT answer answered %s: %s", response.Status, b)
	}
}

type questionView struct {
	Step *struct {
		Questions []struct {
			ID     int    `json:"id"`
			Text   string `json:"text"`
			Answer string `json:"answer"`
		} `json:"questions"`
	} `json:"step"`
}

type fetchedQuestions struct {
	Message   string `json:"message"`
	Questions []struct {
		ID         int    `json:"id"`
		Step       int    `json:"step"`
		Question   string `json:"question"`
		Answer     string `json:"answer"`
		Unanswered bool   `json:"unanswered"`
	} `json:"questions"`
}

func TestAnAgentQuestionOnAStepIsShownAnsweredAndReturned(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	posted := postRound(t, server.URL, askingOnStep1(featureRepo(t), "3 or 5 retries?", "keep the old name?"))
	review := reviewURL(server.URL, posted.ReviewID)
	httpPost(t, review+"/goto/1")

	var shown questionView
	if err := json.Unmarshal([]byte(get(t, review+"/view")), &shown); err != nil {
		t.Fatal(err)
	}
	answer(t, review, shown.Step.Questions[0].ID, "3")
	httpPost(t, review+"/finish")
	fetched := decodeResult[fetchedQuestions](t, callTool(t, server.URL, "fetch_results", map[string]any{"review_id": posted.ReviewID}))

	if len(shown.Step.Questions) != 2 || shown.Step.Questions[0].Text != "3 or 5 retries?" {
		t.Fatalf("expected Step 1 to show both questions, got %+v", shown.Step)
	}
	if len(fetched.Questions) != 2 {
		t.Fatalf("expected both questions back, got %+v", fetched.Questions)
	}
	first, second := fetched.Questions[0], fetched.Questions[1]
	if first.Question != "3 or 5 retries?" || first.Answer != "3" || first.Unanswered || first.Step != 1 {
		t.Errorf("expected the answered question with its Answer, got %+v", first)
	}
	if !second.Unanswered || second.Answer != "" {
		t.Errorf("expected the other marked unanswered, got %+v", second)
	}
	if strings.Contains(fetched.Message, "raised nothing") || strings.Contains(fetched.Message, "complete") {
		t.Errorf("a Round with questions is not concluded at Hand Off, got: %s", fetched.Message)
	}
	if !strings.Contains(fetched.Message, "Answer") {
		t.Errorf("expected the message to point the agent at the Answers, got: %s", fetched.Message)
	}
}

func TestAnEmptyAgentQuestionIsRefusedAsAProblem(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()

	outcome := decodeResult[postOutcome](t, callTool(t, server.URL, "post_round", askingOnStep1(featureRepo(t), "")))

	if outcome.Accepted || !outcome.has("malformed_question") {
		t.Errorf("expected a malformed_question problem, got %s", outcome.summary())
	}
}

func TestAnAnswerIsRefusedOnceHandedOff(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	posted := postRound(t, server.URL, askingOnStep1(featureRepo(t), "3 or 5?"))
	review := reviewURL(server.URL, posted.ReviewID)
	httpPost(t, review+"/finish")

	body, _ := json.Marshal(map[string]any{"answer": "3"})
	request, _ := http.NewRequest(http.MethodPut, review+"/answer/1", bytes.NewReader(body))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()

	if response.StatusCode != http.StatusConflict {
		t.Errorf("expected an Answer after Hand Off to be refused, got %s", response.Status)
	}
}

func TestTheWaitAnswerCountsAnswersAndUnansweredQuestions(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	posted := postRound(t, server.URL, askingOnStep1(featureRepo(t), "3 or 5?", "keep the name?"))
	review := reviewURL(server.URL, posted.ReviewID)
	answer(t, review, 1, "3")
	httpPost(t, review+"/finish")

	var waited daemon.WaitWire
	if err := json.Unmarshal([]byte(get(t, review+"/wait")), &waited); err != nil {
		t.Fatal(err)
	}

	if waited.Event == daemon.WaitConcluded {
		t.Fatal("a Hand Off that carried questions must not end the wait as concluded")
	}
	if waited.Answers != 1 || waited.Unanswered != 1 {
		t.Errorf("expected 1 Answer and 1 unanswered question, got %+v", waited)
	}
}

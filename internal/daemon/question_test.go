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

func TestARoundLevelQuestionIsShownWithTheBriefAndReturnedOnNoStep(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	round := askingOnStep1(featureRepo(t), "3 or 5?")
	round["questions"] = []any{map[string]any{"text": "is threading the id the right approach?"}}
	posted := postRound(t, server.URL, round)
	review := reviewURL(server.URL, posted.ReviewID)

	var overview struct {
		RoundQuestions []struct {
			ID   int    `json:"id"`
			Text string `json:"text"`
		} `json:"round_questions"`
	}
	if err := json.Unmarshal([]byte(get(t, review+"/view")), &overview); err != nil {
		t.Fatal(err)
	}
	answer(t, review, overview.RoundQuestions[0].ID, "yes")
	httpPost(t, review+"/finish")
	fetched := decodeResult[fetchedQuestions](t, callTool(t, server.URL, "fetch_results", map[string]any{"review_id": posted.ReviewID}))

	if len(overview.RoundQuestions) != 1 || overview.RoundQuestions[0].Text != "is threading the id the right approach?" {
		t.Fatalf("expected the Overview to carry the Round-level question, got %+v", overview.RoundQuestions)
	}
	if len(fetched.Questions) != 2 {
		t.Fatalf("expected both questions back, got %+v", fetched.Questions)
	}
	if onRound := fetched.Questions[0]; onRound.Step != 0 || onRound.Answer != "yes" {
		t.Errorf("expected the Round-level question on no Step, with its Answer, got %+v", onRound)
	}
	if onStep := fetched.Questions[1]; onStep.Step != 1 {
		t.Errorf("expected the Step's question on Step 1, got %+v", onStep)
	}
}

func TestARevisionRoundAccountsForTheQuestionsOverTheWire(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	posted := postRound(t, server.URL, askingOnStep1(root, "3 or 5?"))
	review := reviewURL(server.URL, posted.ReviewID)
	answer(t, review, 1, "5")
	httpPost(t, review+"/finish")
	revision := minimalRound(root)
	delete(revision, "label")
	revision["revises"] = posted.ReviewID

	refused := decodeResult[postOutcome](t, callTool(t, server.URL, "post_round", revision))
	revision["question_statuses"] = []any{map[string]any{"question_id": 1, "status": "addressed", "response": "raised it to 5"}}
	postRound(t, server.URL, revision)
	var overview struct {
		AccountedQuestions []struct {
			Question struct {
				Text   string `json:"text"`
				Answer string `json:"answer"`
			} `json:"question"`
			Status   string `json:"status"`
			Response string `json:"response"`
		} `json:"accounted_questions"`
	}
	if err := json.Unmarshal([]byte(get(t, review+"/view")), &overview); err != nil {
		t.Fatal(err)
	}

	if refused.Accepted || !refused.has("malformed_question_status") {
		t.Errorf("expected a Revision Round leaving the question out to be refused, got %s", refused.summary())
	}
	if len(overview.AccountedQuestions) != 1 {
		t.Fatalf("expected the earlier question on the Overview, got %+v", overview.AccountedQuestions)
	}
	got := overview.AccountedQuestions[0]
	if got.Question.Text != "3 or 5?" || got.Question.Answer != "5" || got.Status != "addressed" || got.Response != "raised it to 5" {
		t.Errorf("expected the question, its Answer and its status, got %+v", got)
	}
}

func TestAnsweredQuestionsCarryOverAReplacementAndCanBeWithdrawn(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	root := featureRepo(t)
	posted := postRound(t, server.URL, askingOnStep1(root, "3 or 5?", "keep the name?", "log it?"))
	review := reviewURL(server.URL, posted.ReviewID)
	answer(t, review, 1, "5")
	answer(t, review, 3, "no")
	replacement := minimalRound(root)
	replacement["replaces"] = posted.ReviewID
	postRound(t, server.URL, replacement)

	request, _ := http.NewRequest(http.MethodDelete, review+"/question/3", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	httpPost(t, review+"/finish")
	fetched := decodeResult[struct {
		Questions []struct {
			ID          int    `json:"id"`
			Answer      string `json:"answer"`
			CarriedOver bool   `json:"carried_over"`
		} `json:"questions"`
	}](t, callTool(t, server.URL, "fetch_results", map[string]any{"review_id": posted.ReviewID}))

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected the carried-over question withdrawn, got %s", response.Status)
	}
	if len(fetched.Questions) != 1 {
		t.Fatalf("expected only the answered question still standing, got %+v", fetched.Questions)
	}
	if got := fetched.Questions[0]; got.ID != 1 || got.Answer != "5" || !got.CarriedOver {
		t.Errorf("expected question 1 carried over with its Answer, got %+v", got)
	}
}

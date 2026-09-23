package daemon_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// Every accepted post hands the agent the command that waits on its review,
// composed by dbn so the agent never has to put the id or the port together.
func TestAnAcceptedPostCarriesTheCommandThatWaitsOnIt(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()

	posted := decodeResult[struct {
		ReviewID    string `json:"review_id"`
		WaitCommand string `json:"wait_command"`
	}](t, callTool(t, server.URL, "post_round", minimalRound(featureRepo(t))))

	if want := "dbn wait " + posted.ReviewID; posted.WaitCommand != want {
		t.Errorf("expected %q, got %q", want, posted.WaitCommand)
	}
}

// agentTold is what the Reviewer's window reads to choose between "Your agent
// has been told" and "Tell your agent you've handed off".
func agentTold(t *testing.T, baseURL, reviewID string) bool {
	t.Helper()
	var view struct {
		Finished  bool `json:"finished"`
		AgentTold bool `json:"agent_told"`
	}
	if err := json.Unmarshal([]byte(get(t, reviewURL(baseURL, reviewID)+"/view")), &view); err != nil {
		t.Fatalf("could not read the view: %v", err)
	}
	if !view.Finished {
		t.Fatal("the view is read after a Hand Off")
	}
	return view.AgentTold
}

// pollOnce makes one wait poll and returns once it is answered.
func pollOnce(t *testing.T, baseURL, reviewID string) {
	t.Helper()
	get(t, reviewURL(baseURL, reviewID)+"/wait")
}

func TestAHandOffWithAWaitOpenTellsTheAgent(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	posted := postRound(t, server.URL, minimalRound(featureRepo(t)))
	answered := make(chan struct{})
	go func() {
		pollOnce(t, server.URL, posted.ReviewID)
		close(answered)
	}()
	time.Sleep(100 * time.Millisecond) // the poll is open before the Hand Off

	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/finish")

	<-answered
	if !agentTold(t, server.URL, posted.ReviewID) {
		t.Error("a waiter was handed the Hand Off, so the agent has been told")
	}
}

func TestAHandOffWithNoWaitDoesNotTellTheAgent(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	posted := postRound(t, server.URL, minimalRound(featureRepo(t)))

	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/finish")

	if agentTold(t, server.URL, posted.ReviewID) {
		t.Error("nothing was waiting, so nobody was told")
	}
}

// A wait asks again the moment a poll is answered "not yet", so a Hand Off can
// land between two polls. The waiter is still listening, and the screen must not
// send the Reviewer off to relay what it is about to hear.
func TestAHandOffBetweenTwoPollsStillCountsTheWaiter(t *testing.T) {
	server := httptest.NewServer(daemon.New(daemon.WithWaitHold(10 * time.Millisecond)).Handler())
	defer server.Close()
	posted := postRound(t, server.URL, minimalRound(featureRepo(t)))
	pollOnce(t, server.URL, posted.ReviewID) // answered "not yet"; no poll open now

	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/finish")

	if !agentTold(t, server.URL, posted.ReviewID) {
		t.Error("a waiter between polls is still listening")
	}
}

// A waiter that stops asking died with its harness session. The screen then
// says to tell the agent, which is right: nothing is listening.
func TestAWaiterThatStoppedAskingIsNotCountedOnceTheGraceRunsOut(t *testing.T) {
	server := httptest.NewServer(daemon.New(
		daemon.WithWaitHold(10*time.Millisecond),
		daemon.WithWaiterGrace(50*time.Millisecond),
	).Handler())
	defer server.Close()
	posted := postRound(t, server.URL, minimalRound(featureRepo(t)))
	pollOnce(t, server.URL, posted.ReviewID)
	time.Sleep(200 * time.Millisecond)

	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/finish")

	if agentTold(t, server.URL, posted.ReviewID) {
		t.Error("a waiter silent past the grace is not listening")
	}
}

// A wait that was handed the Hand Off has exited: it will not ask again. If the
// Reviewer takes the Hand Off back and makes it again, nobody hears that one.
func TestAWaiterThatWasToldIsNotCountedForTheNextHandOff(t *testing.T) {
	server := httptest.NewServer(daemon.New().Handler())
	defer server.Close()
	posted := postRound(t, server.URL, minimalRound(featureRepo(t)))
	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/goto/1")
	raiseComment(t, reviewURL(server.URL, posted.ReviewID), 0, 4, 4, "rename this")
	answered := make(chan struct{})
	go func() {
		pollOnce(t, server.URL, posted.ReviewID)
		close(answered)
	}()
	time.Sleep(100 * time.Millisecond)
	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/finish")
	<-answered
	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/reopen")

	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/finish")

	if agentTold(t, server.URL, posted.ReviewID) {
		t.Error("the only waiter already exited, so the second Hand Off reached nobody")
	}
}

// A waiter answered "not yet" is expected back within the grace. Once it is
// handed an event it exits instead, so that grace must not outlive it: a
// Hand Off made again straight after a Reopen reaches nobody.
func TestAWaiterThatWasToldIsNotCountedEvenWithinItsGrace(t *testing.T) {
	server := httptest.NewServer(daemon.New(daemon.WithWaitHold(10 * time.Millisecond)).Handler())
	defer server.Close()
	posted := postRound(t, server.URL, minimalRound(featureRepo(t)))
	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/goto/1")
	raiseComment(t, reviewURL(server.URL, posted.ReviewID), 0, 4, 4, "rename this")
	pollOnce(t, server.URL, posted.ReviewID) // "not yet": the grace starts
	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/finish")
	pollOnce(t, server.URL, posted.ReviewID) // handed the Hand Off; the waiter exits
	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/reopen")

	httpPost(t, reviewURL(server.URL, posted.ReviewID)+"/finish")

	if agentTold(t, server.URL, posted.ReviewID) {
		t.Error("the only waiter already exited, so the second Hand Off reached nobody")
	}
}

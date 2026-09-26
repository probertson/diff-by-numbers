package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/probertson/diff-by-numbers/internal/review"
)

// A Review changing hands is pushed to its Authoring Agent through a process
// the harness owns: `dbn wait`, run in the background, long-polls the endpoint
// here and exits when the Reviewer hands off or dismisses. The harness wakes the
// agent when it exits (ADR-0016). The agent itself never waits.

// WaitWire is what the wait endpoint answers: the outcome of the Round that ended the wait,
// or none yet when the poll was held as long as it is held.
type WaitWire struct {
	// Event is one of the Wait constants, or "" for "not yet: poll again".
	Event    string `json:"event"`
	ReviewID string `json:"review_id"`
	Label    string `json:"label,omitempty"`
	// Comments is how many the Reviewer raised, for a Hand Off. Answers and
	// Unanswered count the Round's Agent Questions the Reviewer did and did not
	// answer.
	Comments   int `json:"comments"`
	Answers    int `json:"answers,omitempty"`
	Unanswered int `json:"unanswered,omitempty"`
	// MayConclude says the agent may end the review itself if no Answer calls
	// for a change: every question answered, nothing else raised.
	MayConclude bool `json:"may_conclude,omitempty"`
}

// The events that end a wait. A Hand Off with Comments is the one left over.
const (
	WaitConcluded = string(review.HandedOffNothingRaised)
	WaitDismissed = string(review.Dismissed)
	// WaitUnknown is a daemon that answered but is not holding the review: it
	// restarted and lost it, usually. Without it a wait would retry forever.
	WaitUnknown = "unknown"
)

// defaultWaitHold is how long a poll is held open before it is answered "not
// yet". Well under any client or proxy timeout, so a poll never dies of one.
const defaultWaitHold = 30 * time.Second

// defaultWaiterGrace is how long after a poll closes its waiter still counts as
// listening: the gap before the next poll is not the agent going away.
const defaultWaiterGrace = 5 * time.Second

// WithWaitHold changes how long a wait poll is held, so a test can watch a wait
// carry on across polls without sitting through real ones.
func WithWaitHold(hold time.Duration) Option {
	return func(d *Daemon) { d.waitHold = hold }
}

// WithWaiterGrace changes how long a waiter counts as listening after its poll
// closes, so a test can watch it lapse.
func WithWaiterGrace(grace time.Duration) Option {
	return func(d *Daemon) { d.waiterGrace = grace }
}

// waiters is what the daemon knows of the waits on one Review. It is transport
// bookkeeping — which polls are open — so it lives here, not in the core.
type waiters struct {
	// open are the polls held right now, each with whether it has been released.
	open map[chan struct{}]bool
	// lastClosed is when a poll last ended, which the grace is measured from.
	lastClosed time.Time
	// told records that a waiter was handed the current Hand Off.
	told bool
}

// waitersOf returns the bookkeeping for one Review, under the caller's lock.
func (d *Daemon) waitersOf(id string) *waiters {
	w, ok := d.waiting[id]
	if !ok {
		w = &waiters{open: map[chan struct{}]bool{}}
		d.waiting[id] = w
	}
	return w
}

// wake releases every poll open on a Review so each answers at once, under the
// caller's lock. Every waiter hears, so two waits started by mistake both end.
func (d *Daemon) wake(id string) {
	w := d.waitersOf(id)
	for poll, released := range w.open {
		if !released {
			close(poll)
			w.open[poll] = true
		}
	}
}

// agentTold reports whether the Authoring Agent has heard about the Hand Off
// on screen, under the caller's lock: a waiter was handed it, or one is still
// listening and is about to be. The Reviewer stops relaying only if this can be
// trusted, so a waiter that stopped asking — its harness session ended — counts
// for no longer than the grace.
func (d *Daemon) agentTold(id string) bool {
	w, ok := d.waiting[id]
	if !ok {
		return false
	}
	if w.told || len(w.open) > 0 {
		return true
	}
	return !w.lastClosed.IsZero() && time.Since(w.lastClosed) < d.waiterGrace
}

// waitAnswer is the answer for a wait on a Review, under the caller's lock.
func (d *Daemon) waitAnswer(id string) WaitWire {
	session, ok := d.review(id)
	if !ok {
		return WaitWire{Event: WaitUnknown, ReviewID: id}
	}
	event := session.Outcome()
	if event != review.NoOutcomeYet {
		// A waiter handed an event exits rather than polling again, so the
		// grace a "not yet" gave it ends here too.
		listening := d.waitersOf(id)
		listening.lastClosed = time.Time{}
		if event != review.Dismissed {
			listening.told = true
		}
	}
	answered, unanswered := review.CountAnswers(session.Questions())
	return WaitWire{
		Event:      string(event),
		ReviewID:   id,
		Label:      session.Label(),
		Comments:   len(session.Comments()),
		Answers:    answered,
		Unanswered: unanswered,

		MayConclude: session.MayConclude(),
	}
}

// serveWait serves one long poll: an answer at once if the wait is already
// over, otherwise held until the Reviewer hands off or dismisses, or until the
// hold runs out and the answer is "not yet".
func (d *Daemon) serveWait(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("review")

	d.mu.Lock()
	answer := d.waitAnswer(id)
	if answer.Event != "" {
		d.mu.Unlock()
		writeWait(w, answer)
		return
	}
	poll := make(chan struct{})
	d.waitersOf(id).open[poll] = false
	d.mu.Unlock()

	hold := time.NewTimer(d.waitHold)
	defer hold.Stop()
	gone := false
	select {
	case <-poll:
	case <-hold.C:
	case <-r.Context().Done():
		gone = true
	case <-d.quit:
		gone = true
	}

	d.mu.Lock()
	listening := d.waitersOf(id)
	delete(listening.open, poll)
	if !gone {
		answer = d.waitAnswer(id)
	}
	// Only a "not yet" is followed by another poll. A waiter handed an event
	// exits, and one whose connection went has died or lost its daemon, so
	// neither is still listening.
	if !gone && answer.Event == "" {
		listening.lastClosed = time.Now()
	}
	d.mu.Unlock()
	if gone {
		return
	}
	writeWait(w, answer)
}

func writeWait(w http.ResponseWriter, answer WaitWire) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(answer); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Wait waits on the Review id at the daemon at baseURL until the
// Reviewer hands it off or dismisses it, or the daemon says it does not know
// it. A daemon that cannot be reached is asked again after retry, for as long as
// ctx allows: only a daemon that answers can end the wait, so an outage or a
// `dbn update` restart is waited through rather than reported.
func Wait(ctx context.Context, baseURL, id string, retry time.Duration) (WaitWire, error) {
	// No timeout of its own beyond a generous bound on one poll: the daemon
	// answers every poll well inside it.
	client := &http.Client{Timeout: 2 * defaultWaitHold}
	for {
		answer, err := pollWait(ctx, client, baseURL, id)
		switch {
		case err == nil && answer.Event != "":
			return answer, nil
		case err == nil:
			continue
		}
		select {
		case <-ctx.Done():
			return WaitWire{}, ctx.Err()
		case <-time.After(retry):
		}
	}
}

// pollWait makes one poll. Anything but a well-formed answer is an error,
// which the caller retries: something other than dbn on the port is no more an
// answer than nothing there.
func pollWait(ctx context.Context, client *http.Client, baseURL, id string) (WaitWire, error) {
	target := fmt.Sprintf("%s/reviews/%s/wait", baseURL, url.PathEscape(id))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return WaitWire{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return WaitWire{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return WaitWire{}, fmt.Errorf("the daemon answered %s", response.Status)
	}
	var answer WaitWire
	if err := json.NewDecoder(response.Body).Decode(&answer); err != nil {
		return WaitWire{}, err
	}
	return answer, nil
}

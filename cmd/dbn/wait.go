package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/probertson/diff-by-numbers/internal/daemon"
)

// waitRetry is how long `dbn wait` pauses before asking an unreachable daemon
// again. A variable so a test need not sit through it.
var waitRetry = 2 * time.Second

// waitContext is what cancels `dbn wait`: nothing, outside a test, which bounds
// it so a wait that never returns fails rather than hangs.
var waitContext = context.Background

// runWait is `dbn wait <review_id> [-port N]`: it blocks until the Reviewer
// hands the review off or dismisses it, then prints one line saying so and what
// to call next. The Authoring Agent's harness runs it in the background and
// wakes the agent when it exits (ADR-0016).
func runWait(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("wait", flag.ContinueOnError)
	port := flags.Int("port", defaultPort(), "port the daemon is listening on")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "usage: dbn wait <review_id> [-port N]")
		flags.PrintDefaults()
	}
	// The id comes first in the command dbn hands out, and the flag package
	// stops at the first argument that is not a flag, so take it before parsing.
	var id string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if id == "" && flags.NArg() > 0 {
		id = flags.Arg(0)
	}
	if id == "" {
		return fmt.Errorf("dbn wait needs the review id post_round gave you; run `dbn wait -h` for usage")
	}

	answer, err := daemon.Wait(waitContext(), daemonURL(*port), id, waitRetry)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, waitLine(answer))
	return nil
}

// waitLine is the one line a wait ends with: what happened, and what to
// call next. It never carries the results — fetch_results stays their only
// source, and it is what re-grounds the agent (ADR-0008).
func waitLine(answer daemon.WaitWire) string {
	id := answer.ReviewID
	review := "Review " + id
	if answer.Label != "" {
		review += " (" + answer.Label + ")"
	}
	switch answer.Event {
	case daemon.WaitConcluded:
		return fmt.Sprintf("%s was handed off with nothing raised; the review is concluded. Call fetch_results with review_id %s to release it.", review, id)
	case daemon.WaitDismissed:
		return fmt.Sprintf("%s was dismissed by the Reviewer. Call fetch_results with review_id %s to see what was raised.", review, id)
	case daemon.WaitUnknown:
		return fmt.Sprintf("Review %s is no longer known to dbn; the daemon was probably restarted. Tell the Reviewer.", id)
	}
	// Something was raised: Comments, Agent Questions, or both. What each count
	// names is only said when there is some of it.
	var raised, work []string
	if answer.Comments > 0 {
		raised = append(raised, counted(answer.Comments, "Comment", "Comments"))
		work = append(work, "Comments")
	}
	if answer.Answers > 0 {
		raised = append(raised, counted(answer.Answers, "Answer", "Answers"))
	}
	if answer.Unanswered > 0 {
		raised = append(raised, counted(answer.Unanswered, "unanswered question", "unanswered questions"))
	}
	if answer.Answers+answer.Unanswered > 0 {
		work = append(work, "questions")
	}
	// Every question answered and nothing else raised is the one Hand Off the
	// agent may end the review from: only it can tell whether an Answer calls
	// for a change (ADR-0017).
	if answer.Comments == 0 && answer.Unanswered == 0 {
		return fmt.Sprintf("%s was handed off with %s. Call fetch_results with review_id %s and read them: conclude if no Answer calls for a change, otherwise post a Revision Round.",
			review, listed(raised), id)
	}
	return fmt.Sprintf("%s was handed off with %s. Call fetch_results with review_id %s, work the %s, then post a Revision Round.",
		review, listed(raised), id, listed(work))
}

// counted says how many of something there are, in the singular for one.
func counted(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// listed reads a list as a phrase: "a", "a and b", "a, b and c".
func listed(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

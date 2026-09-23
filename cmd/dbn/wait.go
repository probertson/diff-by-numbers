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

	answer, err := daemon.AwaitHandover(waitContext(), daemonURL(*port), id, waitRetry)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, handoverLine(answer))
	return nil
}

// handoverLine is the one line a wait ends with: what happened, and what to
// call next. It never carries the results — fetch_results stays their only
// source, and it is what re-grounds the agent (ADR-0008).
func handoverLine(answer daemon.WaitWire) string {
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
	comments := fmt.Sprintf("%d Comments", answer.Comments)
	if answer.Comments == 1 {
		comments = "1 Comment"
	}
	return fmt.Sprintf("%s was handed off with %s. Call fetch_results with review_id %s, work the Comments, then post a Revision Round.", review, comments, id)
}

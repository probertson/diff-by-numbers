# The harness waits for the Reviewer, not the agent

When a Review changes hands, the Authoring Agent is told directly rather than through the
Reviewer. It posts, starts `dbn wait <review_id>` as a background command, and ends its turn.
The command blocks until the Reviewer hands off or dismisses the Review, then exits with one
line naming what happened and what to call next. The harness wakes the agent when a
background command exits, so the agent's next turn begins with that line.

ADR-0008 and ADR-0011 stand: the agent posts once and does not wait. What waits is a process
the harness owns, outside any turn. Nothing blocks the agent's conversation, and the
Reviewer's backward navigation is untouched.

## Why a background command

No external process can start a turn in an idle Claude Code session: MCP server messages do
not, Channels needs a launch flag, and there is no `claude message`. What does wake an idle
session is something it started finishing. A spike on #22 confirmed that for both a background
subagent and a background shell command.

- **A background subagent parked on a blocking MCP tool** was the spike's first design. It
  spends a subagent's context and tokens for the whole wait, which can be hours. It also has to
  long-poll around MCP's idle timeout, and each "not yet" costs another turn of the subagent.
- **Monitor** expires within 30 minutes and needs a turn to re-arm, so it wakes the agent for
  nothing every half hour.
- **A background command** costs nothing while it waits and is subject to no MCP timeout. Its
  retrying lives in Go, in a command dbn owns and tests, rather than in a subagent's prompt.
  It needs a shell permission, which one allow rule covers.

## What ends the wait

`dbn wait` returns on four events, and only those:

- a Hand Off with Comments,
- a Hand Off with nothing raised,
- a Dismissal,
- a daemon that answers but does not know the id, because it restarted and lost its
  in-memory state.

Everything else, such as the Reviewer opening the Review, moving between Steps or raising a
Comment, is the Reviewer working, and nothing the agent can act on. An unreachable daemon
is retried, not reported. Reporting the unknown id matters as much as the other three: without
it, a restart would leave the wait retrying forever against an id that will never come back.

The line it prints names the event and the next call. It does not carry the results:
`fetch_results` stays their only source, it already re-grounds the agent (ADR-0008), and in the
concluded and dismissed cases the fetch is what releases the Review.

## Harness-neutral dbn, capability-conditional skill

This works wherever a harness can run a command in the background and wake the agent when it
exits. Claude Code can. For other harnesses, nothing changes: the Reviewer tells the agent,
and it fetches.

dbn stays harness-neutral. The post result carries a `wait_command`, composed by dbn with the
review id and any non-default port. The relay `message` does not change. The `dbn-review` skill
runs the command only when the harness can background it. The skill is not specific to one
harness, so it states the capability rather than naming a product, and it warns never to run
the command in the foreground, where it would block the turn until the Reviewer hands off.

## Consequences

The daemon tracks which Reviews have a live waiter. The handed-off screen says the agent has
been told when a waiter received the event, and to tell the agent otherwise. The Reviewer
stops relaying only if the screen can be trusted, and it can be trusted only if it reports
what actually happened.

A waiter dies with the harness session that started it. The screen then falls back to
"tell your agent", which is correct: the agent is not listening.

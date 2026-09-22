# An Inbox of reviews, each addressed by its id

The daemon holds several reviews at once. The Reviewer sees them in an **Inbox**, picks one,
and can leave it mid-review for another and come back to find it where it was left. Every
call about a review, from the Authoring Agent or from the TUI, names the review by its id.

This resolves the restriction the MVP left in place, where one daemon held one Walkthrough
and a second session's post was refused as `walkthrough_active` until the first was over.

## Why an Inbox

Several agent sessions running in parallel is the real workflow, and it is why dbn is a
long-lived HTTP server rather than a stdio subprocess. Two alternatives were weighed.

- **Process per session**, one daemon and one window per agent. Ruled out by the topology
  ADR-0011 already chose: one fixed port and one MCP registration for every session.
- **Queue**, which accepts every post but shows one review at a time. Its original objection,
  that a waiting session goes quiet, no longer holds: since ADR-0011 the agent does not
  wait, it posts and ends its turn. What still holds is that a queue cannot let the Reviewer
  take the quick review first or set a long one aside, and that was the point.

The Inbox also costs less than it looks. A `Session` already keeps everything per review:
position, the Steps seen, Comments, the round and its snapshot. Multiplexing is mostly a map
of Sessions keyed by id, not new concepts.

## Every call names its review

When there was one review, "the review" was implicit. A post after a Hand Off became a
Revision Round of it, and `fetch_results` took no arguments. With several open, implicit
meant nothing, so:

- A post without an id starts a new review. A Revision Round names the review it continues,
  alongside the `replaces` a Replacement already names.
- `fetch_results` and `describe_changes` take the id. `describe_changes` needs it because in
  a Revision Round what it pre-marks depends on which review it is describing.

Inferring the review was rejected both ways it could be done. From the MCP connection: a
restart, a resumed harness session or a reconnecting shim gives a new connection, and the
review is silently orphaned. From the Change Set: two sessions in one checkout collide, and
a review that adds a repository in a later round stops matching. An explicit id is the only
answer that survives restarts and compaction without guessing.

An agent that has lost its id, usually to compaction, finds out at `fetch_results`, which
it must call before it can write a Revision Round's dispositions. Called without an id, it
is refused, and the refusal lists the open reviews by id, label and state. That is the whole
recovery mechanism. dbn does not refuse a new review over a repository that already has one
open. An agent working a Revision Round has always fetched first, so it cannot get as far as
posting without its id.

`label` becomes required on a new review, since it is what tells rows apart. A Revision
Round or a Replacement may still omit it and keep the one first given.

## The window holds which review is open

The daemon holds each review's own Reviewer state, as before. Which review is on screen
belongs to the TUI window, and every Reviewer endpoint takes a review id. A daemon-held
"focused review" was rejected: it would bring back the single-slot "the review" on the
Reviewer's side that explicit ids removed on the agent's. It would also make two windows
move each other around.

## What the Reviewer sees

- The Inbox is always the TUI's home, including when it holds one review or none. An empty
  Inbox replaces the waiting screen. A screen that depended on how many reviews happened to
  be open would put the same keypress somewhere different from one minute to the next.
- It lists every review not yet concluded, including those handed off with Comments,
  marked *waiting on agent*. That row is the one most easily forgotten, and the one most
  likely to show an agent that went quiet.
- Rows group by whether they need the Reviewer, *new* and *needs you* first, oldest first
  within a group. The cursor follows the review rather than the row index, so a row that
  arrives or moves never changes what Enter opens.
- Nothing announces an arrival mid-review. Every post is one the Reviewer asked for, so
  they know it is coming.
- A Hand Off never moves the Reviewer. Handed off with Comments, the screen stays, and the
  Revision Round replaces it when it arrives. Handed off with nothing raised, the completion
  screen stays until the daemon releases the review, then returns to the Inbox.

## Dismissal

The Reviewer can dismiss any review from the Inbox, after an inline confirmation, since it
discards the Comments raised. This replaces the id-less `dbn abandon`. The daemon keeps a
tombstone for the id: the agent's next `fetch_results` reports that the Reviewer dismissed
the review, with any Comments already raised, rather than the "no such review" a typo would
get, which would invite the agent to post it again. Once reported, the tombstone is
released. #22 is expected to push this to the agent rather than wait for it to ask.

## Consequences

State stays in memory, and a daemon restart now loses every open review instead of one.
Durability is left to #3, which should key its state by review id rather than by
repository and branch. `dbn update` treats any open review as busy, with `--force` as the
override, as it does for one today.

A concluded review leaves the Inbox but stays in memory until the agent has fetched its
results or called `conclude`, so a late `fetch_results` still gets an answer.

The comment on `Session` saying concurrent Walkthroughs are out of scope "until the
multiplexed inbox exists" is resolved by this ADR. The code's `Abandon` is renamed to
match **Dismissal**.

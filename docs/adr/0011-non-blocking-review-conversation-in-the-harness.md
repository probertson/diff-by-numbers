# The agent does not block; conversation happens in the harness

The Authoring Agent posts a Walkthrough and ends its turn. dbn is a reading surface and a
channel for anchored, batched Change Requests. Questions and discussion happen in the agent's
own chat interface, not in dbn.

An earlier design had the agent block in a long-lived call while the Reviewer worked, with
questions answered live inside dbn. That was rejected once the premise was examined: two
terminals where one sits idle is one terminal with extra steps. The value of a separate
window was never "side-by-side" — it is a dedicated, full-screen, keybindable surface that
does not scroll away, and that benefit is entirely independent of whether the agent waits.

A TUI input box cannot match a chat interface at multi-turn discussion, follow-ups, or
readable history, so conversation belongs where conversation is good.

## What dbn contributes instead

The expensive part of asking a question during review is not the asking — it is the
*pointing*: "in this repo, this file, this line, the code that says `foo`". dbn eliminates
that, because it knows exactly what the Reviewer is looking at. Selecting a line range
composes an **Anchor** — repository, file, line numbers, Step name, and the code itself —
self-contained enough to survive context compaction.

This reverses an earlier decision to anchor coarsely, which was argued on the grounds that
"with a live conversation channel, feedback is about an idea, not a character offset". That
reasoning was backwards: precision is what makes the asking cheap, so line-range selection is
core rather than deferred.

## Consequences

ADR-0004 stops being physically enforced. Nothing prevents the agent editing files
mid-Walkthrough except instruction; per-file staleness detection is the backstop, so a
violation is noisy rather than silent.

The daemon's lifetime is no longer bound to a review, so it is separated from the TUI and run
always-on as a login agent. This is not merely tidiness: Claude Code connects to configured
MCP servers at session start, so an on-demand daemon means every session in every repo
reports a failed server on days you do not review. It is also the shape the multiplexed
inbox needs.

Deferred and recorded as an issue: one-shot answers returning into dbn beside the code, with
deeper threads escalating to chat.

# The agent posts a Walkthrough once, then fetches results when told

The Authoring Agent submits the entire Walkthrough in a single call — Brief, every Step,
every Excerpt range, every explanation — and then ends its turn. It does not make a call per
Step, and it does not wait. When the Reviewer has finished, they say so in chat and the agent
fetches the results.

The obvious design is a blocking call per Step, the agent driving the Reviewer forward one
stop at a time. That is wrong, because the Reviewer must be able to move *backward* —
revisiting an earlier change once a later one has given it meaning. Since dbn already holds
the whole plan and reads the code from the working tree itself, it can re-render any Step
with no agent involvement at all. Navigation is purely local and instant.

## Consequences

The agent must plan the whole Walkthrough up front and cannot adapt it Step by Step as the
review unfolds — which is also what lets the Brief list every Step before any code is shown.

Because the agent's context may have moved on, or been compacted, by the time the Reviewer
finishes, the results call returns the **whole Walkthrough** alongside the Change Requests so
the agent can re-ground itself rather than assuming it still remembers what Step 3 was.

Anyone proposing per-Step calls is removing backward navigation, whether or not they realise
it.

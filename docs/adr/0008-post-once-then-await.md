# The agent posts a Walkthrough once, then waits

The Authoring Agent submits the entire Walkthrough in a single call — Brief, every Step,
every Excerpt range, every explanation — and then blocks in one `await` call that returns
only when the Reviewer asks a question, asks for a Step to be re-planned, or finishes.
It does not make a call per Step.

The obvious design is the opposite: a blocking call per Step, the agent driving the Reviewer
forward one stop at a time. That was the original plan and it is wrong, because the Reviewer
must be able to move *backward* — revisiting an earlier change once a later one has given it
meaning. Since dbn already holds the whole plan and reads the code from the working tree
itself, it can re-render any Step with no agent involvement at all.

So navigation is purely local and instant, and the agent is needed only for interaction.

## Consequences

Fewer round trips, no agent latency between Steps, and free jump-to-any-Step navigation. In
exchange the agent must plan the whole Walkthrough up front and cannot adapt it Step by Step
as the review unfolds — which is also what makes the Brief able to list every Step before
any code is shown.

Anyone proposing per-Step calls is removing backward navigation, whether or not they realise
it.

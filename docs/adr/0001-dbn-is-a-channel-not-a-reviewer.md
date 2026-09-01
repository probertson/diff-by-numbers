# dbn is a channel, not a reviewer

dbn presents changes and carries feedback; it forms no opinion about code and never
decides what the Reviewer sees. The Authoring Agent — the agent that wrote the code —
plans the Walkthrough, because it alone knows what was asked for, and reconstructing
intent from a diff is the very problem this tool exists to remove.

The cost is accepted deliberately: the Authoring Agent is not independent, and will
explain its own bug as fluently as its own good idea. dbn therefore improves *"does
this match what I asked for"*, not *"is this correct"*. Any proposal to make dbn
analyse, score, or filter the code is a reversal of this decision, not an extension
of it.

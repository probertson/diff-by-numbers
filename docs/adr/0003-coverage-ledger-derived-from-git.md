# The Coverage Ledger is derived from git, not from the agent

dbn independently derives the set of Changed Lines from git and tracks which have
been shown by some Excerpt. A Walkthrough cannot be completed while Changed Lines
remain unshown. This is the single deliberate exception to ADR-0001: the Authoring
Agent chooses what the Reviewer sees, except that it cannot choose to show nothing.

Without it the failure mode is silent and lands exactly on the agent's blind spots —
it omits the line it did not think interesting, and the Reviewer never learns there
was anything to omit.

The ledger constrains *completeness only*, never ordering, grouping or size. Steps
may repeat a line freely (the interface in Step 1, its callers in Step 4). A general
principle follows: no mechanism in dbn may force an arbitrary boundary, because
arbitrary boundaries are the thing the tool exists to eliminate. This is why Step
size is a soft budget requiring justification rather than a hard cap.

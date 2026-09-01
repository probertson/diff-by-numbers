# Revision Rounds are scoped by pre-marking the Coverage Ledger

A Revision Round re-derives the *full* change set for the branch and then pre-marks as
shown every Changed Line whose content is unchanged since the previous Walkthrough. The
Coverage Ledger then points the Authoring Agent at exactly what moved, and its existing
enforcement guarantees every fix is shown.

The obvious alternative — snapshotting the tree at finish and diffing against it — was
rejected because it introduces a second notion of "the changes under review" alongside the
one in ADR-0003, with its own snapshot format and its own failure modes. Pre-marking adds
no new concept: it is the ledger doing its existing job from a different starting state.

## Consequences

Revision Rounds work only within a single dbn process, because the previous Walkthrough and
its per-line content are held in memory. Surviving a restart needs durable state, which is
out of scope for the MVP. Restarting dbn mid-review means starting the review over.

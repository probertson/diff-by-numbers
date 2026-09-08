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

Pre-marking is by content, and only content that is *unique* in both rounds is pre-marked: a
line whose text appears more than once in a file cannot be matched to a specific line, so
matching it would risk pre-marking a genuinely new line whose text collides with an old one
and letting it escape coverage. Duplicated content is therefore re-demanded rather than
pre-marked — conservative, never a hole.

Two kinds of change carry no content dbn can compare across rounds, so neither is ever
pre-marked and both are re-demanded every round: **old-side removals** (their content is not
in the working tree) and **Opaque Changes** (a binary file, a mode change, a rename have no
lines, and dbn cannot tell a re-touched binary from an untouched one without hashing it).
The alternative — pre-marking them on a proxy key like the file path — would silently exempt
a re-touched binary or a re-deleted-differently region from review, which is exactly the
escape the guarantee exists to prevent. Re-demanding them is the safe choice; making them
pre-markable would need content dbn does not yet capture.

# Revision Rounds are scoped by pre-marking the Coverage Ledger

> **Superseded in part by [ADR-0014](0014-revision-rounds-scoped-by-round-snapshots.md).**
> Pre-marking itself stands, and so does the full re-derivation and the in-memory lifetime.
> What ADR-0014 replaces is *how* a line is recognised as already read: matching is now
> positional, through a diff of the two rounds' working-tree snapshots, not by line content.
> That retires the uniqueness rule described below, the limitation that Opaque Changes can
> never be pre-marked, and the open follow-up in the #23 amendment — old-side lines are now
> pre-marked too. ADR-0014 also explains why the tree-snapshot alternative rejected below
> was reconsidered: content identity disqualified 42% of the Changed Lines in a measured
> real review, because blank lines and closing braces are never unique.


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

## Amendment (#23): old-side content is now readable

The claim above that "old-side removals carry no content dbn can compare across rounds" no
longer holds: #23 reads the before-side from git's object store at the merge-base, which is
stable across rounds, so a removed line's content *is* now available to compare. Pre-marking
old-side removals by content is therefore feasible and is a natural follow-up; it was left
out of #23 to keep that change to presentation. Until it lands, old-side removals are still
re-demanded every round — conservative, never a hole. Opaque Changes are unaffected: they
still carry no lines to compare, so they remain re-demanded.

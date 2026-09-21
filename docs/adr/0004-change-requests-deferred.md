# Change Requests are collected, not applied mid-Walkthrough

> Now reads "Comments deferred": Change Requests were renamed **Comments**, which may also be
> questions (ADR-0013). The decision below is unchanged.

Questions are asked in the harness chat and change nothing. Change Requests are attached to
their Steps and acted on only once the Walkthrough ends, then reviewed in a fresh
Revision Round over just the resulting changes.

The reason is not preference but invalidation: if the Authoring Agent edits files at
Step 3, every Excerpt in Steps 4..N is potentially stale — line ranges shift and the
plan may no longer be true. The Walkthrough would move under the Reviewer precisely
when they are trying to hold it in their head.

The accepted cost is that a fix never lands while the context is hot. Anyone
proposing a fix-it-now mode must first solve re-planning the remaining Steps against
a moved working tree.

## Amendment (#72): the viewer shows the posted snapshot; a mid-review edit is flagged, not hidden

Deferring Comments keeps the agent from editing mid-Walkthrough by instruction only
(ADR-0011), and the Reviewer can edit files too. dbn used to answer an edit by hiding the
Step's code behind "out of date" and refusing any Anchor into the file, since the
explanation might no longer describe what was on disk. That lost whatever the Reviewer still
had to say about the code. Allowing it without changing anything else would have been
worse: an Anchor re-read the file to quote it, and so paired the line numbers the Reviewer
selected with whatever had since moved into them.

Every round's code is now read from its Round Snapshot, the tree taken when the Walkthrough
was accepted, and never from disk. The before-side is still read from the merge-base the
Change Set was derived from. What the Reviewer sees, expands and anchors is exactly what was
posted, however the files move afterwards, so the guarantee this ADR makes about a
Walkthrough not moving under the Reviewer now holds on screen as well as in intent. A Step
whose file has since changed can go on showing its code, because the explanation was
written against exactly that snapshot.

The snapshot is taken before the post is checked, and the check reads it, so every
Excerpt an accepted Walkthrough names is one its own snapshot can show. That is as far as
it goes: the Change Set is still derived from the working tree a moment later, so an edit
landing between the two can leave the ledger describing a file slightly different from the
one on screen. Closing that, along with what to do when git cannot snapshot a repository
at all (its code is read from disk, as before), is left open in #104.

An edit is flagged instead. A file whose content no longer matches its blob in the
snapshot is *changed on disk*: its Step, or the Acknowledgement expansion showing it,
carries a warning that the Reviewer is seeing the posted version, and an Anchor into it says
its line numbers are from the posted version, so the agent knows they are historical. That
warning is now the backstop ADR-0011 relies on. The snapshot lives in memory with the
review, so a daemon restart loses both together, which adds no new failure mode.

# Change Requests are collected, not applied mid-Walkthrough

Questions are answered live and change nothing. Change Requests are attached to
their Steps and acted on only once the Walkthrough ends, then reviewed in a fresh
Revision Round over just the resulting changes.

The reason is not preference but invalidation: if the Authoring Agent edits files at
Step 3, every Excerpt in Steps 4..N is potentially stale — line ranges shift and the
plan may no longer be true. The Walkthrough would move under the Reviewer precisely
when they are trying to hold it in their head.

The accepted cost is that a fix never lands while the context is hot. Anyone
proposing a fix-it-now mode must first solve re-planning the remaining Steps against
a moved working tree.

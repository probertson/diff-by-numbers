# Revision Rounds are scoped by diffing round snapshots

A Revision Round still re-derives the *full* Change Set and pre-marks what the Reviewer has
already read, exactly as ADR-0007 describes. What changes is how "already read" is decided.

Every accepted Walkthrough now records each repository's working tree as a git tree object.
The next round is scoped by diffing the two trees: a Changed Line is pre-marked when it maps
to a line that was a Changed Line in the previous round, on the same side, and nothing
touched it in between. Matching is positional, not textual.

This **supersedes ADR-0007 in part**: its content-identity rule and its uniqueness
requirement, and its limitation that an Opaque Change can never be pre-marked. Everything
else in ADR-0007 stands — the full re-derivation, the pre-marking mechanism itself, and the
in-memory lifetime.

## Why this reverses ADR-0007

ADR-0007 considered this design and rejected it:

> The obvious alternative — snapshotting the tree at finish and diffing against it — was
> rejected because it introduces a second notion of "the changes under review" alongside the
> one in ADR-0003, with its own snapshot format and its own failure modes.

That reasoning was sound and the conclusion did not survive contact with real reviews. Content
identity requires a line's text to be unique within its file, in *both* rounds, or it cannot be
matched to a specific line. In a braced language most lines are not unique. Measured on a real
review of this repository (8 commits, 1514 new-side Changed Lines), **637 — 42% —** were in a
non-unique group, and that is a floor. The most frequent contents were a blank line, `\t}` and
`}`. A round in which the agent moved almost nothing was nearly as large as the first, and it
was forced to hide eighteen already-reviewed files behind a single Acknowledgement — the exact
thing the `dbn-review` skill warns against.

The objection is also narrower than it looked. A tree object is not "a second notion of the
changes under review": it is not a Change Set and nothing is derived from it. It answers one
question — *did this line move since the last round* — and the Change Set is still derived
exactly as ADR-0003 says, from git, once per round. The snapshot format is git's own, and dbn
reads it with the same `git diff` it already uses.

What the old rule bought was safety: content that appears twice cannot be matched, so a
genuinely new line whose text collides with an old one must never escape coverage. Positional
matching keeps that property outright rather than by exclusion. There is no content to collide,
so the safe answer and the useful answer are the same one.

## How

The git adapter gained two capabilities, which the core reaches through an optional interface
so it holds no git knowledge of its own:

- **Snapshot.** Copy the real index to a temporary file, `git add -A` into the copy, and
  `git write-tree`. This is the same throwaway-index mechanism untracked-file derivation uses
  (#79), so untracked files are in the snapshot and ignored files are not. The real index is
  never written, and the resulting tree is unreferenced, so git's gc collects it in time —
  the same as anything `git stash create` leaves behind.
- **MapBetween.** `git diff --unified=0 -M <from> <to>`, parsed into a line mapping. A line
  inside a hunk was touched and has no counterpart; anything else maps through the accumulated
  shift, following renames.

The new side maps through the round-over-round snapshot diff. The old side consists of
positions in the merge-base, which the working tree says nothing about: while the base is the
same commit those lines have not moved, so the mapping is the identity, and when the branch was
rebased underneath the review the two bases are diffed exactly as two snapshots are. That is
what absorbs #66, where old-side lines were never pre-marked at all because a removed line has
no working-tree text to read.

An Opaque Change has no lines to map. It counts as already read when its file is absent from
the round-over-round diff — identical path, blob and mode — and it was in the previous round's
Change Set. Before this, every round re-Acknowledged every binary.

The snapshot is taken before validation but kept only when the post is accepted, so a rejected
post still changes nothing. If a snapshot or a mapping fails, nothing is pre-marked: the round
demands everything, which is the same conservative answer a daemon restart already gives.

## Consequences

Revision Rounds remain in-memory and still do not survive a restart — the tree objects are
durable in git, but the Session that knows which ones matter is not. ADR-0007's consequence
stands unchanged.

Pre-marking now depends on git being able to write a tree, so it needs a readable index. A
repository whose index is missing fails derivation outright rather than being scoped wrongly.

A line the agent reverted to its previous text, having edited it in between, is now demanded
where content matching would have pre-marked it. That is the correct answer — the Reviewer
last saw it in the previous round and the round-over-round diff says it moved — but it is a
behaviour change in the conservative direction.

**Lines withdrawn between rounds** — added in round 1, gone in round 2 — are in neither round's
Change Set, so nothing shows them. That gap is not new, and it is #44's to close using these
same snapshots.

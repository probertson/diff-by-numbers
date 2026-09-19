---
title: "[TUI] Prompt for the Comment list on the conclusion screen"
label: CL-3
id: "{to be added}"
test-first: true
checkpoint: both
---
## Context

Part of [[TUI] List Comments from the conclusion screen](requirements.md). See
[the breakdown](breakdown.md) for how the epic is split, and for the agreed conclusion screen
layout that this story's line fits into.

CL-2 makes `l` work on the conclusion screen. This story tells the Reviewer about it: when they
have raised at least one Comment, the conclusion screen shows a line inviting them to look
over what they raised before handing off.

Covers the requirements' requirement 1 (with 1.1 and 1.2) and 5.1.

**Depends on:** CL-2. Also lands after #34 (the Comment rename). The line tells the Reviewer to press `l`, which only works on the
conclusion screen once CL-2 lands.

## Open Questions

None at story-write time — all known decisions are resolved or captured in Acceptance Criteria.

## Pre-flight findings

- **This story lands after #34**, which renames Change Request → Comment in code and UI (`ChangeRequest` → `Comment`, "Change Request updated" → "Comment updated", and so on). Names and strings below use the new term; `file:line` citations were taken before #34 and may have shifted by a few lines.
- **Today's conclusion screen** is built by `conclusionView` [1]: a summary sentence ("You raised
  N Comments across M Steps."), a blank line, then "Press h to hand off to your agent." in
  the accent style. Its test is `TestConclusionViewShowsTheSummaryAndHandOffCTA` [2].
- **#74 and #75/#80 add lines to this screen.** The breakdown's "Conclusion screen layout" fixes
  their order: the count line (#74), then the declines hint (#75/#80, only with declines), then
  this story's line (only with Comments), then the hand-off line. Whichever lands later
  fits into that order. This story's line always goes directly above the hand-off line.
- **The count comes from the refreshed view** (`len(m.view.Comments)`) [1], so after the
  Reviewer withdraws the last Comment from the list and returns, the line is already gone.
  No extra refresh is needed.
- `pluralize(n, noun)` [3] adds an `s` for any count other than 1, but it also prints the number.
  This line has no number, so it needs the noun only, singular for exactly 1.

**References**
- [1] `internal/tui/tui.go:1340`
- [2] `internal/tui/tui_test.go:525`
- [3] `internal/tui/tui.go:1675`

## Test seams

The epic's single seam: the model-level unit tests in `internal/tui/tui_test.go`, calling
`conclusionView()` on a `model` with a given view and asserting on the output (see
`TestConclusionViewShowsTheSummaryAndHandOffCTA` [1] for the shape). `test-first` is `true`: each
assertion below can be written before the change. The first two rows fail today; the last two
("With none, no line" and "The line disappears after the last withdrawal") are regression guards
that already pass, since today's screen has no such line.

| Acceptance | Test |
|---|---|
| With two Comments, the plural line shows between the summary and the hand-off line | `conclusionView()` contains the plural literal, positioned after the summary and before "Press h to hand off" |
| With one, the singular line shows | `conclusionView()` contains the singular literal |
| With none, no line | `conclusionView()` contains no "Press l" text |
| The line disappears after the last withdrawal | a model whose view goes from one Comment to none: the line is gone from `conclusionView()` |

How the line looks in a real terminal isn't observable at this seam. Checkpoint 2 covers it.

**References**
- [1] `internal/tui/tui_test.go:525`

## Scope of Work

On the conclusion screen, when the Reviewer has raised at least one Comment, show one line
directly above the hand-off line, separated from the lines around it by a blank line like the
existing lines:

> `Press l to see your Comments.`

With exactly one Comment:

> `Press l to see your Comment.`

- The line is left out entirely when no Comments have been raised.
- Style: plain text, the same as the declines hint (#75/#80) and today's summary line. Not the
  accent style of the hand-off line, which stays the only accented action on the screen, and not
  the accent-and-bold #74 gives its count line.

## Acceptance Criteria

### Summary

In dbn's terminal viewer, the conclusion screen (reached by advancing past the last Step of a
review) shows the line "Press l to see your Comments." above "Press h to hand off to your
agent." whenever the Reviewer has raised at least one Comment ("Comment" when exactly one), and
no such line when they have raised none.

### Prerequisites

- A build of dbn from this branch: `go build -o ./dbn ./cmd/dbn` at the repo root.
- The dbn daemon running from that build (`./dbn serve`), with an agent-posted Walkthrough of at
  least two Steps and exactly two Comments raised on it.

### Verification steps

1. At the repo root, run `./dbn` in a terminal to open the review.
2. Press `→` repeatedly until you pass the last Step and the conclusion screen shows (its header
   starts "dbn — End of review").
3. Press `l`, move to a Comment, press `d`, then `y` to confirm, then press `esc`.
4. Press `l`, withdraw the remaining Comment the same way, then press `esc`.

### Criteria

- After step 2, the conclusion screen shows `Press l to see your Comments.` on its own line, with
  a blank line above it and a blank line below it, directly above
  `Press h to hand off to your agent.`
- After step 3, the conclusion screen shows `Press l to see your Comment.` in the same place.
- After step 4, the conclusion screen shows no line starting "Press l", and
  `Press h to hand off to your agent.` is still there.

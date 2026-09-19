---
title: "[TUI] Editing a Comment from the list returns to the list"
label: CL-1
id: "{to be added}"
test-first: true
checkpoint: both
---
## Context

Part of [[TUI] List Comments from the conclusion screen](requirements.md). See
[the breakdown](breakdown.md) for how the epic is split.

The Comment list lets the Reviewer open a Comment in the edit screen. Whatever the
Reviewer does there (save, cancel, or delete), the TUI drops them back on the Step view, not the
list they were working through. This story makes the edit screen return to where it was opened
from. It is visible today from any Step's list, and CL-2 relies on it so that editing from the
conclusion screen's list doesn't send the Reviewer back to a Step.

Covers the requirements' requirements 4 and 6 (with 6.1 and 6.2), and Decision 6.

**Depends on:** #34 (the Comment rename) landing first. No other story.

## Open Questions

None at story-write time — all known decisions are resolved or captured in Acceptance Criteria.

## Pre-flight findings

- **This story lands after #34**, which renames Change Request → Comment in code and UI (`ChangeRequest` → `Comment`, "Change Request updated" → "Comment updated", and so on). Names and strings below use the new term; `file:line` citations were taken before #34 and may have shifted by a few lines.
- **The edit screen doesn't know where it was opened from.** All three of its exits set the Step
  view's mode: confirmed delete [1], cancel with `esc` [2], and save with `enter` [3]. The first
  carries the comment "origin (Step or List) is not tracked".
- **It is opened from three places:** `e` on a Step line with exactly one Comment [4];
  `c` on a selection, which composes a new one [5]; and `e` / `enter` in the list [6]. Only the
  last is the list origin. The list itself is also opened in two forms: the full list (`l`/`L`)
  and the filtered list (`e` on a Step line with more than one Comment) [7]. Both are
  "the list" for this story.
- **Status messages would leak.** Save and delete set `m.status` ("Comment updated",
  "could not update the Comment", "Comment deleted") [1][3]. `m.status` is only
  drawn in the Step view's stateful row [8], so on the list path the message would stay unseen
  and then appear on the next Step the Reviewer returns to.
- **The list's own delete steps the cursor back** [9]. A delete made in the edit screen doesn't,
  so returning to the list after one could leave `crCursor` [10] past the end of the list.
- The model is `model` [11]. The modes are an `int` enum [12].

**References**
- [1] `internal/tui/tui.go:716`
- [2] `internal/tui/tui.go:729`
- [3] `internal/tui/tui.go:756`
- [4] `internal/tui/tui.go:652`
- [5] `internal/tui/tui.go:680`
- [6] `internal/tui/tui.go:812`
- [7] `internal/tui/tui.go:661`
- [8] `internal/tui/tui.go:1041`
- [9] `internal/tui/tui.go:779`
- [10] `internal/tui/tui.go:119`
- [11] `internal/tui/tui.go:102`
- [12] `internal/tui/tui.go:157`

## Test seams

The epic's single seam: the model-level unit tests in `internal/tui/tui_test.go`, which build a
`model`, call `updateList` / `updateNote` with key messages, and assert on `mode`, `status` and
`crCursor` (see `TestArmedListDeleteConfirmedWithdrawsAndStaysInList` [1] for the shape).
`test-first` is `true`: each assertion below can be written before the change. All but one fail
today; the row "An edit opened from a Step still returns to the Step" is a regression guard that
passes before and after.

| Acceptance | Test |
|---|---|
| Save, cancel and delete from a list-opened edit return to `modeList` | three tests driving `updateNote` with `enter`, `esc` and the confirmed `ctrl+d` delete |
| The filtered list keeps its filter across the round trip | the same, starting from a model with an active `crFilter` |
| An edit opened from a Step still returns to the Step | `updateNote` from a Step-opened edit, for all three exits |
| No status message on the list path | `status` is empty after each list-path exit |
| The cursor is clamped after a delete | delete the last item with `crCursor` on it; the cursor lands on the new last item |

The daemon round trip (the edit actually reaching the service and the refreshed list coming back)
isn't observable at this seam. Checkpoint 1 covers it.

**References**
- [1] `internal/tui/tui_test.go:245`

## Scope of Work

1. Record where the edit screen was opened from: the list, or a Step. Set it at every place that
   opens the edit screen (the three in Pre-flight findings).
2. When the edit screen was opened from the list, all three exits (save, cancel, and confirmed
   delete) return to the list instead of the Step view:
   - The list keeps its current form: a filtered list stays filtered on the same line.
   - No status message is set.
   - After a delete, clamp the list cursor so it is on a valid item (or on nothing, when the list
     is now empty and shows its empty state).
3. When the edit screen was opened from a Step, behaviour is unchanged: all three exits return to
   the Step view and set the same status messages as today.

## Acceptance Criteria

### Summary

In dbn's terminal viewer, when a Reviewer opens a Comment for editing from the Change
Request list and then saves, cancels or deletes it, they return to the Comment list (still
filtered to one line, if it was), not to the Step view; editing a Comment directly from a
Step still returns to that Step.

### Prerequisites

- A build of dbn from this branch: `go build -o ./dbn ./cmd/dbn` at the repo root.
- The dbn daemon running from that build (`./dbn serve`), with an agent-posted Walkthrough of at
  least two Steps.
- In that Walkthrough, at least four Comments:
  - two on the same line of one Step (so pressing `e` on that line opens the list filtered to that
    line);
  - one alone on another line;
  - one more on a third line, raised after the other three so that it is the last entry in the
    Comment list. Step 4 deletes it.

### Verification steps

1. At the repo root, run `./dbn` in a terminal to open the review.
2. On any Step, press `l` to open the Comment list. Move to a Comment (`↓` / `j`)
   and press `e`. Change its text and press `enter` to save.
3. In the Comment list, open a Comment with `e` again, then press `esc` to cancel.
4. In the Comment list, move to the last entry (the Comment on the third line, not
   one of the two sharing a line nor the one alone on its line), press `e`, press `ctrl+d`, then
   press `y` to confirm the delete.
5. Press `esc` to leave the Comment list. Go to the Step with two Comments on one
   line, move the cursor to that line, and press `e`. The Comment list opens filtered to
   that line. In this filtered list, press `e` on a Comment, change its text and press
   `enter`; then press `e` and `esc`; then press `e`, `ctrl+d` and `y`.
6. Press `esc` to leave the filtered Comment list. On the line with a single Comment,
   press `e`, change the text, and press `enter`.

### Criteria

- After step 2, the Comment list is showing, with the edited Comment's new text.
- After step 3, the Comment list is showing, with that Comment's text unchanged.
- After step 4, the Comment list is showing without the deleted Comment, and the
  list's selection marker (`▸`) is on the new last entry.
- In step 5, after each save, cancel and delete, the Comment list is still filtered to
  that one line: its title reads "2 comments on <file>:<line>" after the save and the cancel, and
  "1 comment on <file>:<line>" after the delete.
- Each time `esc` leaves the Comment list (at the start of steps 5 and 6), the Step view
  shows no leftover message such as "Comment updated" or "Comment deleted" in the
  row above the footer.
- After step 6, the Step view is showing (not the list), with the message "Comment updated"
  in the row above the footer.

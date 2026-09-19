---
title: "[TUI] Open the Comment list from the conclusion screen"
label: CL-2
id: "{to be added}"
test-first: true
checkpoint: none
---
## Context

Part of [[TUI] List Comments from the conclusion screen](requirements.md). See
[the breakdown](breakdown.md) for how the epic is split.

The conclusion screen is where the Reviewer lands after advancing past the last Step, just before
handing off. Today it ignores `l`, so checking Comments means stepping back into the
Walkthrough. This story lets `l` open the full list there, returns to the conclusion screen when
the list closes, and brings the conclusion screen's footer in line with a Step's.

Covers the requirements' requirements 2 (with 2.1), 3, 5 and 7, and Decisions 3 and 4.

**Depends on:** CL-1. Also lands after #34 (the Comment rename). Requirement 7 ("check, edit and withdraw, then Hand Off without returning to
a Step") needs CL-1's return-to-list behaviour for edits made from this list.

## Open Questions

None at story-write time — all known decisions are resolved or captured in Acceptance Criteria.

## Pre-flight findings

- **This story lands after #34**, which renames Change Request → Comment in code and UI (`ChangeRequest` → `Comment`, "Change Request updated" → "Comment updated", and so on). Names and strings below use the new term; `file:line` citations were taken before #34 and may have shifted by a few lines.
- **The conclusion screen handles its own keys** [1] and has no case for `l`. The Step view opens
  the list on `l` or `L`, clearing any filter and resetting the list cursor [2]. The conclusion
  screen should do the same, including `L`.
- **Leaving the list always goes to the Step view** [3]. The list's exit keys are `esc`, `L` and
  `q`. The list needs to remember whether it was opened from the conclusion screen.
- **The daemon never leaves the last Step while the conclusion screen shows** [4], so no request
  to the daemon is needed for opening or closing the list here. Edits and withdrawals made in the
  list already call the daemon and refresh the view.
- **The conclusion screen's summary comes from the refreshed view** (`m.view.ChangeRequests`) [5],
  so after returning from the list it already reflects edits and withdrawals. No extra refresh is
  needed on return.
- **The footer is hard-coded** as `keybar("← back", "g Overview", "q exit")` [6]. A Step's footer
  is `keybar(m.navHint(), "g Overview", "l list", "h hand off", "q exit")` [7]. `keybar` joins its
  tokens with `  ·  ` (two spaces each side) and makes the spaces inside a token non-breaking [8],
  so the requirements' single-spaced `← back · g Overview · …` is shorthand for the key order, not
  the exact bytes.
- **The quit guard** is armed only in the Step view and on the conclusion screen [9]. In the list,
  `q` closes the list (it's one of the list's exit keys), so it returns to the conclusion screen
  rather than arming the guard.

**References**
- [1] `internal/tui/tui.go:861` — `updateConclusion`
- [2] `internal/tui/tui.go:566`
- [3] `internal/tui/tui.go:790`
- [4] `internal/tui/tui.go:864`
- [5] `internal/tui/tui.go:1343`
- [6] `internal/tui/tui.go:1020`
- [7] `internal/tui/tui.go:1084`
- [8] `internal/tui/tui.go:1550` — `keybar`
- [9] `internal/tui/tui.go:504`

## Test seams

The epic's single seam: the model-level unit tests in `internal/tui/tui_test.go`, driving
`updateConclusion` and `updateList` and asserting on `mode` and `View()` (see
`TestConclusionBackReturnsToTheStep` [1] for the shape). `test-first` is `true`: each assertion
below can be written before the change. All but one fail today; the row "Leaving a list opened
from a Step still returns to the Step" is a regression guard that passes before and after.

| Acceptance | Test |
|---|---|
| `l` and `L` on the conclusion screen open the unfiltered list | `updateConclusion("l")` / `("L")` → `modeList`, with the filter inactive and `crCursor` 0 |
| With no Comments, the list still opens and shows its empty state | the same with an empty `ChangeRequests`; `View()` contains the empty-state text |
| Leaving that list returns to the conclusion screen | `updateList` with `esc`, `L` and `q` → `modeConclusion` |
| Leaving a list opened from a Step still returns to the Step | `updateList("esc")` from a Step-opened list → `modeReview` |
| An edit made from the conclusion screen's list returns to that list, and then to the conclusion screen | chain `updateList("e")`, `updateNote(enter)`, `updateList("esc")` → `modeConclusion` |
| The footer matches | `View()` in `modeConclusion` contains `keybar("← back", "g Overview", "l list", "h hand off", "q exit")` |

The daemon round trip and the terminal rendering aren't observable at this seam. Checkpoint 2
(after CL-3) covers them.

**References**
- [1] `internal/tui/tui_test.go:379`

## Scope of Work

1. On the conclusion screen, `l` and `L` open the Comment list, unfiltered, with the cursor
   on the first item, whatever the number of Comments (with none, the list shows its
   existing empty state).
2. Record that the list was opened from the conclusion screen. When the list closes (`esc`, `L`
   or `q`), return to the conclusion screen. A list opened from a Step (or the Overview) still
   returns to the Step view.
3. Change the conclusion screen's footer to the Step footer's keys, in the same order:

   ```go
   keybar("← back", "g Overview", "l list", "h hand off", "q exit")
   ```

   It's the same whatever the number of Comments.

## Acceptance Criteria

### Summary

In dbn's terminal viewer, on the conclusion screen (reached by advancing past the last Step of a
review), pressing `l` opens the Comment list, leaving the list returns to the conclusion
screen, and the conclusion screen's footer offers, in order: `← back`, `g Overview`, `l list`,
`h hand off`, `q exit`.

### Prerequisites

- A build of dbn from this branch: `go build -o ./dbn ./cmd/dbn` at the repo root.
- The dbn daemon running from that build (`./dbn serve`), with an agent-posted Walkthrough of at
  least two Steps and at least two Comments raised on it, not yet handed off.

### Verification steps

1. At the repo root, run `./dbn` in a terminal to open the review.
2. Press `→` repeatedly until you pass the last Step and the conclusion screen shows (its header
   starts "dbn — End of review").
3. Read the conclusion screen's footer, the bottom row.
4. Press `l`.
5. In the Comment list, press `e` on a Comment, change its text, and press `enter`.
6. Press `esc`.
7. Press `L`, then `q`.
8. Press `l`. In the Comment list, withdraw every Comment: on each, press `d`, then
   `y` to confirm. Then press `esc`.
9. Press `l`, then `esc`.
10. Press `h`.

### Criteria

- In step 3, the footer shows these five entries in this order, separated by `·`: `← back`,
  `g Overview`, `l list`, `h hand off`, `q exit`.
- After step 4, the Comment list shows every Comment raised in the review (not
  filtered to one line).
- After step 5, the Comment list is still showing, with the edited text.
- After step 6, the conclusion screen shows again (header "dbn — End of review"), not a Step.
- In step 7, `L` opens the Comment list, and `q` returns to the conclusion screen (the
  quit warning does not appear and dbn does not exit).
- In step 8, after the last withdrawal the Comment list shows its empty state ("No
  comments to show."), and `esc` returns to the conclusion screen.
- In step 9, `l` opens the Comment list showing "No comments to show.", and `esc` returns
  to the conclusion screen, whose footer still shows the same five entries.
- After step 10, the handed-off screen shows, headed "Review complete".
- At no point in steps 4–10 did a Step show.

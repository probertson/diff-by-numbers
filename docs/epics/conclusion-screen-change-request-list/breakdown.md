---
title: "Breakdown: [TUI] List Change Requests from the conclusion screen"
requirements: requirements.md
---
# Breakdown: [TUI] List Change Requests from the conclusion screen

Breaks down [[TUI] List Change Requests from the conclusion screen](requirements.md) (GitHub
issue #65). Read the requirements first: their Background, Requirements and Decisions are not
repeated here.

## What changed since the requirements were written

Several issues grilled on 2026-09-19 now touch the same screen or the same words.

- **#34 renames Change Request to Comment** on every surface. **This epic lands after #34**
  (decided during story creation), so every story is written in the new vocabulary: the new line
  reads `Press l to see your Comments.` (`Comment` when there is one) and the code uses the new
  names. This supersedes the requirements' Decision 5, which expected #34 to sweep the wording
  afterwards.
- #74 and #75/#80 don't block this epic. Whichever lands second adapts to the layout below.
- **#74 replaces the conclusion screen's summary sentence.** The Comment count gets its own line
  in accent and bold (`3 Comments for your agent`), and with none the screen says
  `No Comments — handing off completes the review.` Requirement 1's "summary line" is that
  line once #74 lands.
- **#75 adds a declines hint** to the conclusion screen in Revision Rounds
  (`The agent declined 2 requests — R to re-raise`). #80 later makes it count only declines not
  yet re-raised.

### Conclusion screen layout (agreed for #65, #74, #75 and #80)

Status first, then the actions, each separated by a blank line. Lines marked "only with …" are
left out when their condition doesn't hold.

```
dbn — End of review · 38/40 changed lines seen

3 Comments for your agent                          ← #74 (accent, bold)

The agent declined 2 requests — R to re-raise      ← #75/#80 (only with declines)

Press l to see your Comments.                      ← this epic, CL-3 (only with Comments)

Press h to hand off to your agent.

← back · g Overview · l list · h hand off · q exit ← this epic, CL-2
```

## Findings

- **Neither the list nor the editor remembers where it was opened from.** Every exit from the
  editor (delete [1], cancel [2], save [3]) and from the list [4] sets the Step view's mode. So
  returning to the right place needs two small pieces of origin state: the list's (Step or
  Overview, versus the conclusion screen) and the editor's (Step, versus the list).
- **A status message would leak.** Save and delete in the editor set a status message [2][3],
  and status messages only show in the Step view. With the editor returning to the list, the
  message would go unseen and then appear later on a Step. The requirements' Decision 6 says
  the list shows none, so the editor must not set one when it returns to the list.
- **The list cursor needs clamping after a delete from the editor.** The list's own delete
  already steps the cursor back [5]. A delete made in the editor and returning to the list does
  not, and could leave the cursor past the end.
- **The conclusion screen handles its own keys** [6] and never sees `l`, which the Step view
  handles as `l` or `L` [7]. The conclusion screen should accept both.
- **Its footer is hard-coded** to back, Overview and exit [8].

**References**
- [1] `internal/tui/tui.go:716`
- [2] `internal/tui/tui.go:729`
- [3] `internal/tui/tui.go:756`
- [4] `internal/tui/tui.go:790`
- [5] `internal/tui/tui.go:779`
- [6] `internal/tui/tui.go:861` — `updateConclusion`
- [7] `internal/tui/tui.go:566`
- [8] `internal/tui/tui.go:1020`

## Stories

### CL-1: Editing from the list returns to the list

Save, cancel and delete in the editor return to the list when the edit began there, for both
the full list and a Step's filtered list. Editing directly on a Step still returns to the Step.

- Add the editor's origin (Step or list), set where the editor is opened: from a Step's line,
  and from the list's `e` / `enter` [1].
- On the list path: set no status message, and clamp the list cursor after a delete.
- Covers requirements 4 and 6 (with 6.1 and 6.2), and Decision 6.
- Visible from any Step's list, so it stands alone within the epic.

**Depends on:** #34 (the whole epic lands after it). No other story.

**References**
- [1] `internal/tui/tui.go:812`

### CL-2: Open the list from the conclusion screen

`l` or `L` on the conclusion screen opens the full, unfiltered list, including with no Comments,
where it shows its empty state. Leaving the list returns to the conclusion screen, which then
reflects any edits and withdrawals. The footer becomes
`← back · g Overview · l list · h hand off · q exit`.

- Add the list's origin (Step view, or the conclusion screen), set when the list opens, and
  honoured by the list's exit.
- The footer change adds `h hand off` as well as `l list` (the requirements' Decision 3).
- Covers requirements 2 (with 2.1), 3, 5 and 7. Requirement 7, "check, edit and withdraw, then
  Hand Off without returning to a Step", needs CL-1 for the edit part.

**Depends on:** CL-1.

### CL-3: The conclusion screen's `l` prompt

Show `Press l to see your Comments.` in the position the layout above gives it: singular with
one Comment, left out with none. It is re-rendered from the refreshed view, so it disappears
when the last Comment is withdrawn from the list.

- Covers requirement 1 (with 1.1 and 1.2) and 5.1.
- Wording follows #34 (see "What changed since the requirements were written").

**Depends on:** CL-2.

## Test seams

One seam for the whole epic: the existing model-level unit tests in `internal/tui/tui_test.go`,
which build a `model`, call `updateConclusion`, `updateList` or `updateNote`, and assert on
`mode` and `View()` (for example `TestConclusionBackReturnsToTheStep` [1] and
`TestArmedListDeleteConfirmedWithdrawsAndStaysInList` [2]). Every story's acceptance is
observable there. No new harness.

**What it cannot observe:** the round trip to a real daemon (an edit or withdrawal reaching the
service and the refreshed view coming back), and how the screen looks in a real terminal. The
verification checkpoints cover both.

**References**
- [1] `internal/tui/tui_test.go:379`
- [2] `internal/tui/tui_test.go:245`

## Verification checkpoints

| Checkpoint | Kind | Follows | Covers |
|---|---|---|---|
| 1 | both | CL-1 | Manual: edit and delete from the full list and from a Step's filtered list, landing back in the list each time, with no status message left over on the Step afterwards. Review: CL-1's commits. |
| 2 | both | CL-3 | Manual: in a real review, reach the conclusion screen, open the list, edit a Comment, withdraw the last one and watch the prompt disappear, then hand off without visiting a Step. Also check the footer, and the screen with zero Comments. Review: CL-2 and CL-3's commits. |

## Designs

No design artifact. The layout above fixes the screen's content and order, the footer copies
the Step footer, and the screen is plain text. See `design/SOURCE.md`.

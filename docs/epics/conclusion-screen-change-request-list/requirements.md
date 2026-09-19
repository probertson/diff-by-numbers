---
title: "[TUI] List Change Requests from the conclusion screen"
id: "#65"
---
## Background

When a Reviewer advances past the last Step of a Walkthrough, the TUI shows the conclusion screen:
a one-line summary of how many Change Requests they raised, and the prompt to Hand Off. It is the
last stop before handing the round back to the Authoring Agent, and the natural moment to look over
what they raised. Today they cannot do that from there: the key that opens the Change Request list
does nothing on the conclusion screen, so checking their Change Requests means stepping back into
the Walkthrough first.

The original ask, from GitHub issue #65, "Support `l` for list Change Requests on conclusion
screen":

> 1 - Prompt:
> Between "You raised N Change Requests..." and "Press h to ..."
>
> If N > 0, add another line (with space between, like the existing lines):
> > Press l to see your Change Requests
>
> Also show it as an option in the footer
>
> 2 - functionality
> The l modal behaves the same as it does anywhere, allowing you to view, edit, and delete change
> requests. When you exit the modal you return to the conclusion screen.

Constraints from the system as it stands:

- The conclusion screen belongs to the terminal viewer alone. Reaching it tells the dbn service
  nothing; as far as the service knows, the Reviewer is still on the last Step [1].
- The conclusion screen is reachable only before Hand Off; after Hand Off the Reviewer is on a
  different screen with its own keys [2].
- The conclusion screen shows "You raised N Change Requests across M Steps." followed by "Press h
  to hand off to your agent." [3] Its footer offers back, Overview and exit, but not the list and
  not Hand Off, though every Step's footer offers both [4][5]. It does not respond to the list key
  [6].
- The Change Request list is a full screen, not an overlay. From it the Reviewer can edit a Change
  Request or withdraw one (after a confirmation) [7]. A Step can also open a filtered form of it,
  showing only the Change Requests on one line [8].
- Leaving the list always returns the Reviewer to the Step view [9]. Saving, cancelling or deleting
  in the edit screen also always returns to the Step view, even when the edit began in the list:
  where the edit started is not remembered [10].
- The list refreshes after every save or delete, and shows an empty-state message once nothing is
  left in it [11].
- The Reviewer-facing term is "Change Request". An open issue (#34) proposes renaming it to
  "Comment" across every surface [12].

**References**
- [1] `internal/tui/tui.go:539` — advancing past the last Step switches the viewer to the conclusion screen without telling the service
- [2] `internal/tui/tui.go:931`
- [3] `internal/tui/tui.go:1340`
- [4] `internal/tui/tui.go:1018`
- [5] `internal/tui/tui.go:1084`
- [6] `internal/tui/tui.go:861`
- [7] `internal/tui/tui.go:769`
- [8] `internal/tui/tui.go:660` — `e` on a line with more than one Change Request
- [9] `internal/tui/tui.go:790`
- [10] `internal/tui/tui.go:716` — "origin (Step or List) is not tracked"; also lines 733 and 756
- [11] `internal/tui/tui.go:1224`
- [12] `CONTEXT.md:119`; GitHub issue #34

## Open questions

None.

## Requirements

1. As a Reviewer on the conclusion screen who has raised at least one Change Request, I see the
   line "Press l to see your Change Requests." between the summary line and the Hand Off line,
   separated from each by a blank line like the existing lines.
   1. The noun agrees with the count: "Change Request" when I have raised exactly one, "Change
      Requests" otherwise.
   2. When I have raised none, the line does not appear.
2. As a Reviewer on the conclusion screen, I can press `l` to open the list of all my Change
   Requests, unfiltered, whatever their count.
   1. With none raised, the list opens and shows its empty state.
3. As a Reviewer on the conclusion screen, I see the footer `← back · g Overview · l list · h hand
   off · q exit`, the same keys in the same order as a Step's footer, whatever the count.
4. As a Reviewer in the list, I can view, edit and withdraw Change Requests the same way whether I
   opened it from a Step or from the conclusion screen.
5. As a Reviewer leaving the list, I return to where I opened it: the conclusion screen if I opened
   it there, the Step if I opened it from a Step.
   1. On returning to the conclusion screen, its summary and the line in requirement 1 reflect any
      edits and withdrawals I made; if I withdrew my last Change Request, the line is gone.
6. As a Reviewer in the edit screen that I reached from the list, when I save, cancel or delete, I
   return to the list, wherever the list itself was opened from.
   1. This holds for the filtered list opened from a Step as well as the full list.
   2. Editing a Change Request directly on a Step, without going through the list, still returns
      me to the Step.
7. As a Reviewer, I can check, edit and withdraw my Change Requests from the conclusion screen and
   Hand Off without ever returning to a Step. This is the success measure: it is observed directly,
   not measured.

## Decisions

1. **After an edit that began in the list, return to the list** (requirement 6). Rejected:
   returning straight to the conclusion screen, which skips the list the Reviewer was working
   through; and keeping today's return to the Step view, which from the conclusion screen would
   drop the Reviewer on the last Step, a detour out of the "check my Change Requests, then Hand
   Off" flow.
2. **Make the return to the list uniform for every place the list opens from** (requirement 6),
   not only the conclusion screen. Rejected: changing only the conclusion-screen path, because the
   ask is that the list behave the same everywhere, and one list with two return behaviours
   contradicts that.
3. **Add Hand Off to the conclusion screen's footer alongside the list** (requirement 3). Rejected:
   adding only the list key, as the ask literally reads, because once the footer carries the list
   the missing Hand Off key reads as an oversight, and matching the Step footer keeps every key in
   the place the Reviewer already knows.
4. **With no Change Requests raised, hide the prompt line but keep the list key working and in the
   footer** (requirements 1.2, 2.1, 3). Rejected: removing the key and its footer entry at zero,
   because a Step's footer always offers the list regardless of count, and a footer that changes
   with the count is less predictable.
5. **Word the new line with "Change Requests", as the ask does** (requirement 1). Rejected: holding
   the wording open until #34 is settled, because the requirements should not decide #34, and
   a rename there sweeps this line along with every other surface.
6. **The list shows no status message after an edit or delete.** Rejected: showing the Step view's
   status messages ("Change Request updated", "could not update the Change Request", and so on) in
   the list, because the refreshed list already shows the current state: an edited note shows its
   new text or its old one, and a deleted Change Request is gone.

## Out of scope

- Re-raising a declined Change Request from the conclusion screen: the ask covers only the list,
  and re-raising belongs to Revision Rounds, not to reviewing what was raised this round.
- Renaming "Change Request" to "Comment": that is GitHub issue #34's decision, for every surface at once.
- Matching the list's filter on repository as well as file: that is GitHub issue #24, a separate defect in the
  filtered list that this work neither causes nor needs.
- Any change to the dbn service or the Authoring Agent's side: the conclusion screen comes before
  Hand Off and belongs to the terminal viewer alone, and editing and withdrawing already exist. So
  there is nothing to roll out, migrate or backfill beyond relaunching the terminal viewer.

## Designs

This needs no designs. The ask specifies the new line's wording and position, the footer follows
the existing Step footer, and the screen is plain text.

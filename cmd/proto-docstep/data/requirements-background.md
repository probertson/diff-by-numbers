---
title: "[TUI] List Comments from the conclusion screen"
id: "#65"
---
## Background

When a Reviewer advances past the last Step of a Walkthrough, the TUI shows the conclusion screen:
a one-line summary of how many Comments they raised, and the prompt to Hand Off. It is the
last stop before handing the round back to the Authoring Agent, and the natural moment to look over
what they raised. Today they cannot do that from there: the key that opens the Comment list
does nothing on the conclusion screen, so checking their Comments means stepping back into
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
- The conclusion screen shows "You raised N Comments across M Steps." followed by "Press h
  to hand off to your agent." [3] Its footer offers back, Overview and exit, but not the list and
  not Hand Off, though every Step's footer offers both [4][5]. It does not respond to the list key
  [6].
- The Comment list is a full screen, not an overlay. From it the Reviewer can edit a Comment
  or withdraw one (after a confirmation) [7]. A Step can also open a filtered form of it,
  showing only the Comments on one line [8].
- Leaving the list always returns the Reviewer to the Step view [9]. Saving, cancelling or deleting
  in the edit screen also always returns to the Step view, even when the edit began in the list:
  where the edit started is not remembered [10].
- The list refreshes after every save or delete, and shows an empty-state message once nothing is
  left in it [11].
- The Reviewer-facing term is "Comment". #34 renamed it from "Change Request" across every
  surface [12].

**References**
- [1] `internal/tui/tui.go:539` — advancing past the last Step switches the viewer to the conclusion screen without telling the service
- [2] `internal/tui/tui.go:931`
- [3] `internal/tui/tui.go:1340`
- [4] `internal/tui/tui.go:1018`
- [5] `internal/tui/tui.go:1084`
- [6] `internal/tui/tui.go:861`
- [7] `internal/tui/tui.go:769`
- [8] `internal/tui/tui.go:660` — `e` on a line with more than one Comment
- [9] `internal/tui/tui.go:790`
- [10] `internal/tui/tui.go:716` — "origin (Step or List) is not tracked"; also lines 733 and 756
- [11] `internal/tui/tui.go:1224`
- [12] `CONTEXT.md:119`; GitHub issue #34


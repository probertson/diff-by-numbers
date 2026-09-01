# The MVP renders diffs itself rather than integrating delta or difftastic

The MVP renders unified diffs directly — +/- colouring, no syntax highlighting, no
side-by-side. Integrating a best-of-breed renderer (delta, difftastic) is deferred,
not rejected.

The non-obvious constraint worth recording: borrowing a renderer costs anchoring
precision. What comes back from an external renderer is styled text, and dbn cannot
reliably map a row of it back to a line of source — so it cannot attach a Change
Request to a place in the code. Owning the render loop is what makes anchoring
possible at all.

This tension is live, not hypothetical. Change Requests and question Anchors both point at
a Reviewer-selected *line range* (ADR-0011), so dbn must know precisely what sits on every
row. Adopting an external renderer would mean giving that up, or reconstructing the mapping
from its output. That is the cost anyone proposing delta or difftastic has to pay.

# v1 renders diffs itself rather than integrating delta or difftastic

v1 renders unified diffs directly — +/- colouring, no syntax highlighting, no
side-by-side. Integrating a best-of-breed renderer (delta, difftastic) is deferred,
not rejected.

The non-obvious constraint worth recording: borrowing a renderer costs anchoring
precision. What comes back from an external renderer is styled text, and dbn cannot
reliably map a row of it back to a line of source — so it cannot attach a Change
Request to a place in the code. Owning the render loop is what makes anchoring
possible at all.

v1 anchors Change Requests at Excerpt granularity, which is coarse enough that this
tension does not yet bite. It will bite the day someone wants both line-level
anchoring and delta's output.

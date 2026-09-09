# Acknowledgements are the Coverage Ledger's escape valve

The Authoring Agent may cover part of the Change Set with an Acknowledgement — a declaration
that a set of changes is mechanical and need not be read line by line, carrying a one-line
reason — instead of an Excerpt. The Coverage Ledger accepts it as accounted for.

Without this, ADR-0003's guarantee makes the tool unusable. An ordinary branch that
regenerates a lockfile, deletes a long file, and touches a binary asset would demand Excerpts
over thousands of mechanical lines, and could never complete at all: a binary file has no
lines to excerpt, so no plan could ever account for it. The guarantee intended to create
trust would instead prevent finishing.

For the same reason the ledger's atom is *Changed Line or Opaque Change*, an Opaque Change
being a change with no line-level representation — a modified binary file, a mode change, a
pure rename with no content delta.

## Consequences

An Acknowledgement is a claim, not a dismissal. It renders as a manifest of files and line
counts, and the Reviewer may expand it into real Excerpts at any time. Every bulk claim is
therefore visible and callable, which is what stops it becoming a way to hide real code —
the risk this mechanism obviously carries.

## Amendment (#23): a deletion may now also be a Step

When this was written a deletion could *only* be an Acknowledgement, because dbn could not
read the old side and so had no lines to show. #23 reads the before-side, so a removal can
now be shown in a regular Step — its removed code rendered as the "before", under the
agent's explanation of why it went. This does not change the valve: the Acknowledgement
still exists for bulk, mechanical removals (a deleted vendored directory), and the agent
chooses per case whether a removal is worth a Step or belongs in an Acknowledgement — the
same judgement it already makes for additions.

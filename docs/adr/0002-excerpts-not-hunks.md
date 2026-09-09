# Excerpts are agent-chosen line ranges; git hunks are not a domain concept

A Step is built from Excerpts — arbitrary contiguous line ranges the Authoring Agent
selects for comprehension — not from git hunks. Hunk boundaries are an artifact of
git's text output format and carry no meaning: a new file arrives as one 300-line
hunk, which is precisely the arbitrary boundary this tool exists to escape. dbn
parses hunks only to determine which lines changed, then discards them.

An Excerpt may include unchanged lines, since showing an untouched caller beside a
changed signature is often the point; dbn renders changed and reference lines
distinctly. Ranges are side-qualified, because deleted lines exist only on the old
side and added lines only on the new.

## Consequences

The Authoring Agent sends *ranges*, never code content. dbn reads the bytes from the
working tree itself. If the agent supplied the code it could paraphrase, tidy or
hallucinate what it is showing — during the one step meant to catch exactly that.
Ranges force fidelity at the cost of the agent being unable to show a synthesised
view; prose explanation carries anything synthetic.

## Amendment (#23): cross-side correspondence is adapter-derived, hunks stay non-domain

Reading the before-side (#23) needs to know which removed lines a shown after-range
replaced, so the git adapter now *retains* the cross-side correspondence of each edit —
its removed range paired with the range that replaced it — in the Derivation. This does
not walk back "dbn discards hunks": git hunks remain an adapter-internal artifact and are
never a domain concept. The agent still authors arbitrary, Side-qualified ranges, never
hunks. The correspondence is derived plumbing the core consumes for two mechanical
purposes only — letting a shown after-side account for the before-side it replaced (the
"point once" behaviour), and interleaving the two into a unified diff — not a boundary the
agent sees or authors against.

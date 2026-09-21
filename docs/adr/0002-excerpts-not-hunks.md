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

## Amendment (#85): dbn may widen a range over whitespace, never narrow one

Agents split a new file into its sections and leave out the blank lines between
them. Those are Changed Lines, so the post was refused for lines nobody needs to
be told to read, and the fix an agent reached for — listing the blanks — made the
Walkthrough worse to read in order to satisfy a ledger. dbn now absorbs a run of
whitespace-only Changed Lines into an Excerpt it touches, at post time, and stores
the widened range.

This is dbn editing what the agent chose to show, which ADR-0001 otherwise
forbids, so the limits are the point. It only ever *widens*: nothing the agent
selected is dropped, so it cannot hide code. It widens only over lines whose
content git says is whitespace, and only over lines nothing else already accounts
for, so it adds nothing the Reviewer would have been shown anyway. And an absorbed
line is genuinely rendered — the alternative, exempting blank lines from coverage,
was rejected because it would count lines as accounted for without ever showing
them, and a blank line can carry meaning in Markdown or YAML.

Whitespace-only lines also stop counting toward the Step budget, wherever they
appear. This does not walk back ADR-0003's "the ledger constrains completeness
only, never ordering, grouping or size": the budget has always been counted from
ledger-derived lines, and what ADR-0003 forbids is a mechanism that *forces* an
arbitrary boundary. Exempting whitespace only relaxes the pressure, and stops
dbn's own widening from being what pushes a Step over.

## Amendment (#86): a rename's source path is an alias for its destination

git attributes every atom of a renamed file to the path it was renamed *to*, so an
Acknowledgement or an old-side Excerpt written about the path the file came *from*
accounted for nothing and came back refused. Writing about a rename under the old
name is the natural thing to do, and the refusal said only that the file had no
changes. dbn now resolves a source path to its destination in the same post-time
pass as #85's widening.

This goes further than that widening, which only ever adds lines: aliasing replaces
a value the agent authored, so the limits matter more. Only the *old* side of an
Excerpt is aliased, because a source path names the merge-base and a new-side range
names the working tree, where the name either does not exist or belongs to
something else. And the alias lapses entirely when the branch reused the freed-up
name for a new file: that path then carries atoms of its own, and dbn takes the
agent at its word rather than guessing which file was meant. Both ends of a rename
listed in one Acknowledgement fold into one entry, because they are one file.

Nothing is hidden by this. The Reviewer sees the destination path everywhere, as
before, carrying git's own "renamed from …" detail.

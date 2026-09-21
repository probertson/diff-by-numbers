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

## Amendment (#84): the budget counts new reading, not everything shown

A Revision Round whose Step was made entirely of unchanged, already-reviewed
lines was refused as oversized, with no justification its author could honestly
give: the reading had already been done. The budget now counts only the Changed
Lines a Step asks the Reviewer to read for the first time — so a line pre-marked
as shown by a Revision Round (ADR-0007, and ADR-0014 once round snapshots decide
it) is free, on both sides, including a before-side line that rides along.

That makes two exemptions, with the whitespace one above, and they have the same
shape: what a Step *shows* and what it *costs* are no longer the same number. The
Step still shows every line — nothing is hidden, and coverage still accounts for
all of it. Neither exemption forces a boundary, which is what ADR-0003 rules out;
both only relax a pressure that was being applied for reading nobody has to do.

The live coverage the Reviewer sees is deliberately not filtered this way. It
counts every atom in the Change Set, because it answers "how much of this review
is behind me", which is a different question from "how much does this Step ask
of me now".

## Amendment (#68): an old-side Excerpt may also assign a rewrite's before-side

A Walkthrough that split the after-side of one rewrite across Steps by idea —
exactly the authoring the Reviewer asked for, including splitting inside a
function — put the whole before-side in whichever Step happened to show the
rewrite's first new line. That Step was enormous and mostly showed removals
belonging to code it did not display, and the others lost the before → after
framing that is the point of pointing once at a change.

git gives no pairing inside a rewrite, so dbn cannot derive the split and must not
invent one: a guessed pairing would put removed code under an explanation that
does not describe it, which is the failure this tool exists to prevent. The
Authoring Agent says so instead, with old-side Excerpts. That gives an old-side
range a second job beyond ADR-0002's "a standalone deletion": it also assigns
part of a rewrite's before-side to the Step that replaced it. Whatever no Step
claims still goes to the Step showing the replacement's first line, so a
Walkthrough that says nothing behaves exactly as before.

Two consequences run against the amendments above, and are narrower than they
look:

- Amendment (#85) says absorption "only ever widens: nothing the agent selected is
  dropped". A claimed line no longer renders as a standalone block, because it has
  already been drawn interleaved above the after-side it replaced. Nothing is
  dropped — the line is shown exactly once, in the place that makes it legible —
  and what falls outside any modification the Step shows still renders standalone.
- Amendment (#23) limits the cross-side correspondence to "two mechanical purposes
  only". This is a third: deciding which Step draws which removed lines. It stays
  mechanical and stays invisible to the agent, which still authors Side-qualified
  ranges and never sees a hunk.

The signpost row is the one thing dbn puts on screen that it did not read from a
file. It is not content and cannot be quoted: it carries no code, the cursor
passes over it, and an Anchor drops it rather than quoting it as source. It exists
because the alternative — an after-side with nothing to read it against and no
explanation of why — is worse than a row saying where to look.

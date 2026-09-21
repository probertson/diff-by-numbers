# diff-by-numbers

A channel for structured agent-to-human communication during code review. An
agent that has just written code drives a numbered, semantically ordered
walkthrough of its own changes; the human reviews and replies in place. The
name plays on "paint by numbers" — a simple, ordered path through work that is
otherwise overwhelming to approach all at once.

## Guiding principle

dbn optimises for one thing: **comprehension at a glance**. A change should reach
the Reviewer as a sequence of small, self-contained ideas, each understandable
without holding the rest in your head.

The obstacle it exists to beat is **git's granularity floor**. git can only cut a
diff where lines happen to be untouched, and those boundaries are routinely too
coarse for understanding — a new file is one giant hunk, two unrelated edits land
in one hunk because no blank line separates them. The Authoring Agent's Steps get
*below* that floor: it groups and orders changes semantically, for a reader, rather
than however the text diff fell out. This is the same instinct as hand-crafting a
branch's commits with interactive rebase so they read well — dbn automates it,
differing only in that it curates the *narrative over a single net diff*, not the
intermediate states, so a line's evolution across commits is neither shown nor
needed.

Every mechanism here serves that end, and none may work against it: no mechanism
may force an arbitrary boundary (ADR-0003), which is why Step size is a soft budget
requiring justification, not a hard cap.

## Language

**diff-by-numbers** (`dbn`):
The tool. A presentation and interaction surface that an agent drives. It holds
no opinion about the code and never decides what to show.
_Avoid_: reviewer, review bot, diff viewer

**Authoring Agent**:
The agent that made the changes and drives the walkthrough. Named for the fact
that its authority here comes from having written the code and known why.
_Avoid_: the AI, the assistant, the model

**Reviewer**:
The human being walked through the changes.
_Avoid_: the user, the developer

**Walkthrough**:
One complete review session over one set of changes: a Brief followed by an
ordered sequence of Steps.
_Avoid_: review, session, run

**Brief**:
The opening screen of a Walkthrough, shown before any code. States what the
Reviewer originally asked for, the approach the Authoring Agent took, its
Provenance, and the list of Steps to come. Always present, regardless of how
small the change is.
_Avoid_: summary, overview, table of contents

**Provenance**:
A Brief's declaration of where its account of intent came from: *stated* (the
Authoring Agent was there, or read the session transcript, and can cite it) or
*inferred* (reverse-engineered from the changes themselves). Required, and shown
to the Reviewer, because a guessed intent reads exactly as confidently as a known
one and deserves far less trust.
_Avoid_: source, confidence, basis

**Step**:
One numbered stop in a Walkthrough: a single self-contained idea the Authoring
Agent names, carrying whatever Excerpts across whatever files that idea touches.
Steps are ordered so each is comprehensible given only the Steps before it.
_Avoid_: chunk, chunk set, section, slice

**Change Set**:
The complete set of changes under review. May span several repositories, each named by its
own **base** ref — everything from the merge-base of that ref and HEAD to the working tree,
so by default everything that would ship. The base is a single ref, never a range: dbn
works out the merge-base itself. Always named by the Authoring Agent, never discovered by
dbn, which assumes nothing about the session's working directory.
_Avoid_: the diff, the changes, the branch

**Excerpt**:
A contiguous range of lines in one file of one repository that the Authoring Agent chose
to show,
sized and bounded for comprehension rather than by any tool's output format. May
include unchanged lines for reference. Named as an editorial selection, because
that is what it is. A Step is made of Excerpts. The repository is named
explicitly, except where only one is under review — then it may be left out and
dbn fills it in, since there is nothing else it could mean.
_Avoid_: hunk, chunk, block, fragment, snippet

**Before-Side Claim**:
An old-side Excerpt used to say which removed lines a Step's new lines replaced.
git reports a rewrite as a single edit with no pairing inside it, so when the
Authoring Agent splits its after-side across Steps by idea, dbn cannot tell which
old lines belong with which new ones — and does not guess. Whatever a Step claims,
it draws; whatever no Step claims goes to the Step showing the replacement's first
line, which is where the whole before-side goes when nothing is claimed. A line
claimed by two Steps is drawn in both and costs both their budget.
_Avoid_: pairing, mapping, allocation, ownership

**Signpost**:
A row standing where a before-side would go, naming the Steps that draw it
instead. It appears when a Step shows part of a rewrite but none of the lines that
rewrite replaced, so the Reviewer is never left reading an addition out of nowhere.
Drawn but not code: the cursor passes over it, and nothing can be selected, quoted
or anchored from it.
_Avoid_: pointer, placeholder, stub, marker

**Changed Line**:
A single line that differs between the two sides of the changes under review.
The atom of coverage, and the only unit dbn derives for itself. Git hunks are
parsed to find Changed Lines and then discarded — a hunk is an artifact of a
text format, not a unit of meaning, and is never shown to the Reviewer as one.

**Absorption**:
dbn widening an Excerpt over the run of whitespace-only Changed Lines beside it,
so an Authoring Agent listing a new file's sections need not name the blank
separators between them. The widened range is the stored one: absorbed lines are
shown like any other, and cost nothing against a Step's budget. Only lines
nothing else accounts for are absorbed, and only where they touch an Excerpt — a
whitespace-only line standing on its own is covered like any other change, because
dbn never counts a line as accounted for without showing it.
_Avoid_: exemption, skip, ignore, whitespace-insensitive, trim

**Opaque Change**:
A change with no line-level representation — a modified binary file, a mode change, a
pure rename carrying no content delta. Accounted for by the Coverage Ledger alongside
Changed Lines, since it would otherwise be invisible to the completeness guarantee.
_Avoid_: binary change, non-text change

**Rename Alias**:
A renamed file's *source* path — the name it had at the merge-base — standing for
its *destination*, the name it has now. Every atom of a renamed file is derived
under the destination, so an Acknowledgement or an old-side Excerpt written about
the name the file came from would otherwise account for nothing. dbn resolves the
source to the destination when a Walkthrough is posted, and the Reviewer sees the
destination throughout. The alias lapses when the branch reused the freed-up name
for a new file: that name carries atoms of its own, so it means that file.
_Avoid_: move, moved, old name, path mapping

**Acknowledgement**:
The Authoring Agent's declaration that a set of changes is mechanical and need not be
read line by line, carrying a one-line reason. Satisfies the Coverage Ledger in place of
an Excerpt and renders as a manifest of files and counts. A claim, not a dismissal: the
Reviewer may expand it into real Excerpts at any time — inline in the Step, as a unified diff
they can navigate, anchor and raise Comments against like any other code. Its repository may
be left out on the same terms as an Excerpt's.
_Avoid_: skip, noise, ignore, suppress

**Anchor**:
dbn's composed reference to a contiguous run of lines the Reviewer selected out of a Step's
rendering — repository, file, line numbers, Step name, and the code itself. Self-contained by
design, so it survives being pasted into a chat whose context has since been compacted. It
exists because the expensive part of raising a point during review is the pointing, not the
saying. Its extent may cross from the before-side to the after-side, because a new-side
Excerpt renders as a unified diff and a point is often about the removal and its replacement
together; it may not cross an Excerpt, which is one file's range. An Anchor into an expanded
Acknowledgement names that Acknowledgement and its reason, because a point raised there
disputes the claim that the change was mechanical, not just the line.
_Avoid_: reference, citation, pointer, location

**Comment**:
A Reviewer's remark raised against an Anchor, either a request for an edit or a
question, collected during the Walkthrough and responded to only once it ends. Resolves
to *addressed*, *answered* or *declined*. An answer or a decline carries the Authoring
Agent's response; an addressed Comment may carry one too. A question that needs a live
exchange still belongs in the harness chat (ADR-0011).
_Avoid_: change request, note, feedback, todo

**Revision Round**:
The Authoring Agent responding to the collected Comments, followed by a fresh
Walkthrough over just the resulting changes. Its Brief maps every Comment to its
resolution, so an answer or a decline is read before any code. Repeats until the Reviewer
hands a Walkthrough off having raised nothing. Lines it has already shown are pre-marked:
coverage does not demand them again, and they cost nothing against a Step's budget.
_Avoid_: fix pass, iteration, follow-up

**Hand Off**:
The Reviewer ending a round and passing the baton back: it locks their own edits
until they resume, and authorizes the Authoring Agent to respond to the Reviewer's
Comments in the next Revision Round. A turn boundary in a loop, not a conclusion —
which is why it is not called *finishing*, a word that reads as a synonym of
leaving the viewer.
_Avoid_: finish, finalize, submit, sign off, complete

**Round Snapshot**:
A git tree object recording each repository's working tree as it stood when a
Walkthrough was accepted. It does two jobs. It is what the round's code is read
from: the Reviewer sees, expands and anchors exactly what was posted, and a file
edited since is *changed on disk*, flagged with a warning rather than hidden
(ADR-0004). And a Revision Round is scoped by diffing the previous round's
snapshot against the current one, so "already read" is decided by position
rather than by line text (ADR-0014). Nothing is derived from a snapshot: the
Change Set still comes from the working tree.
_Avoid_: stash, checkpoint, baseline

**Position**:
Where a line stood in a given Round Snapshot: a file path and a line number.
What the round-over-round mapping answers with, and the unit that decides
whether a Changed Line was already shown.
_Avoid_: location, coordinate, address

**Problem**:
One fault in a refused post: what kind it is, and what specifically to fix. A
refusal carries every problem dbn could find rather than the first, so an agent
fixes them all before re-sending the Walkthrough. At most one per kind, in the
order dbn checks them.
_Avoid_: error, failure, violation

**Coverage Ledger**:
dbn's own record, derived from git rather than from the Authoring Agent, of every
Changed Line and Opaque Change under review, and whether each has been shown by an
Excerpt or covered by an Acknowledgement. A Walkthrough cannot be completed while any
remains unaccounted for. It constrains
completeness only — never ordering, grouping or size.
_Avoid_: checklist, manifest, progress

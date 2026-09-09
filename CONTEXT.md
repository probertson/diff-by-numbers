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
The complete set of changes under review. May span several repositories, each contributing
its own range — by default everything that would ship, being the merge-base with that
repository's default branch plus its working tree. Always named by the Authoring Agent,
never discovered by dbn, which assumes nothing about the session's working directory.
_Avoid_: the diff, the changes, the branch

**Excerpt**:
A contiguous range of lines in one file of one repository that the Authoring Agent chose
to show,
sized and bounded for comprehension rather than by any tool's output format. May
include unchanged lines for reference. Named as an editorial selection, because
that is what it is. A Step is made of Excerpts.
_Avoid_: hunk, chunk, block, fragment, snippet

**Changed Line**:
A single line that differs between the two sides of the changes under review.
The atom of coverage, and the only unit dbn derives for itself. Git hunks are
parsed to find Changed Lines and then discarded — a hunk is an artifact of a
text format, not a unit of meaning, and is never shown to the Reviewer as one.

**Opaque Change**:
A change with no line-level representation — a modified binary file, a mode change, a
pure rename carrying no content delta. Accounted for by the Coverage Ledger alongside
Changed Lines, since it would otherwise be invisible to the completeness guarantee.
_Avoid_: binary change, non-text change

**Acknowledgement**:
The Authoring Agent's declaration that a set of changes is mechanical and need not be
read line by line, carrying a one-line reason. Satisfies the Coverage Ledger in place of
an Excerpt and renders as a manifest of files and counts. A claim, not a dismissal: the
Reviewer may expand it into real Excerpts at any time.
_Avoid_: skip, noise, ignore, suppress

**Anchor**:
dbn's composed reference to a Reviewer-selected line range — repository, file, line numbers,
Step name, and the code itself. Self-contained by design, so it survives being pasted into a
chat whose context has since been compacted. It exists because the expensive part of raising
a point during review is the pointing, not the saying.
_Avoid_: reference, citation, pointer, location

**Change Request**:
A Reviewer's request for an edit, raised against an Anchor. Collected
during the Walkthrough and acted on only once the Walkthrough ends — never
mid-flight. A proposal rather than an instruction: it resolves to *addressed* or
*declined*, and a decline carries the Authoring Agent's reasoning. Distinct from a
question, which is asked in the harness chat and changes nothing.
_Avoid_: comment, note, feedback, todo

**Revision Round**:
The Authoring Agent working the collected Change Requests, followed by a fresh
Walkthrough over just the resulting changes. Its Brief maps every Change Request
to its resolution, so a decline is read before any code. Repeats until the Reviewer
finishes a Walkthrough having raised nothing.
_Avoid_: fix pass, iteration, follow-up

**Coverage Ledger**:
dbn's own record, derived from git rather than from the Authoring Agent, of every
Changed Line and Opaque Change under review, and whether each has been shown by an
Excerpt or covered by an Acknowledgement. A Walkthrough cannot be completed while any
remains unaccounted for. It constrains
completeness only — never ordering, grouping or size.
_Avoid_: checklist, manifest, progress

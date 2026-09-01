# diff-by-numbers

A channel for structured agent-to-human communication during code review. An
agent that has just written code drives a numbered, semantically ordered
walkthrough of its own changes; the human reviews and replies in place. The
name plays on "paint by numbers" — a simple, ordered path through work that is
otherwise overwhelming to approach all at once.

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

**Excerpt**:
A contiguous range of lines in one file that the Authoring Agent chose to show,
sized and bounded for comprehension rather than by any tool's output format. May
include unchanged lines for reference. Named as an editorial selection, because
that is what it is. A Step is made of Excerpts.
_Avoid_: hunk, chunk, block, fragment, snippet

**Changed Line**:
A single line that differs between the two sides of the changes under review.
The atom of coverage, and the only unit dbn derives for itself. Git hunks are
parsed to find Changed Lines and then discarded — a hunk is an artifact of a
text format, not a unit of meaning, and is never shown to the Reviewer as one.

**Change Request**:
A Reviewer's request for an edit, raised at a Step and attached to it. Collected
during the Walkthrough and acted on only once the Walkthrough ends — never
mid-flight. Distinct from a question, which is answered live and changes nothing.
_Avoid_: comment, note, feedback, todo

**Revision Round**:
The Authoring Agent working the collected Change Requests, followed by a fresh
Walkthrough over just the resulting changes. Repeats until the Reviewer approves
with nothing outstanding.
_Avoid_: fix pass, iteration, follow-up

**Coverage Ledger**:
dbn's own record, derived from git rather than from the Authoring Agent, of every
Changed Line in the changes under review and whether some Excerpt has shown it. A
Walkthrough cannot be completed while Changed Lines remain unshown. It constrains
completeness only — never ordering, grouping or size.
_Avoid_: checklist, manifest, progress

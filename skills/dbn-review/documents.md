# Document guidance: planning a Round around Documents

Read this when `describe_changes` marks any file in the Change Set as a
**Document**: a file of prose for a person to read, such as a spec, a story, an
ADR or a skill's instructions. It adds to *Planning a Round* in SKILL.md and
replaces none of it. It applies to the Documents alone: in a Change Set that
mixes Documents and code, plan the code as you always would. One Step may still
show a Document beside the code it describes.

## The reviewer's priorities come first

**Which parts of a Document matter is the reviewer's to say.** They may have
said so already, in their own instructions: a personal skill listing the order
a story's sections should come in, say, or which parts of an ADR they read
closely. Follow any such instructions over everything below. dbn holds no
opinion of its own on which sections matter, and the order below is a default,
not a judgment.

**When they have said nothing, ask before you leave anything out or reorder.**
If you plan to show only part of a Document, or to show its parts in an order
other than its own, ask the reviewer in chat which parts matter to them, and
wait for their reply before you build the Round. Propose the default order
below; if they leave it to you, follow it. A guess about what they care about
can build the whole Round around the wrong parts.

## A new Document: outline, grounding, then the rest

**Without stated priorities, plan a new Document, or a large new section of
one, in this order:**
1. **An outline Step:** the headings alone, one Excerpt per heading line. It
   shows the Document's shape before its content, in the order its readers will
   meet it. The headings show again, with their text, in the Steps after it.
2. **The grounding:** the summary, the purpose, the acceptance criteria —
   whatever the rest is judged against.
3. **Where you applied or interpreted a decision:** the content that turns a
   decision into requirements, rules or wording, and above all the places where
   you read a decision one way when it could have been read another.
4. **Supporting detail.**
5. **Boilerplate last.**

**"Decisions" never means the record of decisions the reviewer made
themselves**: their answers while the work was planned, or their Answers to
your Agent Questions. A Decisions section or a log of what was settled with
them is text they have already reviewed. What needs their attention is how
those decisions were turned into content, so build the Round around that, not
around the record.

**Say in each Step where it sits in its Document** — "§4, after the data
model". A Step shown out of the Document's own order has nothing around it to
place it by.

## Splitting and grouping

**Split by the Document's own sections, not by page.** A Step follows the
Document's section boundaries: it does not stop partway through a section
because it has shown enough lines, or run on into the next section for the same
reason. A section that holds more than one idea is split by idea, as code is.

**One idea per Step, exactly as in code.** That holds for changes to an
existing Document as much as for a new one. When two changes run across the
same files — a renamed term and a new rule, say, both touching the same three
Documents — show them change by change, each Step carrying one change across
every file it touches, rather than file by file.

**Group related Documents together, and say why when the order jumps.** Show
closely related Documents together, even out of their numeric order, so the
reviewer reads them while their shared context is fresh. When the order jumps —
story 29 before story 21, because 21 raises an issue that 29 fixes — say why in
the Step where it jumps. A jump that is right but unexplained still leaves the
reviewer working out why.

## Context in a Document is its enclosing paragraph

**An Excerpt into an existing Document includes the whole paragraph the change
sits in**, not a line or two either side. A sentence changed in the middle of a
paragraph means little without the rest of it. Unchanged lines cost nothing
against a Step's budget, so the paragraph comes free. This is still *Keep
Excerpts tight*: the paragraph gives the change its context; it does not bridge
to the next change.

## Open questions in a Document

**An open question that blocks the work is an Agent Question.** If a Document
has a question still open that the work cannot go on without, ask it as an
Agent Question on the Step that shows it, rather than leaving it as text for
the reviewer to come across. Then update the Document from the Answer, so the
question is settled in the Document and not just in the Review.

**Questions left open on purpose stay as text.** A section such as "Settled at
verification" holds questions the Document deliberately leaves for later.
Asking them now asks the reviewer to decide what was meant to wait.

## Worked examples

Both are Rounds from dbn's own history, planned again as this guidance would
plan them. Each lists the Steps in order, with a line on why each sits where it
does. In both, the reviewer had stated no priorities and, when asked, left the
order to the agent.

### A new ADR beside edits to the glossary

The Change Set: `docs/adr/0017-agent-questions.md`, a new ADR introducing
Agent Questions (as first written, before ADR-0018 amended it), and the
glossary edits that go with it in `CONTEXT.md`: two new entries, Agent Question
and Answer, and edits to four others, Overview, Hand Off, Revision Round and
Replacement. Each glossary entry is one paragraph, so each glossary Excerpt is
the whole entry.

1. **ADR-0017's outline** — its title and section headings alone, so the
   reviewer sees the argument's shape before any of it.
2. **The ADR's opening: what an Agent Question is** — the decision in one
   paragraph, the grounding the rest is judged against.
3. **§Why: a question lost in the narration** — the purpose, with the writing
   convention it rejects, before any of the design.
4. **The glossary gains Agent Question and Answer** — the order jumps to
   `CONTEXT.md` here because every remaining section of the ADR uses these two
   terms.
5. **§Where a question sits, with the Overview entry's edit** — the decision
   and the glossary wording that applies it, together.
6. **§Hand Off, with the Hand Off entry's sentence on unanswered questions** —
   Hand Off is not blocked; the same pairing.
7. **§What a Hand Off with questions means, with the Revision Round entry's
   "in a Round that asked nothing either"** — the first of two separate edits
   to that entry, each shown with the section it applies.
8. **§Accounting in the Revision Round, with the Revision Round and Hand Off
   entries' "Comments and Answers"** — the second change, shown change by
   change rather than entry by entry.
9. **§Replacement, with the Replacement entry's edit** — what carries over and
   what is dropped.
10. **§Rejected** — supporting detail: the alternatives weighed.
11. **§Consequences** — last: what the decision changes elsewhere.

### One long new requirements document

The Change Set: one new 136-line Document, the requirements at
`docs/epics/conclusion-screen-change-request-list/requirements.md`. Its
Decisions section records the answers the reviewer gave while the requirements
were planned.

1. **Outline** — the title and the six section headings, so the reviewer sees
   the shape first.
2. **§Background, opening: the gap** — what the conclusion screen cannot do
   today. Grounding.
3. **§Background: the original request** — the issue, quoted in full, that
   every requirement is judged against. Grounding.
4. **§Background: constraints from the system as it stands** — how the screen,
   the list and the edit screen behave today, which the requirements change.
   Grounding, without the code references beneath it.
5. **§Requirements 1–3: the conclusion screen** — the new line, the `l` key and
   the footer. The explanation names where they interpret the request: the
   footer gains Hand Off too, and the line says "Comments", not the request's
   "Change Requests".
6. **§Requirements 4–6: back to where the list was opened** — the return made
   uniform everywhere the list opens, not only from the conclusion screen: the
   widest reading of the request.
7. **§Requirement 7: the success measure** — short, but its own idea.
8. **§Out of scope** — supporting detail: what was deliberately not done.
9. **§Background: references** — the order jumps back to the end of
   Background because these code references only support Step 4's
   constraints, for the reviewer who wants to check one.
10. **§Decisions** — the record of the reviewer's own answers, already
    reviewed. Late, because what needed their attention was Steps 5 and 6,
    where those answers became requirements.
11. **§Open questions** — boilerplate: "None", so there is nothing to ask as an
    Agent Question.
12. **§Designs** — boilerplate, last.

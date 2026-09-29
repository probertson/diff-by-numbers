# The Brief's Background: what the Reviewer needs before any code

The Brief's `approach` field is renamed `background`, and its meaning changes with the name.
It is no longer "the approach you took, so the Reviewer can judge it apart from the code". It
is what the Reviewer needs to know before seeing any code: the stage-setting that makes the
whole Review's argument make sense, starting from what the Reviewer already knows (#128).

Round 1 must give it, and so must a Replacement of round 1. A Revision Round, or a Replacement
of one, gives it only when there is new stage-setting worth reading. Unlike the Goal, nothing
carries it forward: a round that gives none shows none on its Overview.

`approach` is removed, not kept as an alias. A post that still sends it is refused by the MCP
library's own schema check, which names the field.

## Why

A dogfooding review got an `approach` that read as a hand-off report: every change in turn,
the test-suite count and lint, and two things "deliberately NOT changed, both waiting for your
decision" that were never asked as questions. The field's name, and the schema description
every agent reads on every post, invited exactly that: a record of what the agent did.

- **The name.** "Approach" says "what I did". "Background" says "what you need first",
  without inviting a report.
- **Design belongs with its code.** An architecture, a pattern, a data flow or a data model
  is shown in a Step with the least code that demonstrates it, such as signatures or the
  calls between them. It goes in the Background only when no code shows it.
- **No meta-commentary.** The Background says nothing about the Steps or their order, which
  the Overview already lists. It doesn't name the spec either, since the Goal says what the
  Reviewer asked for.
- **Undone work is a question or nothing.** Work left for the Reviewer to decide is a
  Round-level Agent Question. Work that needs no decision stays out of the Review, because
  mentioning it only tells the Reviewer what not to look for.
- **Not carried forward.** Round 1's stage-setting shown again in a later round reads like
  current information. It is better to show nothing than something stale.
- **No alias.** An alias would let exactly the stale sessions that wrote the hand-off report
  keep filling the renamed field the old way. The library's refusal names the field, and the
  schema shows `background` with a description that stands on its own. The general problem of
  a session posting from an old copy of the skill is #130.

## Consequences

ADR-0017 said a Round-level Agent Question is "about the approach", shown with the Brief on
the Overview, "which exists so the Reviewer can judge the approach apart from the code". That
purpose is gone. A Round-level question is now about the work as a whole, including work left
undone for the Reviewer to decide, and it is still shown with the Brief.

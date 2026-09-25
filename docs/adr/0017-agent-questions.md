# Agent Questions: the agent asks, the Reviewer answers

The Authoring Agent can put an **Agent Question** to the Reviewer: a structured question that
dbn knows about, shows apart from the narration, and makes the Reviewer account for at Hand
Off. The Reviewer's reply is its **Answer**, in free text. This is the reverse of a Comment
(ADR-0013): the loop already carried the Reviewer's questions to the agent, and now carries
the agent's to the Reviewer.

## Why

An agent wrote "PLEASE DECIDE: …" in a Step's Explanation, and the Reviewer, reading a large
Round for its design, missed it (#114). An agent asking for a decision is a legitimate part of
a review. But a Step then had only a name, an Explanation, Excerpts and Acknowledgements, so the
question was prose among prose. dbn could not surface it, count it or notice it going
unanswered, and the only way back was a Comment, which needs an Anchor on code even when
the question is not about any line.

A writing convention was weighed and rejected: telling the agent to put decisions in the Brief
under a fixed heading. It is free, but it leaves the question as prose, which is what failed.

## Where a question sits

An Agent Question attaches either to the Round or to a Step, as the agent chooses.

- **To the Round**, when it is about the approach. It is shown with the Brief on the
  Overview, which exists so the Reviewer can judge the approach apart from the code.
- **To a Step**, when it is about that Step's code. Its text is shown only on that Step, at the
  top, before the Excerpts, in its own labelled block distinct from the Explanation. The
  Step's status line counts it while it is unanswered.

A Step-level question's text never appears on the Overview. It depends on the Steps before it
and on its own Step for its context, and read cold on the Overview it only distracts. The
Overview's list of Steps marks each Step that carries one, without the text, so the Reviewer
knows a decision is waiting there and slows down when they reach it.

## Hand Off

Hand Off is not blocked by an unanswered question. It lists the unanswered ones and asks the
Reviewer to confirm, and each links to its Step. The failure was not noticing, not refusing to
answer. A hard gate would push the Reviewer to invent an answer to get past it, when the
honest answer is "not yet" or "your call". A question handed off unanswered reaches the agent
marked *unanswered*, so the agent never reads silence as agreement.

Answers behave like Comments: editable and clearable until Hand Off, locked by it, unlocked by
resuming.

## What a Hand Off with questions means

A Round that carried Agent Questions is never inferred to be concluded at Hand Off, even when
no Comment was raised and no question was answered. An Answer may call for a change, which
must be reviewed like any other. So in `dbn wait` and the Round's outcome, *nothing raised*
now means no Comments and no Agent Questions. The agent fetches the Answers and decides:

- It may `conclude` only when there are no Comments and every question was answered with
  nothing to change.
- Otherwise it posts a Revision Round, even one in which no code changed. Such a Round is
  already accepted, with a Step re-showing code the Reviewer has read.

The rule is in the `dbn-review` skill, not enforced. `conclude` cannot know what the agent
would have said.

## Accounting in the Revision Round

As with Comments, the Revision Round accounts for every Agent Question of the round before, and
dbn refuses a Round that leaves one out. dbn shows each question with its Answer on the
Overview, since it holds both, and the agent gives each one a status:

- **addressed**: the Answer led to a change. A response is optional.
- **no change needed**: the Answer agreed with the code as it stood. A response is optional.
- **asked again**: the question was unanswered, or its Answer cannot be acted on as written.
  The response is required. The status links to a new Agent Question in this Round, and dbn
  refuses a link to nothing. The new question shows the earlier wording and Answer, so the
  Reviewer does not start over.
- **agent's call**: the question was unanswered, and the agent went ahead on its own judgment.
  The response is required: what it chose.

There is no *declined*. The Reviewer was asked to decide, so the agent cannot overrule the
Answer. If it cannot follow the Answer, it asks again and says why.

*asked again* links to a new question rather than being answered on the Overview. That way a
question asked again can still sit on the Step whose code it concerns.

## Replacement

When a Round is replaced (ADR-0004), answered questions carry over as Comments do. They move
off their Step, keep their wording and Answer, are marked *carried over*, and the Reviewer may
withdraw them. Unanswered questions are dropped. The agent wrote them and the replacement is
its fresh post, so it asks again what still matters, placed on the new Steps. Carrying an
unanswered question over would detach it from the context it needs. Unlike a Comment, it has
no Anchor quoting its code.

The agent cannot see mid-round which questions were answered, so it may ask one again that
the Reviewer already answered. The Reviewer then sees the carried Answer and the new question
together, and resolves the pair either way.

## Rejected

- **Answer choices defined by the agent.** A picker saves little over typing "Y" in a TUI.
  The questions that matter draw "Y, but …" answers, and a structured choice brings rules for a
  choice plus text. It is additive, so it can come later.
- **A cap on questions per Round.** It would be an arbitrary boundary of the kind ADR-0003
  rules out. Overuse limits itself, since the Reviewer notices and says so. The `dbn-review`
  skill carries the guidance instead: ask only when a decision or judgment is needed before
  going on, never "does this look OK?", never for what the Brief should simply state, and
  write each question so it can be answered from its Step and the Steps before it. A soft
  budget like the Step size budget stays available if guidance proves not to be enough.
- **Anchoring a question to lines in an Excerpt.** A Step is already one idea. That is fine
  enough, and it avoids new rendering inside the code view.

## Consequences

ADR-0011 said conversation belongs in the harness chat. The ADR-0013 amendment already sent
questions that can wait for the round to end through dbn, and this ADR applies that in the
other direction. A question that needs back-and-forth still belongs in chat.

The `dbn-review` skill must now say what to post when nothing changed since the last Round,
which it never covered. This design makes that case routine rather than rare.

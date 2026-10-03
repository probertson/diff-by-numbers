
**Authoring Agent**:
The agent that made the changes and drives the Review. Named for the fact
that its authority here comes from having written the code and known why.
_Avoid_: the AI, the assistant, the model

**Reviewer**:
The human being walked through the changes.
_Avoid_: the user, the developer

**Review**:
The whole exchange over one piece of work: from the Authoring Agent's first post,
through every Revision Round, until it is concluded or dismissed. It has an id,
minted by dbn, that every call about it names.
_Avoid_: session, walkthrough

**Round**:
One pass of a Review: the Brief and ordered Steps the Authoring Agent posts, and
the Reviewer working through them until they Hand Off. Round 1 is the first; every
round after it is a Revision Round. A Replacement puts new Steps into the same
round rather than starting another.
_Avoid_: walkthrough, pass, iteration

**Brief**:
What the Authoring Agent writes to open a Round: the approach it took, so the
Reviewer can judge the approach apart from the code implementing it, and in round 1
the Goal. Required on every Round, however small the change. It is authored, not composed: dbn shows it on the
Overview and hands it back to the agent to re-ground it, but adds nothing to it.
_Avoid_: summary, table of contents

**Goal**:
What the work set out to achieve, as the Reviewer asked for it, and what every
Round's approach and code are judged against. It belongs to the Review, not a
Round: given in round 1's Brief and carried forward to every Overview after it. A
Revision Round restates it only when what the Reviewer wants has changed.
_Avoid_: ask, request, task, objective

**Overview**:
The Reviewer's opening screen of a Round, shown before any code. dbn composes it
from the Brief and what it knows itself: in a Revision Round, what became of each
of the last round's Comments and what was withdrawn since; and the Steps to come.
_Avoid_: brief, summary, table of contents


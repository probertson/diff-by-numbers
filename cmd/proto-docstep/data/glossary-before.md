
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


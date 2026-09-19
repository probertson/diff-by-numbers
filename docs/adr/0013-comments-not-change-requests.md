# Comments, not Change Requests

What the Reviewer raises against an Anchor is a **Comment**. It may ask for a change or ask a
question, and the Authoring Agent responds to each one in the Revision Round. The term replaces
**Change Request** everywhere: glossary, code, wire, MCP tools and UI. Earlier ADRs keep their
wording as historical records; read "Change Request" there as "Comment".

## Why

The old name said every remark asks for a change, and ADR-0011 sent questions to the harness
chat. In practice Reviewers already raised questions through dbn, because the batched,
anchored channel is the cheapest place to ask one: the Anchor does the pointing, and the
question waits with the rest of the round instead of interrupting the agent. A question
disposed of as *declined* read as a refusal, and one disposed of as *addressed* claimed a
change that never happened. The UI had also split along the same seam, saying "comment" in
the raise flow and "Change Request" everywhere else.

## Dispositions

A Revision Round resolves every Comment from the previous round to one of three statuses. The
agent chooses; the Reviewer labels nothing when raising.

- **addressed**: the agent made a change. A response is optional.
- **answered**: the agent responded without changing anything, as to a question. The response
  is required, since it is the answer.
- **declined**: the agent will not make the change asked for. The response is required, as a
  reason the Reviewer can weigh (and may re-raise against).

The field that carries this was `reasoning`, required only for a decline; it is now
`response`.

## Consequences

The rename has no aliases for the old wire names (`change_requests`, `change_request_id`,
`reasoning`, the `/changerequest` endpoints). Agents read tool schemas from the running daemon,
and the plugin's skill ships in step with releases, so nothing outside the repository holds
the old names for long.

Live discussion still belongs in the harness chat (ADR-0011): a Comment is for a question that
can wait for the round to end.

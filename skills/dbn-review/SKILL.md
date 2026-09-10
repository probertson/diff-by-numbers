---
name: dbn-review
description: Post a walkthrough of your own code changes to diff-by-numbers (dbn) for a human to review. Use this when you have finished a body of work and want it reviewed — instead of handing over a raw diff, plan a narrated, semantically-ordered walkthrough and post it over the dbn MCP server. Also use it to collect the reviewer's Change Requests and post a Revision Round.
---

# Reviewing your changes with dbn

dbn is a channel for you to walk a human through the code you just wrote, in an
order that makes sense for review rather than the order git happens to print.
You post one complete **Walkthrough**; the reviewer navigates it themselves in a
side terminal and raises **Change Requests**; you collect those and post a
**Revision Round**. You never block waiting — you post, end your turn, and pick
the results up later.

The dbn daemon exposes two MCP tools: `post_walkthrough` and `fetch_results`. If
they are not available, dbn's MCP server is not registered or not running — see
the project README for the one-time setup.

## Planning a Walkthrough

A Walkthrough is a **Brief** followed by ordered **Steps**.

**The Brief** sets context before any code:
- `ask` — what the reviewer originally asked you for, in their words.
- `approach` — the approach you took, so they can judge it apart from the code.
- `provenance` — `stated` if you were actually asked this (cite the session or
  prompt in `citation`), or `inferred` if you are reverse-engineering the intent
  from the changes. Be honest: a guessed intent reads as confidently as a known
  one and deserves less trust.

**Steps** each carry one self-contained idea — "add retry with backoff to the
fetch layer", "thread the tenant id through the callers" — not one file, and not
one git hunk. Plan them so:

- **Ordering is narrative.** Each Step must be comprehensible given only the
  Steps before it. Show a function before its callers; a type before its uses.
- **Sizing is for comprehension.** Keep a Step to roughly 30 changed lines. A
  large new file becomes several Steps by meaning, not one wall of code. If a
  Step genuinely must be bigger, say why in `oversize_justification` — dbn never
  forbids a large Step, it only asks for a reason.
- **Excerpts are ranges you choose**, `{repository, file, side, first_line,
  last_line}`. Send ranges, never code — dbn reads the bytes itself. An Excerpt
  may include unchanged lines for context.
- **Point once at a change.** For an edit, give a `new`-side Excerpt over the
  after-side; dbn reads the before-side from git and shows a real before → after
  diff, so you never name the old side for an edit. Use an `old`-side Excerpt only
  to show a **standalone deletion** — removed code that nothing replaced — with
  your explanation of why it went. (A bulk, mechanical removal can stay an
  Acknowledgement instead; your call per case, exactly as for additions.)
- **One Step may span several files** if one idea touches several.
- **Prefer more, smaller Steps.** A Step should be comprehensible at a glance —
  don't make the reviewer hold two functions in their head at once. Because an
  edit shows both its before and after, the ~30-line budget counts both sides, so
  a rewrite fills it faster than an addition. That pressure is intentional: split
  it into more Steps rather than justify a wall of diff.

**Acknowledgements** cover mechanical changes you should not make the reviewer
read line by line: a regenerated lockfile, a deleted dead module, a re-exported
binary asset. An Acknowledgement is `{repository, files, reason}` and stands in
for Excerpts on those files. It is the **only** way to cover an Opaque Change — a
binary file, a mode change, a pure rename — which has no lines to excerpt. Use
one when the change is truly mechanical; do not use it to hide real code, because
the reviewer sees the manifest and can expand it into the actual diff.

**Coverage is enforced.** dbn derives the changed lines from git and refuses a
Walkthrough that leaves any of them shown by neither an Excerpt nor an
Acknowledgement. If your post is rejected as `uncovered_changes`, it names what
you missed — add it and re-post.

## Posting

Call `post_walkthrough` once, complete. Name every repository under review in
`repositories` (each with its own `range`, e.g. the default branch), then the
`brief` and the ordered `steps`. Then **end your turn** — tell the human their
review is ready and that you will pick up their feedback when they are done. Do
not poll.

## Collecting feedback and revising

When the human says they have finished, call `fetch_results`. It returns
immediately (it never waits) with the reviewer's Change Requests, each carrying
an anchored reference to the exact code it concerns. Work them.

Then post a **Revision Round**: another `post_walkthrough`, over the full change
set again, but this time include `dispositions` — one entry per Change Request
you were handed, each `addressed` or `declined` (a decline needs a one-line
`reasoning` the reviewer will see before any code, and may re-raise). dbn
re-derives everything and pre-marks as already-seen every line whose content is
unchanged, so the new Walkthrough is scoped to exactly what you moved. You still
plan Steps and coverage for the moved lines the same way.

Repeat until the reviewer finishes having raised nothing — `fetch_results` will
say the review is complete.

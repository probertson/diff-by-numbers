---
name: dbn-review
description: Post a walkthrough of your own code changes to diff-by-numbers (dbn) for a human to review. Use this when you have finished a body of work and want it reviewed — instead of handing over a raw diff, plan a narrated, semantically-ordered walkthrough and post it over the dbn MCP server. Also use it to collect the reviewer's Comments and post a Revision Round.
---

# Reviewing your changes with dbn

dbn is a channel for you to walk a human through the code you just wrote, in an
order that makes sense for review rather than the order git happens to print.
You post one complete **Walkthrough**; the reviewer navigates it themselves in a
side terminal and raises **Comments** (requests for a change, or questions); you
collect those and post a **Revision Round**. You never block waiting — you post,
end your turn, and pick the results up later.

The dbn daemon exposes three MCP tools: `post_walkthrough`, `fetch_results`, and
`conclude`. If they are not available, dbn's MCP server is not registered — see
the project README for the one-time setup. (You do not need to start anything
first: registering the shim is enough. It starts the daemon on demand the moment
your session connects.)

**Requires dbn v0.2.2 or later.** This skill ships separately from the binary,
so the two can drift apart. If a dbn tool call fails because the tool is unknown
or because it rejects a field this skill told you to send, do not work around it
and do not fall back to a raw diff: tell the Reviewer to run `dbn update`, which
updates the binary and says whether this skill is stale too.

The tools' exact schemas and parameters come from the running daemon, which
cannot drift from itself — read them there rather than from anything written
here.

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
  may include unchanged lines for context. Reviewing a single repository, omit
  `repository` — on Excerpts and Acknowledgements both — and dbn uses the only
  one there is. With several, it is required and never guessed from the path.
- **You need not list blank separator lines.** A blank line between two ranges
  you do list is absorbed into the range before it, so listing a new file's
  sections covers the whole file. Absorbed lines are shown, and cost nothing
  against the Step's budget. A blank line standing on its own, next to no
  Excerpt, still has to be covered like any other change.
- **Point once at a change.** For an edit, give a `new`-side Excerpt over the
  after-side; dbn reads the before-side from git and shows a real before → after
  diff, so you never name the old side for an edit. Use an `old`-side Excerpt only
  to show a **standalone deletion** — removed code that nothing replaced — with
  your explanation of why it went. (A bulk, mechanical removal can stay an
  Acknowledgement instead; your call per case, exactly as for additions.)
- **Either path of a renamed file works.** dbn derives a renamed file's changes
  under its new path, but an Acknowledgement or an `old`-side Excerpt may name
  the path it came from — listing both is fine too. The Reviewer sees the new
  path either way. The exception is a path the same branch reused for a new
  file: that name now means the new file, so cover the two separately.
- **Splitting one edit across Steps: say which old lines went where.** Changes in
  contiguous code that are about different things belong in different Steps, even
  inside one function. When that splits a single edit's after-side across Steps,
  give each Step an `old`-side Excerpt over the lines its new lines replaced, and
  dbn draws each before → after pair in its own Step. Say nothing and the whole
  before-side stays with the Step showing the edit's first new line, and the other
  Steps get a signpost saying where to find it.
- **One Step may span several files** if one idea touches several.
- **Prefer more, smaller Steps.** A Step should be comprehensible at a glance —
  don't make the reviewer hold two functions in their head at once. Because an
  edit shows both its before and after, the ~30-line budget counts both sides, so
  a rewrite fills it faster than an addition. That pressure is intentional: split
  it into more Steps rather than justify a wall of diff. In a Revision Round,
  lines the reviewer has already seen are free, so re-showing context around a fix
  costs you nothing — the budget counts new reading, not everything on screen.

**Acknowledgements** cover mechanical changes you should not make the reviewer
read line by line: a regenerated lockfile, a deleted dead module, a re-exported
binary asset. An Acknowledgement is `{repository, files, reason}` and stands in
for Excerpts on those files. It is the **only** way to cover an Opaque Change — a
binary file, a mode change, a pure rename — which has no lines to excerpt. Use
one when the change is truly mechanical; do not use it to hide real code, because
the reviewer sees the manifest and can expand it into the actual diff — and raise
Comments against it.

**Coverage is enforced.** dbn derives the changed lines from git and refuses a
Walkthrough that leaves any of them shown by neither an Excerpt nor an
Acknowledgement. If your post is rejected as `uncovered_changes`, it names
everything you missed, grouped by file and side — add it all and re-post.

**A rejection lists every problem it found.** Once the Walkthrough is
structurally sound, dbn runs all its checks and reports all of them in
`problems`, rather than stopping at the first. Fix every entry before posting
again: posting to discover the next one costs you the whole Walkthrough each
time. A structural fault — a malformed Brief or Step, a bad `base`, a Change
Set that will not derive — comes back on its own, because the later checks
cannot say anything useful until it is fixed.

**Untracked files count.** A file you created but never `git add`ed is part of
the Change Set, every line of it, and needs covering like any other. There is no
opt-out. Scratch files you do not want reviewed should be deleted or added to
`.gitignore` — or, if they belong to the work but do not repay reading, covered
by an Acknowledgement, which the reviewer can see and expand.

## Posting

`base` is a single ref — a branch, tag or commit — not a range. dbn reviews
everything from the merge-base of that ref and HEAD to the working tree,
including uncommitted changes, so `HEAD~1..HEAD` is refused. For "the last
commit", the base is `HEAD~1`.

Call `post_walkthrough` once, complete. Name every repository under review in
`repositories` (each with its own `base` ref, e.g. the default branch), then the
`brief` and the ordered `steps`. You may add an optional `label` — a short name
like "auth refactor" — to help a reviewer tell several reviews apart.

`post_walkthrough` returns a `review_id`. **Record it**: you pass it back to
`conclude` when the review is over. It also returns a `message`: **relay it** to
the human, since it says how they open the review, and say you will pick up
their feedback when they are done.
Then **end your turn**. Do not poll.

## Collecting feedback and revising

When the human says they have handed the review off, call `fetch_results`. It returns
immediately (it never waits) with the reviewer's Comments, each carrying an
anchored reference to the exact code it concerns. Respond to each: a Comment may
ask for a change or ask a question.

Then post a **Revision Round**: another `post_walkthrough`, over the full change
set again, but this time include `dispositions` — one entry per Comment you were
handed, with a `status` you choose and a `response` the reviewer sees before any
code:

- `addressed` — you made a change. A `response` is optional but welcome, e.g. to
  say how you fixed it or that you fixed a twin elsewhere too.
- `answered` — you responded without changing anything, as to a question. The
  `response` is required: it is the answer.
- `declined` — you won't make the change asked for. The `response` is required:
  a reason, in a line or two, that the reviewer can weigh (and may re-raise).

Use `answered` for a question, even one you could read as a request; use
`declined` only when you are turning down a change. If answering the question
led you to change the code, that is `addressed`, with the answer as its
`response`.

An anchor whose first line says `file changed since this round was posted` was
taken from code you edited after posting: its lines and line numbers are the ones
the reviewer saw, not what is on disk now, so find the place by its code.

A Comment carrying `re_raised_from` is one the reviewer pushed back on: they read
your decline or your answer and did not accept it. It calls for a change, or for
a stronger argument than the one they already rejected — repeating the same
reasoning is not a response.

dbn re-derives everything and pre-marks as already-seen every line the reviewer
read last round and nobody has touched since, so the new Walkthrough is scoped to
exactly what you moved. You still plan Steps and coverage for the moved lines the
same way.

Scoping is positional — dbn compares each round against a snapshot of the working
tree it took when the last round was accepted — so it is exact. Blank lines and
lone closing braces are scoped out like anything else, and a file you did not
touch at all needs nothing. That includes Opaque Changes: a binary, a mode change
or a rename you have not re-touched since the last round needs **no second
Acknowledgement**. Only what actually moved comes back.

A Comment whose anchor says the code was `acknowledged in Step "…"`
disputes that Acknowledgement as well as the line: the reviewer read code you
called mechanical and found something to say. When you resolve it, say in the
Revision Round's Brief whether the "mechanical" claim still holds. Do not
acknowledge the same kind of change again in a later Walkthrough without saying
why it is mechanical this time.

Repeat until the reviewer hands off having raised nothing — `fetch_results` will
say the review is complete.

## Updating a Walkthrough while it is under review

`post_walkthrough` is refused while a review is under review and not handed off.
To change it in place, post again with `replaces` set to its `review_id`. Do
that only when:

- the reviewer asked you, in the chat, for a change during the review, or
- you realise your Walkthrough is wrong before they have got far into it.

Do not use it to slip in changes nobody asked for. Edits the reviewer did not
request wait for the Revision Round, so the review does not move under them.

The replacement is validated like any post and keeps the same `review_id`
(and label, unless you give a new one). The reviewer starts it again from the
top. Their Comments carry over with `carried_over` set and no Step, since the
Steps they were raised on are gone. Replacing a Revision Round needs its
`dispositions` again. It is still scoped against the last round the reviewer
handed off, not against the Walkthrough you replaced.

Never call dbn's `/abandon` endpoint: discarding a review is the reviewer's
decision, not yours.

## Concluding a review

A review that ends this way — the reviewer handing off having raised nothing — is
already concluded; dbn treats `fetch_results` reporting "complete" as the end of
the loop. There is nothing more you must do.

For any other ending — you decide to stop, or the reviewer declines everything and
you will post no further round — call `conclude` with the `review_id` from
`post_walkthrough`. Concluding does not discard anything; it tells dbn the review
is over so it can release the daemon it started for you. A `post_walkthrough`
after that starts a new review with a new `review_id`. A review you never
conclude just leaves the daemon holding it, which is untidy, not harmful.

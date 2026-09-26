---
name: dbn-review
description: Post a Review of your own code changes to diff-by-numbers (dbn) for a human to review. Use this when you have finished a body of work and want it reviewed — instead of handing over a raw diff, plan a narrated, semantically-ordered Round and post it over the dbn MCP server. Also use it to collect the reviewer's Comments and post a Revision Round.
---

# Reviewing your changes with dbn

dbn is a channel for you to walk a human through the code you just wrote, in an
order that makes sense for review rather than the order git happens to print.
You post one complete **Round**; the reviewer navigates it themselves in a
side terminal and raises **Comments** (requests for a change, or questions); you
collect those and post a **Revision Round**. When you need the reviewer to decide
something, you can put an **Agent Question** to them in the Round, and they answer
it. You never block waiting — you post, end your turn, and pick the results up
later.

The dbn daemon exposes four MCP tools: `describe_changes`, `post_round`,
`fetch_results`, and `conclude`. If they are not available, dbn's MCP server is
not registered — see the project README for the one-time setup. (You do not need
to start anything
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

## Planning a Round

A Round is a **Brief** followed by ordered **Steps**. Plan it as a story told to
someone who was not there: decide first what the reviewer needs to understand,
and in what order, and only then fit Excerpts to that plan.

### Telling the story

**Write for a reviewer who was not there.** The work may have been planned days
or weeks before they read it, and they may be moving between several other
tasks. Names you coined while working — for a helper, a phase, an approach you
tried — mean nothing to them until you explain them. Do not assume they remember
decisions made while planning: when one matters to a Step, say what was decided
and why.

**The Brief is the birds-eye view.** It sets context before any code:
- `goal` — what the work set out to achieve, as the reviewer asked for it, in
  their words. Give it in round 1. The Goal belongs to the Review, so dbn
  carries it forward: leave it out of a Revision Round or a replacement, and
  give it again only if what the reviewer wants has changed.
- `approach` — the overall architecture and direction of the change, and the
  order the Steps will take the reviewer through it, so they can judge the
  approach apart from the code. It does not explain every change; that is what
  the Steps are for. When the work came from a spec — an issue, an ADR, a plan
  — name it, so the reviewer can go back to it.

Only the agent that wrote the changes posts their Review; the point is to hear
the story from the one who knows it. If you are working from a summary of that
work — after compaction, say — say so in `approach`.

**Order the Steps to communicate.** There is no fixed direction, top-down or
bottom-up. Choose the sequence that best conveys what the change is about, as
you would organise a document for a reader. Strategies that often work, for the
whole Round or within one part of it:
- **By layer:** the change's overall shape, then each subsystem from its outline
  down to its parts, then the next subsystem.
- **By data flow:** input, then processing, then output.
- **Principles first, then mechanics:** the rule or model the change introduces,
  then the code that applies it.

A new function and its callers may share a Step, or sit back to back in
whichever order reads better.

**The one firm rule: each Step is comprehensible given only the Steps before
it.** A Step may rely on something a later Step shows, provided its explanation
says what that thing does. Understanding a caller takes knowing what its callee
does, not how.

**One idea per Step, even a small one.** A Step carries one self-contained idea
— "add retry with backoff to the fetch layer", "thread the tenant id through the
callers" — not one file, and not one git hunk. A change about something else
gets its own Step, even a one-line deletion in the same file. An explanation
that says "and also changes an unrelated…" or "included because it is in the
same file" is describing two Steps. Changes in contiguous code that are about
different things belong in different Steps too, even inside one function (see
*Splitting one edit across Steps*, below).

**Every explanation says what and why:** what the change is, and how it fits
into the overall work.

**Describe the diff the reviewer will see.** git decides how an edit aligns, and
`describe_changes` reports what it decided: a modification pairs removed lines
with their replacement, and dbn draws the pair before → after. If git aligned
your edit as pure additions, there is no before-side to draw, so do not call it
"modified" and leave the reviewer looking for the old version. Describe what
they will see, or give the before-side yourself with an `old`-side Excerpt.

**Keep Steps small; oversize is a last resort.** Keep a Step to roughly 30
changed lines, and prefer more, smaller Steps: each comprehensible at a glance,
never two functions to hold in your head at once. An edit shows both its before
and after, and the budget counts both sides, so a rewrite fills it faster than
an addition. That pressure is intentional. Split by idea first;
`oversize_justification` is only for a Step whose idea genuinely does not divide,
never a routine alternative to splitting. dbn never refuses a justified Step. An
unjustified one is refused as `oversized_step`, naming each such Step and the
changed lines it counts toward the budget. That count leaves out whitespace-only
lines and lines already shown last round, so it can be lower than the lines
your Excerpts span. In a Revision Round, re-showing already-read context around
a fix costs you nothing.

**A new file is split by meaning.** git reports a new file as one hunk; that is
no reason to show it in one Step. Give each part of it its own place in the
story, across as many Steps as that takes. An Acknowledgement covers a file's
whole change, so acknowledge a new file only when all of it is mechanical — a
generated file, a fixture — never to skip the parts of real code you did not
excerpt.

**Keep Excerpts tight.** Give each group of related changes its own Excerpt,
rather than one wide range across unrelated ones. Unchanged lines are there to
give a change context, not to bridge from one change to the next.

### Asking the reviewer a question

When you need the reviewer to decide something before you go on — a trade-off you
could take either way, a behaviour the goal leaves open — ask it as an **Agent
Question**, not in an explanation. A question written into the narration reads
like the rest of it, and a reviewer reading for the design can miss it. dbn shows
an Agent Question apart, in its own block. It also marks it on the Overview,
counts it until it is answered, and checks with the reviewer before they hand off
without answering.

- **Ask only for a decision or a judgment.** Never "does this look OK?" — the
  whole review asks that. Never for something you should simply state in the
  Brief.
- **Attach it where it belongs.** A question about a Step's code goes in that
  Step's `questions`; it is shown after the Step's explanation and before its
  code, and only there. A question about the approach goes in the Round's own
  `questions`, shown with the Brief.
- **Write it so it can be answered from its Step and the Steps before it.** The
  reviewer reads a Step's question after the Steps leading up to it, never on the
  Overview, so it can lean on them — but not on anything later, or on
  conversation they were not part of.
- **There is no cap, and restraint is what keeps questions noticed.** A Round that
  asks about everything trains the reviewer to skim past the questions.

The reviewer answers in free text, and "your call" is a real answer. A Round that
asks anything is never concluded at Hand Off, answered or not: an Answer may call
for a change, and only you can tell. See *Collecting feedback and revising*.

### The mechanics

**Start with `describe_changes`**, giving it the same `repositories` you will
post, and — when you are planning a Revision Round — the `review_id` of the
review you are revising, so it pre-marks what that review has already shown. It
returns dbn's own account of the change set, worked out exactly as a
post is checked:
- each file's status (`renamed` files say where they came from),
- the Changed Line ranges your Excerpts must cover,
- the `modifications` whose removed lines ride along when you show their
  replacement.

Plan your Excerpts from these ranges, not from `git diff`, which misses what
dbn counts (untracked files, renames) and cannot know what a Revision Round has
already shown. It changes nothing, so call it as often as you like: before
planning, and again before a Revision Round, when it also gives the
`pre_marked_new`/`pre_marked_old` ranges you need not cover and how many lines
are `still_to_cover`.

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
- **Splitting one edit across Steps: say which old lines went where.** When
  Steps divided by idea split a single edit's after-side between them, give each
  Step an `old`-side Excerpt over the lines its new lines replaced, and dbn draws
  each before → after pair in its own Step. Say nothing and the whole before-side
  stays with the Step showing the edit's first new line, and the other Steps get
  a signpost saying where to find it.
- **One Step may span several files** if one idea touches several.

**Acknowledgements** cover mechanical changes you should not make the reviewer
read line by line: a regenerated lockfile, a deleted dead module, a re-exported
binary asset. An Acknowledgement is `{repository, files, reason}` and stands in
for Excerpts on those files. It is the **only** way to cover an Opaque Change — a
binary file, a mode change, a pure rename — which has no lines to excerpt. Use
one when the change is truly mechanical; do not use it to hide real code, because
the reviewer sees the manifest and can expand it into the actual diff — and raise
Comments against it.

**Coverage is enforced.** dbn derives the changed lines from git and refuses a
Round that leaves any of them shown by neither an Excerpt nor an
Acknowledgement. If your post is rejected as `uncovered_changes`, it names
everything you missed, grouped by file and side — add it all and re-post.

**A rejection lists every problem it found.** Once the Round is
structurally sound, dbn runs all its checks and reports all of them in
`problems`, rather than stopping at the first. Fix every entry before posting
again: posting to discover the next one costs you the whole Round each
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

Call `post_round` once, complete. Name every repository under review in
`repositories` (each with its own `base` ref, e.g. the default branch), then the
`brief` and the ordered `steps`. `label` is **required** on a new review — a
short name like "auth refactor", which is how the reviewer tells several reviews
apart.

`post_round` returns a `review_id`. **Record it**: every later call about this
review names it — `fetch_results`, `describe_changes`, the `revises` of a
Revision Round, `replaces`, and `conclude`. It also returns a `message` and a
`wait_command`. After every accepted post — a new review, a Revision Round, or a
Replacement:

1. **Relay the `message`** to the human, since it says how they open the
   review, and say you will pick up their feedback when they are done.
2. **If your harness can run a command in the background and wake you when it
   exits**, run `wait_command` that way, exactly as given. (In Claude Code, that
   is the Bash tool with `run_in_background`.) It blocks, costing nothing, until
   the reviewer hands the review off or dismisses it, then prints one line and
   exits — which wakes you.
3. **End your turn.** Do not poll.

**Never run `wait_command` in the foreground.** It would block your turn until
the reviewer is done, which can be hours. If your harness cannot run it in the
background and wake you, do not run it at all: end your turn, and the human will
tell you when they are done. Do the same if a post result has no `wait_command`:
the dbn you are talking to predates it.

A wait left running from before a Replacement keeps waiting, and a second one on
the same review is harmless: both end on the same event.

Running `wait_command` each round may ask the human for permission every time.
Suggest they allow it once, e.g. with the Claude Code permission rule
`Bash(dbn wait:*)`.

### When the wait ends

When you are woken, read the command's output. It is exactly one line, and never
carries the results — `fetch_results` is still where they are:

- **"… was handed off with N Comments. Call fetch_results …"** — collect them
  and post a Revision Round, as below.
- **"… was handed off with 1 Comment, 2 Answers and 1 unanswered question. Call
  fetch_results with review_id …, work the Comments and questions, then post a
  Revision Round."** — the round asked Agent Questions. The line names only what
  there is: Comments, Answers, unanswered questions. Collect them and post a
  Revision Round that accounts for each, as below.
- **"… was handed off with 2 Answers. Call fetch_results with review_id … and read
  them: conclude if no Answer calls for a change, otherwise post a Revision
  Round."** — every question was answered and nothing else was raised. This is
  not a conclusion: read the Answers and decide, as below.
- **"… was handed off with nothing raised; the review is concluded. Call
  fetch_results …"** — call it: that releases the review, and the loop is over.
- **"… was dismissed by the Reviewer. Call fetch_results …"** — call it to see
  what they raised; see *If they dismiss it* below.
- **"Review … is no longer known to dbn …"** — the daemon lost it, usually by
  restarting. Do not post the work again; tell the reviewer what happened and ask
  them what they want.

**If you lose the id** — after compaction, say — call `fetch_results` with no
`review_id`. You will be refused, and told which reviews dbn is holding, by id,
label and state. Pick yours out and call again with its id. Do not start a new
review to get a fresh one: that leaves the reviewer with two.

## Collecting feedback and revising

When your wait ends with a Hand Off, or the human says they have handed the
review off, call `fetch_results` with your `review_id`. It returns immediately (it never waits)
with the reviewer's Comments, each carrying an anchored reference to the exact
code it concerns. Respond to each: a Comment may ask for a change or ask a
question.

Then post a **Revision Round**: another `post_round`, over the full change set
again, with `revises` set to your `review_id` — that is what makes it a round of
that review rather than a new one — and this time include `dispositions`: one
entry per Comment you were handed, with a `status` you choose and a `response`
the reviewer sees before any
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

**Agent Questions come back too.** `fetch_results` returns each question you
asked, with its `answer`, or marked `unanswered`. An unanswered question is not
agreement — the reviewer handed off without deciding. Give every one a status in
`question_statuses`, with `question_id` and a `response`. dbn shows the reviewer
each question, their Answer and your status before any code, so you need not
restate them:

- `addressed` — the Answer led you to change something. A `response` is optional.
- `no_change_needed` — the Answer agreed with the code as it stood. A `response`
  is optional.
- `agents_call` — only for an unanswered question: you went ahead on your own
  judgment. The `response` is required: what you chose.
- `asked_again` — the question went unanswered, or its Answer cannot be acted on
  as written. The `response` is required: why, or what is still unclear. Ask it
  again as a new question in this Round with `asks_again` set to the old id, on
  the Step whose code it now concerns, or on the Round. The reviewer sees the
  earlier wording and Answer with it, so ask what is still open rather than
  starting over.

There is no `declined`. The reviewer was asked to decide, so you cannot overrule
their Answer. If you cannot follow it, ask again and say why.

**Concluding or revising after questions.** You may `conclude` only when there
are no Comments and every question was answered with nothing for you to change.
Anything else — a Comment, an Answer you acted on, an unanswered question — means
a Revision Round, even if no code moved (see *When nothing moved*, below).

An anchor whose first line says `file changed since this round was posted` was
taken from code you edited after posting: its lines and line numbers are the ones
the reviewer saw, not what is on disk now, so find the place by its code.

A Comment carrying `re_raised_from` is one the reviewer pushed back on: they read
your decline or your answer and did not accept it. It calls for a change, or for
a stronger argument than the one they already rejected — repeating the same
reasoning is not a response.

dbn re-derives everything and pre-marks as already-seen every line the reviewer
read last round and nobody has touched since, so the new Round is scoped to
exactly what you moved. You still plan Steps and coverage for the moved lines the
same way.

Do not label Steps "Changed" or "unchanged" in their names or explanations. In a
Revision Round dbn shades the code by what changed since the previous round, and
marks a Step whose code did not change at all, so the reviewer already sees it. Lines
you removed since the last round are shown to them too, even where no Step points.

Scoping is positional — dbn compares each round against a snapshot of the working
tree it took when the last round was accepted — so it is exact. Blank lines and
lone closing braces are scoped out like anything else, and a file you did not
touch at all needs nothing. That includes Opaque Changes: a binary, a mode change
or a rename you have not re-touched since the last round needs **no second
Acknowledgement**. Only what actually moved comes back.

**When nothing moved.** If you changed no code since the last round — every
Comment `answered` or `declined`, every question `no_change_needed`, your call or
asked again — you still post the Revision Round, because it is what carries your
dispositions, statuses and any question asked again to the reviewer; do not
`conclude` instead. The one exception is a round with no Comments whose every
question was answered with nothing to change: there you conclude.
dbn still needs at least one Step, and a Step must show something, so give it an
Excerpt re-showing code the reviewer has already read: the code the dispositions
are about is the natural choice. Already-read lines cost nothing against the
budget, and dbn marks the Step as unchanged since the last round, so the
reviewer knows there is nothing new in it.

A Comment whose anchor says the code was `acknowledged in Step "…"`
disputes that Acknowledgement as well as the line: the reviewer read code you
called mechanical and found something to say. When you resolve it, say in the
Revision Round's Brief whether the "mechanical" claim still holds. Do not
acknowledge the same kind of change again in a later Round without saying
why it is mechanical this time.

Repeat until the reviewer hands off having raised nothing — `fetch_results` will
say the review is complete — or until you conclude after a round whose every
question was answered with nothing to change.

## Updating a Round while it is under review

A post that names neither `revises` nor `replaces` opens a new review, however
many the daemon already holds: the reviewer picks between them in their Inbox.
So never post new work hoping it lands in a review already open — to change the
round under review in place, post again with `replaces` set to its `review_id`. Do
that only when:

- the reviewer asked you, in the chat, for a change during the review, or
- you realise your Round is wrong before they have got far into it.

Do not use it to slip in changes nobody asked for. Edits the reviewer did not
request wait for the Revision Round, so the review does not move under them.

The replacement is validated like any post and keeps the same `review_id`
(and label, unless you give a new one; a Revision Round keeps it too). The
reviewer starts it again from the
top. Their Comments carry over with `carried_over` set and no Step, since the
Steps they were raised on are gone. Agent Questions they had answered carry over
the same way, with their Answers, and come back to you at the next Hand Off.
Questions they had not answered are dropped, so ask again, in the replacement,
any that still matter — on the Steps that now give them context. You cannot see
mid-round which were answered, so you may ask one again that already was; the
reviewer sees both and resolves them. Replacing a Revision Round needs its
`dispositions` and `question_statuses` again. It is still scoped against the last round the reviewer
handed off, not against the Round you replaced.

Never dismiss a review yourself: discarding one is the reviewer's decision, not
yours.

**If dbn has no record of your review at all** — `unknown_review`, and it is not
in the list a bare `fetch_results` gives you — then it is gone: the daemon was
restarted, or the review ended a while ago. Do not post the same work again to
"recover" it. Tell the reviewer what happened and ask them what they want.

**If they dismiss it**, `fetch_results` comes back with `dismissed` set instead
of results. The review is over: a Revision Round of it will be refused. Anything
they raised before dismissing it comes with the answer — read it, since it is
why they dismissed it — and if there is still work to do, post it as a new
review.

## Concluding a review

A review that ends this way — the reviewer handing off having raised nothing — is
already concluded; dbn treats `fetch_results` reporting "complete" as the end of
the loop. A round that asked any Agent Question never ends this way, since the
Answers are yours to read. There is nothing more you must do, and your next `post_round`
starts a new review with a new `review_id`, not a Revision Round of this one.

For any other ending — you decide to stop, the reviewer declines everything and
you will post no further round, or no Comment was raised and every question was
answered with nothing for you to change — call `conclude` with your
`review_id`.
Concluding does not discard anything; it tells dbn the review
is over so it can release the daemon it started for you. A `post_round`
after that starts a new review with a new `review_id`. A review you never
conclude just leaves the daemon holding it, which is untidy, not harmful.

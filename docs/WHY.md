# Why a new code review tool?
Reviewing an agent's work (planning/design documents or code changes) is the most
time-consuming human-in-the-loop step in agentic software development. Since the main motivator for agentic software development (from a business standpoint) is increasing velocity, developers often feel pressure (external or internal) to skip review steps or at best treat them at a surface level.

## Why do I think traditional code review is broken?
You are handed a diff in an arbitrary order [1], with no explanation, and has to reconstruct *why* from *what*.

[1] Not technically arbitrary, since we're talking about computers. Invariably the files are in alphabetical order by folder hierarchy. In any case, it's not a sequence based on meaning or optimized for understanding.

### Prior art: A walk down memory lane
Before agent-driven development, when I did a set of work, I would try to break it down in progressive steps: Refactor or move files first. Commit. Now make actual changes. Commit. Add next bit of functionality. Commit.

This has a name: "atomic commits." Each commit is standalone, and only does one thing.

Of course, in reality I couldn't actually do my work that way. Building is messy; sometimes we need to try things out and prototype and experiment to figure out how to do something, or we realize we've gone down a wrong path, or find a bug in the code we wrote two commits ago, etc. That's where `git rebase` earns its keep: we do our best to build in a way that works as a linear flow; when we have to deviate or backgrack, we can circle back at the end and rewrite history so it all looks like we did it just right the first time.

(When I first started coding with an agent, I tried to get it to make atomic  commits. That was a losing battle -- it seems that momentum and the training set are against me there.)

Finally we tell code reviewers: "review by commit." Don't _only_ look at the end state; look at the steps in the path to understand the thinking and intention.

It was a beautiful art, and I don't regret it, but I don't miss it either. And it's certainly at odds with "velocity."

### Where does diff-by-numbers fit in
Imagine that as an alternative to atomic commits, code review was done as a presentation. The author that wrote the code walks you through it, not in filename order or even "whole file at a time" but in a sequence and structure that tells the story of the _meaning_ of the code. They can have steps earmarked with "I have a question here about how I did it." At any time the reviewer can say "go back two steps" or "tell me more about this part" or "wait here while I dig deeper" or "I have a question/suggestion on this bit."

dbn is that, for code written by, and built on the backbone of, AI coding agents. The agent that wrote the code walks you through it: a narrated, semantically-ordered walkthrough of the code (with optional "questions from the author") that you navigate yourself, comment on in place (or ask about directly in the waiting agent session), and hand back as a list of comments and answers. The agent takes the feedback and posts an update for review again, round by round, until you have nothing more for it to change.

dbn holds no opinion about the code (it never decides what is shown) and enforces one thing against the agent's account of its own work: every changed line must be shown (the "coverage ledger," derived from git — not from the agent). The agent that wrote the changes is the one that posts them, because the point is to hear the story from the one who knows it.
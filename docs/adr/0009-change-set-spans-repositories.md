# A Change Set spans repositories, and dbn never assumes the session root is one

The Change Set under review is a *list* of repositories, each with its own range. Excerpts
are qualified by repository, coverage aggregates across all of them, and each repository
carries its own default branch. The Authoring Agent names the repositories in the Walkthrough
payload; dbn never runs git in the working directory and never scans for repositories.

The motivating case is real and common: a session root holding many repositories rather than
being one. One such tree here contains 54 repositories at depths one to four, and
`git rev-parse` from its root fails outright. Work routinely spans several of them, because
the applications are many frontends plus many backend microservices. Most review tools assume
session root equals repository root and one application equals one repository, and are
unusable in that layout.

## Consequences

Multi-repository support costs almost nothing when built in from the start and is a rewrite
of the Change Set, the Excerpt reference, the Coverage Ledger and the TUI if retrofitted —
which is why it is in the MVP despite spanning repositories being the uncommon case.

Discovery is the part deliberately not built. dbn does not search for repositories with
changes; the agent already knows which ones it touched, so it says so. A future helper that
finds candidates is additive and changes no model.

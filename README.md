# diff-by-numbers

Agent-driven diff review with paint-by-numbers simplicity.

Reviewing an agent's code changes is the most time-consuming human-in-the-loop
step in agent-assisted development, and the one most often skipped — because the
reviewer is handed a diff in the tool's arbitrary order, with no explanation, and
has to reconstruct *why* from *what*. dbn is a channel for the agent that wrote
the code to walk you through it: a narrated, semantically-ordered **Walkthrough**
you navigate yourself, comment on in place, and hand back as a list of Change
Requests — which the agent works and puts through review again until you finish
having raised nothing.

dbn holds no opinion about the code (it never decides what is shown) and enforces
only two things against the agent's account of its own work: every changed line
must be shown (the **Coverage Ledger**, derived from git — not from the agent),
and the Brief must declare whether its account of intent is *stated* or merely
*inferred*.

See `CONTEXT.md` for the vocabulary and `docs/adr/` for the decisions behind it.

## The end-to-end flow

1. **A daemon runs in the background**, owning the review state and exposing an
   MCP server on `127.0.0.1:7373`. It is separate from any agent session, so
   closing a window loses nothing.
2. **Your agent posts a Walkthrough** over MCP — a Brief plus ordered Steps, each
   a self-contained idea built from line ranges it chose for comprehension. It
   posts once and ends its turn; it does not wait on you.
3. **You review in a terminal** beside your agent session: `dbn` opens the TUI,
   attaches to the daemon, and draws the Walkthrough. You move through Steps,
   select a line range to copy a self-contained **Anchor** into your agent chat,
   or raise a **Change Request** in place.
4. **You finish, the agent collects the Change Requests** with a second MCP call,
   works them, and posts a **Revision Round** — the full change set again, scoped
   by dbn to just what moved, with each of your requests marked addressed or
   declined. Repeat until you finish having raised nothing.

## Install

One line downloads the right prebuilt binary for your machine and puts it on your
PATH — no Go toolchain needed:

```sh
curl -fsSL https://raw.githubusercontent.com/probertson/diff-by-numbers/main/install.sh | sh
```

It installs to `~/.local/bin` and verifies the download against the published
SHA-256 checksum. Two knobs — note the variable goes on the `sh` side of the
pipe, since that is the process that reads it:

- `curl -fsSL … | DBN_VERSION=v0.2.0 sh` installs a specific release instead of the latest.
- `curl -fsSL … | DBN_INSTALL_DIR=/somewhere/bin sh` installs elsewhere.

macOS and Linux, on amd64 and arm64. On Windows, run it inside WSL2 (it uses the
Linux build). `dbn version` confirms the install.

## Build from source

```sh
go build -o /usr/local/bin/dbn ./cmd/dbn
```

(Anywhere on your `PATH` is fine; `dbn version` confirms the build.)

## One-time setup

### 1. Register the MCP server with your agent

dbn speaks MCP over Streamable HTTP. Register it once so every session can reach
it. For Claude Code:

```sh
claude mcp add --transport http dbn http://127.0.0.1:7373/mcp
```

### 2. Install the review skill

Copy the review skill so your agent knows how to plan and post a Walkthrough:

```sh
cp -r .claude/skills/dbn-review ~/.claude/skills/     # available in every project
# or leave it in a project's .claude/skills/ for that project only
```

The skill teaches Step sizing and narrative ordering, Provenance, when an
Acknowledgement is appropriate, and how to run the collect-and-revise loop.

### 3. Always-on daemon (recommended)

So the MCP server is always there and no session reports a failed MCP server, run
the daemon as a login agent:

```sh
# edit the path inside the plist to point at your dbn binary first
cp docs/launchd/com.probertson.dbn.plist ~/Library/LaunchAgents/
launchctl load ~/Library/LaunchAgents/com.probertson.dbn.plist
```

It starts at login and is restarted if it exits. To stop it:

```sh
launchctl unload ~/Library/LaunchAgents/com.probertson.dbn.plist
```

Prefer to run it by hand instead? `dbn serve` in a terminal does the same thing,
and prints `press q to quit`.

## Reviewing

With the daemon running and your agent having posted a Walkthrough, open the
review surface:

```sh
dbn
```

Keys are shown along the bottom: a fixed row of global actions (go to Step,
overview, list, finish, quit) and a row of the current page's actions (move,
select, comment, copy an Anchor, expand an Acknowledgement). If no daemon is
running, dbn says so and tells you to start one — it never leaves you staring at
an empty screen.

Other subcommands: `dbn dump` prints the posted Walkthrough as text, `dbn
abandon` discards it, `dbn version` reports the build.

## Development

```sh
go test ./...      # the review core and git adapter are the tested seams
go vet ./...
```

The review core (`internal/review`) owns the whole life of a Walkthrough and
knows nothing of MCP, git, or the terminal. The git adapter (`internal/git`)
derives changed lines; the working-tree adapter (`internal/workingtree`) reads
and fingerprints files. The daemon and TUI are deliberately thin.

# diff-by-numbers

Agent-driven diff review with paint-by-numbers simplicity.

Reviewing an agent's code changes is the most time-consuming human-in-the-loop
step in agent-assisted development, and the one most often skipped — because the
reviewer is handed a diff in the tool's arbitrary order, with no explanation, and
has to reconstruct *why* from *what*. dbn is a channel for the agent that wrote
the code to walk you through it: a narrated, semantically-ordered **Walkthrough**
you navigate yourself, comment on in place, and hand back as a list of Change
Requests — which the agent works and puts through review again until you hand off
having raised nothing.

dbn holds no opinion about the code (it never decides what is shown) and enforces
only two things against the agent's account of its own work: every changed line
must be shown (the **Coverage Ledger**, derived from git — not from the agent),
and the Brief must declare whether its account of intent is *stated* or merely
*inferred*.

See `CONTEXT.md` for the vocabulary and `docs/adr/` for the decisions behind it.

## The end-to-end flow

1. **A daemon owns the review state**, exposing an MCP server on
   `127.0.0.1:7373`, separate from any agent session so closing a window loses
   nothing. It starts on demand the first time an agent session connects and lets
   go of itself once no review needs it — you never run it by hand.
2. **Your agent posts a Walkthrough** over MCP — a Brief plus ordered Steps, each
   a self-contained idea built from line ranges it chose for comprehension. It
   posts once and ends its turn; it does not wait on you.
3. **You review in a terminal** beside your agent session: `dbn` opens the TUI,
   attaches to the daemon, and draws the Walkthrough. You move through Steps,
   select a line range to copy a self-contained **Anchor** into your agent chat,
   or raise a **Comment** in place: a change you want, or a question.
4. **You hand off, the agent collects the Comments** with a second MCP call,
   responds to them, and posts a **Revision Round** — the full change set again,
   scoped by dbn to just what moved, with each of your Comments marked addressed,
   answered or declined. Repeat until you hand off having raised nothing.

## Installation

One line downloads the right prebuilt binary for your machine and puts it on your
PATH — no Go toolchain needed:

```sh
curl -fsSL https://raw.githubusercontent.com/probertson/diff-by-numbers/main/install.sh | sh
```

It installs to `~/.local/bin` and verifies the download against the published
SHA-256 checksum. There are two options you can specify as environment variables:

- `curl -fsSL … | DBN_VERSION=v0.2.0 sh` installs a specific release instead of the latest.
- `curl -fsSL … | DBN_INSTALL_DIR=/somewhere/bin sh` installs elsewhere.

Note the variables go on the `sh` side of the pipe, since that is the process that reads it.

Currently supports: macOS and Linux, on amd64 and arm64. On Windows, run it inside WSL2 (it uses the
Linux build). `dbn version` confirms the install.

### Updating

dbn checks once a day whether a newer release has been published, and says so in
the TUI header and under `dbn version`. To update:

```sh
dbn update
```

It downloads the release for your platform, verifies it against the published
SHA-256 checksums, and replaces the binary in place. Nothing is downloaded or
replaced unless you run it.

A few things worth knowing:

- **The daemon.** If one is running and no review is in progress, `dbn update`
  stops it so it comes back on the new build (launchd/systemd restart it; an
  on-demand one starts next time your agent needs it). If a review *is* in
  progress the daemon is left alone — its Walkthrough lives in memory — and dbn
  tells you to run the update again once you are done. `dbn update --force`
  restarts it anyway, losing the review. Agent sessions reconnect on their own.
- **If the binary's directory is not writable** (say you installed to
  `/usr/local/bin`), dbn will not use sudo for you. It points you at reinstalling
  somewhere you own with the installer above, or at `sudo dbn update`.
- **Turning the check off.** `DBN_NO_UPDATE_CHECK=1` disables it entirely — no
  network call is made. `dbn update` still works when you ask for it.
- Builds you compiled yourself never check, and never update themselves.

### ... or build from source

```sh
go build -o /usr/local/bin/dbn ./cmd/dbn
```

(Anywhere on your `PATH` is fine; `dbn version` confirms the build.)

## One-time setup

### 1. Register dbn with your agent

Register dbn once. Your agent launches it per session, and it starts the shared
dbn daemon on demand — so the review tools are always present, with nothing to
start by hand first. For Claude Code, register it for every project:

```sh
claude mcp add -s user dbn -- dbn mcp
```

Without `-s user`, `claude mcp add` uses its default scope, `local`: dbn is
registered only for the project you ran the command in, so it is missing
everywhere else. To register it for just one project, run this from that
project's directory:

```sh
claude mcp add dbn -- dbn mcp
```

(`dbn` must be on your PATH for your agent to launch it.)

### 2. Install the review skill

Copy the review skill so your agent knows how to interact with dbn:

**Via `npx skills`**
```sh
npx skills add probertson/diff-by-numbers/skills/dbn-review
```

**As a Claude Code plugin**
```sh
# Add marketplace, one time
/plugin marketplace add probertson/diff-by-numbers
/plugin install dbn@diff-by-numbers
```

The skill teaches Step sizing and narrative ordering, Provenance, when an
Acknowledgement is appropriate, and how to run the collect-and-revise loop.

### 3. (Optional) Keep the daemon always running

You do not need this: the shim starts the daemon on demand, and it stops itself
once no review needs it. But if you would rather the daemon be permanently warm —
so the first review of a session has nothing to start — run it on login/startup.
This is purely a pre-warm; the shim simply finds and shares a daemon that is
already running.

**NOTE:** `dbn` needs to be on your PATH for these. Both commands below write
the absolute path of your dbn (`$(command -v dbn)` — normally
`~/.local/bin/dbn`, where the installer puts it) into the service definition,
because neither launchd nor systemd expands `~`. `dbn update` replaces that
binary in place, so the path stays good across updates; if you move dbn
elsewhere, rewrite the definition.

#### macOS
```sh
cat > ~/Library/LaunchAgents/com.probertson.dbn.plist <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.probertson.dbn</string>
  <key>ProgramArguments</key>
  <array>
    <string>$(command -v dbn)</string>
    <string>serve</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>/tmp/dbn.log</string>
  <key>StandardErrorPath</key>
  <string>/tmp/dbn.err.log</string>
</dict>
</plist>
EOF

launchctl load ~/Library/LaunchAgents/com.probertson.dbn.plist
```

It starts at login and is restarted if it exits. To stop it:

```sh
launchctl unload ~/Library/LaunchAgents/com.probertson.dbn.plist
```

#### Linux/Unix (via systemd): install as a user service

```sh
mkdir -p ~/.config/systemd/user

cat > ~/.config/systemd/user/dbn.service <<EOF
[Unit]
Description=dbn review daemon (MCP server on 127.0.0.1:7373)

[Service]
ExecStart=$(command -v dbn) serve
Restart=always
RestartSec=2

[Install]
WantedBy=default.target
EOF

systemctl --user enable --now dbn.service
```

To stop it:
```sh
systemctl --user disable --now dbn.service
```

To read its logs:
```sh
journalctl --user -u dbn
```

## Using dbn

1. Ask your agent to "review [describe set of changes, for example 'the last
two commits'] with dbn." The daemon starts automatically the first time your
agent uses dbn — you do not need to start anything.

2. In a separate terminal, open the TUI:

```sh
dbn
```

You can open it before your agent is ready: with no daemon yet, the TUI waits for
one and fills in the moment the Walkthrough is posted.

The first screen shows an overview. Use Left/Right arrows to navigate through screens.
Select lines to copy-by-reference (for pasting to your agent, if you want to ask questions
mid-review) or to add a Comment. When you're done, press `h` to hand the review off, then
tell your agent. It will then retrieve your Comments. Handing off is not leaving: `q` exits
the viewer at any time without losing anything, and `dbn` reopens to the same review.

Other subcommands: `dbn dump` prints the posted Walkthrough as text, `dbn
abandon` discards it, `dbn version` reports the build, `dbn update` installs a
newer one (see [Updating](#updating)).

## Development

```sh
go test ./...      # the review core and git adapter are the tested seams
go vet ./...
```

The review core (`internal/review`) owns the whole life of a Walkthrough and
knows nothing of MCP, git, or the terminal. The git adapter (`internal/git`)
derives changed lines; the working-tree adapter (`internal/workingtree`) reads
and fingerprints files. The daemon and TUI are deliberately thin.

### Cutting a release

```sh
scripts/release.sh v0.1.0   # an explicit version
scripts/release.sh MINOR    # or bump the latest tag: MAJOR, MINOR or PATCH
```

A bump keyword reads the latest `vX.Y.Z` tag (after fetching tags from origin)
and bumps it, resetting the lower parts: `MINOR` takes v0.2.0 to v0.3.0. It validates (clean tree, on `main`, tag unused, tests pass), pushes `main` if
needed, then tags and pushes the tag — which triggers the release workflow that
builds and publishes the binaries. Pass `SKIP_TESTS=1` to skip the test gate.

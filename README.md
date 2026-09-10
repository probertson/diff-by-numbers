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

### ... or build from source

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

### 3. Always-on daemon (recommended)

For the "don't touch my system" option, before asking your agent for a review 
you must start the MCP server:

```sh
dbn serve
```

If you want the "set it and forget it" option, (the MCP server is always running)
run the daemon on login/startup.

**NOTE:** `dbn` needs to be on your PATH to run these commands, so before
running them (immediately after installation) you may need to open a new terminal 
window.

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

1. If the MCP server isn't already running, open a separate terminal and run:
```sh
dbn serve
```

2. Ask your agent to "review [describe set of changes, for example 'the last
two commits'] with dbn."

3. In a separate terminal, open the TUI:

```sh
dbn
```

The first screen shows an overview. Use Left/Right arrows to navigate through screens.
Select lines to copy-by-reference (for pasting to your agent, if you want to ask questions
mid-review) or to add a change request. When you're finished, press `f` and then tell your agent
you've finished. It will then retrieve your change requests.

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

### Cutting a release

```sh
scripts/release.sh v0.1.0
```

It validates (clean tree, on `main`, tag unused, tests pass), pushes `main` if
needed, then tags and pushes the tag — which triggers the release workflow that
builds and publishes the binaries. Pass `SKIP_TESTS=1` to skip the test gate.

# diff-by-numbers (`dbn`)

Agent-driven diff review with paint-by-numbers simplicity.

(Screenshot to be placed here =)

## Contents

1. [What is this and why would I want to use it](#the-why)
2. [Installation](#installation)
3. [One-time setup](#one-time-setup)
4. [Using dbn](#using-dbn)
5. [Contributing](#contributing)

## The _Why_

**TL;dr:** speed up the human part of agentic coding while also improving the quality of the experience and the quality of the output.

[The long version](./docs/WHY.md)

## Installation

Run this command to download a prebuilt binary for your machine and put it on your
PATH:

```sh
curl -fsSL https://raw.githubusercontent.com/probertson/diff-by-numbers/main/install.sh | sh
```

### What it does:

It installs to `~/.local/bin` and verifies the download against the published
SHA-256 checksum. There are two options you can specify as environment variables:

- `curl -fsSL … | DBN_VERSION=v0.2.0 sh` installs a specific release instead of the latest.
- `curl -fsSL … | DBN_INSTALL_DIR=/somewhere/bin sh` installs elsewhere.

Note the variables go on the `sh` side of the pipe, since that is the process that reads it.

Currently supports: macOS and Linux, on amd64 and arm64. On Windows, run it inside WSL2 (it uses the Linux build). `dbn version` confirms the install.

### Additional Requirements

dbn shells out to `git`, and needs **git 2.25 or newer** (January 2020) — it derives the Change Set with pathspec files, which older git does not read. `git --version` confirms yours.

(Anywhere on your `PATH` is fine; `dbn version` confirms the build.)

## One-time setup: Connect dbn to your agent

You need to set up two things for your agent:

- the dbn MCP server, which gives it access to dbn
- the review skill, which teaches it to use dbn

The agent launches the MCP server per session, and that starts the dbn backend on demand, so the review tools are always there with nothing to start by hand first. In order for this to work, `dbn` must be on your PATH for your agent to launch it. The skill includes instructions on how to call the tool, and _also_ guidelines on how to structure a review. If you want to give your agent custom instructions (e.g. "always put section X first" or "I don't care about seeing changes of type Y") you can create your own skill with those extra instructions, and your agent will pick it up and use it.

To get them:

- Claude Code (via plugin): [install the plugin](#claude-code-install-the-plugin)
- Other: [manual setup](#manual-setup)

#### Claude Code: install the plugin

The plugin brings both the skill and the MCP server. In a Claude Code session:

```
/plugin marketplace add probertson/diff-by-numbers
/plugin install dbn@diff-by-numbers
```

(`/plugin install` opens the plugin panel on dbn, where you choose a scope to install it. The user scope makes dbn available in every project.)

Or from a terminal:

```sh
claude plugin marketplace add probertson/diff-by-numbers
claude plugin install dbn@diff-by-numbers
```

##### Optional: auto-update the plugin

The plugin does not auto-update by default. (Claude Code sets auto-update to
"off" for third-party marketplaces by default.) To enable auto-updating for the
diff-by-numbers plugin/skill:

1. `/plugin` — open the plugin panel
2. Switch to the "Marketplaces" tab
3. Choose "diff-by-numbers"
4. Choose "Enable auto-update"

Otherwise, `dbn update` will tell you when your skill is out of date with the
dbn you installed, and how to update it.

#### Manual setup

For non-Claude setups, or to set up dbn without installing the plugin:

1. add the skill
2. register the MCP server

Install the review skill, for example with `npx skills`:

```sh
npx skills add probertson/diff-by-numbers/skills/dbn-review
```

Then register `dbn mcp` as a stdio MCP server with your agent:

- Claude Code: without the plugin:

  ```sh
  claude mcp add -s user dbn -- dbn mcp
  ```

- Codex (untested): from the Codex desktop app, add a "stdio" MCP server with command `dbn mcp`, or from your terminal:

  ```sh
  codex mcp add dbn -- dbn mcp
  ```

## Updating

See [UPDATING.md](./docs/UPDATING.md)

TL;dr: dbn checks once a day whether a newer release has been published, and says so in the TUI header and under `dbn version`. To update, run:

```sh
dbn update
```

## Using dbn

1. Ask your agent to "review \[describe set of changes, for example 'the last
   two commits'] with dbn." The daemon starts automatically the first time your
   agent uses dbn.

2. In a separate terminal, open the TUI:

```sh
dbn
```

You can open it before your agent is ready: with no daemon yet, the TUI waits for one and fills in the moment the Round is posted.

dbn opens on the "Inbox": every review that has been posted to dbn, and whether it's waiting on you or the agent. Move with the arrows, `enter` opens the one under the cursor where you left it, and `i` returns you to the inbox, leaving your review as-is. `d` dismisses a review you don't want, after a y/n confirmation — it discards the comments you raised, and the agent is told you dismissed it.

Inside a review, the first screen is the Overview. Use Left/Right arrows to navigate through screens. Select lines to copy-by-reference (for pasting to your agent, if you want to ask questions mid-review) or to add a comment. Press `a` to answer an agent question where the agent asked it. When you're done, press `h` to hand off the review.

If your agent's harness can wait on a background command, the agent is told directly and the screen says so; otherwise it tells you to let the agent know. Either way, the agent then retrieves your comments.

Handing off is not leaving: `q` exits the viewer at any time without losing anything, and `dbn` reopens to the inbox.

### Other subcommands

- `dbn wait <review_id>` blocks until you hand that review off or dismiss it (the agent runs it in the background, from the command dbn gives it)

- `dbn dump` prints every review the daemon holds as text
- `dbn version` reports the current version
- `dbn update` installs a newer version if one is available (see [Updating](./docs/UPDATING.md))
- `dbn config` shows your settings (see [Configuration](./docs/CONFIG.md))

## Contributing

See [DEVELOPING.md](./docs/DEVELOPING.md)

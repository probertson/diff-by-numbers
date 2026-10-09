# A config file

dbn reads its settings from `$XDG_CONFIG_HOME/dbn/config.toml`, or `~/.config/dbn/config.toml`
when `XDG_CONFIG_HOME` is unset (#161). It starts with the two settings that were environment
variables, `port` and `update-check`; the diff colours (#91) will be the first setting that is
new with it. ADR-0012 put a settings file off until a second setting arrived. The colours are
that setting, and the port and update check move in ahead of them rather than leaving dbn with
two ways to say the same kind of thing.

## `~/.config`, on macOS too

Go's `os.UserConfigDir()` gives `~/Library/Application Support` on macOS. dbn builds the path
itself instead: it is a terminal tool, and terminal tools (and the people who use them) look in
`~/.config`. A relative `XDG_CONFIG_HOME` is ignored, as the XDG spec says.

## TOML

Comments, no indentation to get wrong, and tables (`[diff.dark]`) that read clearly. JSON has no
comments, and YAML's whitespace and implicit typing are traps in a file edited by hand.

## Flag, then environment, then file, then default

The more specific and shorter-lived a source, the more it wins. `DBN_PORT` and
`DBN_NO_UPDATE_CHECK` stay, undeprecated: they cost nothing, they let one command override the
file without editing it, and anyone already using them sees no change. One exception to plain
precedence: `DBN_NO_UPDATE_CHECK` can only turn the check off. Its "no" values (`0`, `false`,
`no`) count as unset, so a variable named "no update check" is never what switches the check
on. `DBN_EXIT_GRACE` stays an environment variable only; it is a knob for tests, not a
preference.

## An invalid setting stops dbn; an unknown key only warns

A file that doesn't parse, a bad value, or an unparseable `DBN_PORT` stops every process that
reads it, with an error naming the source, the line and the key. Falling back to defaults
instead would turn a typo into processes on different ports — the TUI on one daemon, the agent's
shim on another, or on none — which shows up as "dbn can't see my Review", far from its cause.
A setting that something else outranks still fails: the file is wrong either way, and the error
is where it gets fixed.

An unknown key is different. The repo-built `./dbn` and the installed `dbn` share the file, so a
key only the newer one knows must not stop the older one. And a misspelled key is ignored the
same way by every process, so they still agree on the port. It warns: on stderr from a CLI
command, as a persistent notice in the TUI, and from `dbn config`.

## The shim reports through its tools

The agent harness launches `dbn mcp`, and its stderr reaches no one. Exiting would leave the
agent without dbn tools and with no idea why. So with invalid settings the shim still serves
this build's tools (listed from the daemon package in-process, since there is no daemon to ask),
and every call returns the error as a tool error, which the agent shows the Reviewer. Each call
re-reads the settings, so fixing the file needs no restart; from the first valid read the shim
behaves as it always has, and its settings are fixed for the session.

## Read at launch

A running process doesn't notice edits. Watching the file would raise the question of what a
running TUI does with a broken save, and the port cannot change under a running session anyway.

## Room for richer diff styles

A diff colour's value is a `#rrggbb` string, and that string means the **background**. An
inline-table form (`{ fg = "…", strikethrough = true }`) is reserved for later, for presets that
colour text or mark changes instead of tinting the line (#147); the decoder can tell a string from
a table, so a file written today keeps working. delta's style strings were not copied, because in
them a lone colour is the foreground. For the same reason the light/dark key is `background`, not
`theme`, leaving "theme" free for named looks.

## Consequences

An unparseable `DBN_PORT` used to be ignored silently, falling back to the default port. It is
now an error.

`dbn config` prints where the file is, checks it, and lists each setting with where its value came
from, so "why is dbn on that port?" is one command.

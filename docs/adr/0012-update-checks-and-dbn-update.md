# Update checks and `dbn update`

dbn tells the Reviewer when a newer release exists, and replaces its own binary when they
ask it to. The check runs by default; nothing is ever downloaded or replaced without an
explicit `dbn update`.

## dbn now talks to something other than loopback

Until now every byte dbn sent went to 127.0.0.1, and the daemon's listener says so in code
(`internal/daemon/daemon.go`): a review surface has no reason to be reachable from the
network. The update check is the first deliberate exception, and it is worth being explicit
that it is one.

It is kept as small as the job allows: one unauthenticated `GET` of the GitHub
latest-release endpoint, a three-second timeout, and an answer cached for a day (an hour
after a failure) in `os.UserCacheDir()/dbn/update-check.json`. It sends nothing — no
identifier, no telemetry, not even a version — so the request says only that some IP asked
what the newest release is. The **daemon** never makes it: the check belongs to the TUI
process and to `dbn version`, so the always-on background process stays loopback-only,
exactly as before.

The alternative — say nothing, and let people discover releases themselves — loses the case
this exists for: dbn is installed by a curl-to-shell one-liner, which means nothing will ever
tell the Reviewer a release happened.

## Default on, with an environment variable to turn it off

`DBN_NO_UPDATE_CHECK=1` disables it. Default-on is the decision worth defending: a check
nobody enables is a check nobody has, and the cost here is one cached request a day that
carries no information about the person making it.

It is an environment variable rather than a setting because dbn has no settings file, and one
option is not enough reason to introduce one — a settings file is a format, a location, a
migration story and a precedent. The second setting can bring it, and this variable can
become one of its keys.

Development builds never check. A build that was not stamped by the release pipeline was
compiled by whoever is running it; there is no published release it is meaningfully behind,
and its binary did not come from a download that could be replaced.

## Binaries are replaced only by an explicit `dbn update`

No background download, no self-replacement on startup, no "restart to apply". A tool that
rewrites its own executable while someone is mid-task is a tool that gets uninstalled, and
the surprise is worst in exactly the situation dbn is for: a review in flight.

`dbn update` verifies the download against the release's published checksums before anything
touches the disk, then stages the new binary beside the old one and renames over it — the
same swap `install.sh` makes, and for the same reason: a rename inside one filesystem cannot
half-happen, where a copy interrupted partway leaves no working dbn at all.

Where the binary's directory is not writable, dbn does not reach for sudo. It names the
directory and offers reinstalling somewhere the Reviewer owns (`install.sh` defaults to
`~/.local/bin`) before mentioning `sudo dbn update` as the in-place alternative. A tool that
escalates on its own behalf teaches a habit worth not teaching.

## The daemon is restarted only when it is idle

The binary the daemon started from is gone the moment the swap lands, but the running process
is not. dbn asks the daemon what it is doing (`GET /status`) and acts on the answer: with no
review active it calls `POST /shutdown`, and launchd, systemd or the next agent session brings
a daemon back on the new build. With a review in progress it leaves the daemon alone and says
so, because the Walkthrough lives in that process's memory and stopping it would throw away
the Reviewer's work. `--force` is there for someone who has decided the review is expendable.

A daemon whose executable is not the copy just replaced is never restarted: stopping it would
only bring back the same old binary from wherever its service definition points.

## Consequences

The TUI can find itself talking to a daemon of a different version, which no protocol version
negotiation covers. It warns — persistently, in the header — and never refuses to connect:
locking the Reviewer out of a review in progress is the worse failure of the two.

The daemon anyone updating for the first time is running predates `/status` and answers it
with a 404. That is treated as an answer, not as an absence — it proves the daemon is older
than the binary asking — so both the TUI notice and `dbn update` say something about it
rather than silently skipping the one case they were written for.

The shim now reconnects when a forwarded call fails, so an agent session survives the daemon
being replaced under it. The tool list it mirrored at startup does not refresh, so tools added
by a newer release appear only when the agent restarts its MCP server.

The plugin/skill ships separately from the binary and still versions independently. That gap
was recorded as its own issue rather than settled here, and #64 has since closed it: cutting a
release writes the version into `.claude-plugin/plugin.json`, so Claude Code offers the plugin
update at all, and `dbn update` ends by re-execing the newly installed binary as `dbn
skill-check`, which compares the skill it embeds against the copies in the two locations dbn
installs to. The versions still move independently — nothing here makes a stale skill refuse to
run — but drift is now reported instead of silent. The skill's "Requires dbn" line, the
oldest release it works with, is still set by hand. A release warns when the MCP schema changed
since the last one but the line did not, or when the line names a release that is not out yet.

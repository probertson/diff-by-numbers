# Updating

dbn checks once a day whether a newer release has been published, and says so in the TUI header and under `dbn version`. To update:

```sh
dbn update
```

It downloads the release for your platform, verifies it against the published
SHA-256 checksums, and replaces the binary in place. Nothing is downloaded or
replaced unless you run it.

A few things worth knowing:

- **The MCP daemon.** If one is running and no review is in progress, `dbn update`
  stops it so it comes back on the new build (launchd/systemd restart it; an
  on-demand one starts next time your agent needs it). If a review *is* in
  progress the daemon is left alone — its review lives in memory — and dbn
  tells you to run the update again once you are done. `dbn update --force`
  restarts it anyway, losing the review. Agent sessions reconnect on their own.
  
- **If the binary's directory is not writable** (say you installed to
  `/usr/local/bin`), dbn will not use sudo for you. It points you at reinstalling
  somewhere you own with the installer above, or at `sudo dbn update`.
- **Turning the check off.** `update-check = false` in the [config
  file](./CONFIG.md), or `DBN_NO_UPDATE_CHECK=1`, disables it entirely — no
  network call is made. `dbn update` still works when you ask for it.
- Builds you compiled yourself never check, and never update themselves.

# diff-by-numbers

## Agent skills

### Issue tracker

Issues live in GitHub Issues at `probertson/diff-by-numbers`, via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage roles, each label string equal to its name. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.

## Manual verification build

The binary used for manual verification is always the repo-root `./dbn` (gitignored). After a code change the user wants to try, rebuild it with `go build -o ./dbn ./cmd/dbn` — not `~/.local/bin/dbn` or anywhere else on `PATH`. The user launches both the TUI (`./dbn`) and the daemon (`./dbn serve`) from here, so the daemon restarts too when a change touches it (TUI-only changes need only the TUI relaunched, since the keybar and rendering are client-side).

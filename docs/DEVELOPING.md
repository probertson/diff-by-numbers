# Tips for developing on diff-by-numbers

See `CONTEXT.md` for the vocabulary and `docs/adr/` for the decisions behind it.

## Architecture

The review core (`internal/review`) owns the whole life of a Review and
knows nothing of MCP, git, or the terminal. The git adapter (`internal/git`)
derives changed lines; the working-tree adapter (`internal/workingtree`) reads
and fingerprints files. The daemon and TUI are deliberately thin.

## Trying a development build

A development build can run beside the released dbn without replacing it. Build
it at the repository root, then register it once as a second MCP server on its
own port, from the repository root:

```sh
go build -o ./dbn ./cmd/dbn
claude mcp add dbn-local -s user -- "$PWD/dbn" mcp -port 7374
```

Every agent session then has both servers: the release's `dbn` (on 7373) and
`dbn-local` (this build, on 7374). Open the Reviewer's side with `./dbn -port
7374`.

There is no need to run `./dbn serve -port 7374`: each session's `dbn-local`
shim starts a daemon on 7374 from `./dbn` if none is running, which is why a
hand-run one may find the port already in use. That daemon lets itself go once
no review needs it and nothing has talked to it for a minute, and the next
session to need it starts another from whatever `./dbn` is then.

The installed `dbn-review` skill is the released one, and the `wait_command` a
post returns runs whichever `dbn` is on your PATH, which is also the release. So
tell the test session to use the development copies of both, e.g.:

> Use the `dbn-local` tools and follow `<repo>/skills/dbn-review/SKILL.md`
> rather than the installed `dbn-review` skill. When you run `wait_command`,
> replace the leading `dbn` with `<repo>/dbn`.

where `<repo>` is the absolute path of this checkout. The command already
carries `-port 7374`, so it reaches the development daemon. To skip the
permission prompt each round, allow `Bash(<repo>/dbn wait:*)`.

## Running tests

```sh
go test ./...      # the review core and git adapter are the tested seams
go vet ./...
```
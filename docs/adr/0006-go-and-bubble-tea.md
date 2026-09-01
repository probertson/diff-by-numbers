# Go and Bubble Tea, despite a TypeScript codebase everywhere else

dbn is written in Go using Bubble Tea, and not in TypeScript, even though every
neighbouring project is TypeScript/React/Vitest and the author's fluency is there.

Three reasons outweighed familiarity. dbn's real shape is a long-lived server holding
several blocked reviewer calls open while a TUI runs concurrently — a concurrency
problem Go models directly. Bubble Tea is a stronger TUI foundation than anything
available in TypeScript, and the diff pane is the whole product. And a single static
binary gives the portability a Node runtime dependency would not.

The official Go MCP SDK reached v1.0.0 with a no-breaking-change guarantee and
supports Streamable HTTP, so the chosen server topology is directly supported.

## Consequences

The author reviews agent-written Go while learning Go — the exact activity dbn is
being built to improve, made harder, before dbn exists to help.

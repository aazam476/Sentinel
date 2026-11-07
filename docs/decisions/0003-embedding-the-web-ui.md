# 0003 — Embedding the web UI in the server binary

**Date:** 2026-11-07 · **Status:** Accepted

## Context

The server must ship as a single binary that also serves the React web UI. Go's `embed`
package can include files in the binary at compile time, but a `//go:embed` directive fails
to compile if the folder it points to is empty or missing. On a fresh checkout the UI hasn't
been built yet, and we don't want `go build`, `go vet` or the Go tests to require Node.js.

A second problem: some npm packages ship `.go` files inside `node_modules`, so
`go test ./...` would try to build them.

## Decision

- `make web` builds the UI with Vite into `web/dist/` and copies it into
  `internal/server/web/dist/`, which the server embeds with `//go:embed all:dist`.
- That folder is in git with only a `.gitkeep` placeholder, so the embed always compiles.
  Build output in it is ignored by git.
- If the binary was built without the UI, the server answers every web request with
  `503 Service Unavailable` and a message saying to run `make build`, rather than failing to
  start.
- Folder paths other than `/` return 404 instead of a directory listing.
- `web/go.mod` marks `web/` as a separate Go module, which makes the Go tools skip
  `web/node_modules`.

## Consequences

- Go code compiles, lints and tests without Node.js installed.
- Release builds always run `make build` (or `make web` first), so the UI is always embedded.
  The command-line contract tests check that the served page contains the app.
- `web/go.mod` looks odd at first; it carries a comment explaining why it exists.

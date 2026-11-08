# Contributing to Sentinel

Thanks for helping. The single most important quality of this codebase is that **a junior
developer still in college can open any file, understand what it does and why, and make a
correct change.** Every rule below serves that goal.

## Set up (≤ 10 commands)

You need **Go 1.27+**, **Node.js 22+**, **make**, **bash** and **git**. On Windows, use Git
Bash. For linting you also need **golangci-lint v2.15+**.

```bash
git clone https://github.com/aazam476/sentinel.git
cd sentinel
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
make web-deps          # npm packages for the web UI
cd web && npx playwright install chromium && cd ..   # browser for end-to-end tests
make test-all          # everything CI runs, must pass before you commit
make dev               # run it
```

`go` downloads the exact Go toolchain named in `go.mod` on first use, so an older local Go
is fine as long as it's at least 1.21.

## Run a local server

```bash
make dev
```

This builds the web UI and the server, starts `sentinel-server` on
<https://127.0.0.1:7443> with a self-signed certificate, and starts the Vite dev server on
<http://localhost:5173> with hot reload. **Use <http://localhost:5173>.** It is the
configured public URL in development, and requests to `/api`, `/healthz` and `/agent` are
forwarded to the Go server. Press Ctrl+C to stop both.

The development settings live in `.dev/server.toml` and the data in `.dev/data/`; delete
`.dev/` to start again from a fresh install. See
[ADR 0007](decisions/0007-server-configuration-and-tls.md) for every setting. A local agent
will be started and enrolled by `make dev` once agents exist.

## Run the tests

| Command | What it runs |
|---|---|
| `make test` | Go unit tests (white- and black-box) with `-race`, plus web unit and component tests |
| `make test-integration` | HTTP, protocol and command-line contract tests (Go build tag `integration`) |
| `make test-e2e` | Playwright against the real server binary, at desktop and phone width, with and without reduced motion |
| `make test-all` | All of the above, plus every linter, the SEC traceability check, license checks and the prototype check |
| `make fuzz` | Each fuzz target for 30 s (`make fuzz FUZZTIME=2m` to change it) |
| `make cover` | Coverage, checked against the minimums below |

Run `make help` for every command.

**A change is done only when `make test-all` passes.** Never commit with failing, skipped
or flaky tests. A skipped test needs a linked issue and a comment explaining why.

### Coverage minimums

| Scope | Minimum statement coverage |
|---|---|
| Security-critical Go packages (auth, audit, protocol, agent identity, policy, update, handshake, secrets encryption) | 95% |
| All other Go packages with logic | 85% |
| Web UI (`web/src`) | 80% |
| `cmd/` wiring, test helpers and generated files | Excluded |

## How to build a feature

Follow this order every time:

1. Read what the feature must do, including any `SEC-xx` security requirements.
2. **Write black-box tests first**, from the requirements only: package API tests in an
   external test package, HTTP and command-line contract tests, and an end-to-end test for
   each "done when" criterion. They fail at this point; that's expected.
3. Implement the feature.
4. **Write white-box tests** for every branch, error path and edge case in the new code.
5. Run `make test-all` and fix failures. Never weaken or delete a test to make it pass
   unless the test itself was wrong, and say so in the commit message.
6. Check `make cover`.
7. Update the docs if anything changed.
8. Commit and push (see below), then check that CI is green on Linux, macOS and Windows.

### White-box and black-box tests

| | White-box | Black-box |
|---|---|---|
| Package declaration | `package zfs` | `package zfs_test` |
| File name | `<name>_internal_test.go` | `<name>_test.go` |
| Can call | Unexported functions and internal state | Exported API, HTTP, protocol, command line or browser only |
| Written | During and after implementation | Before implementation |

Every Go package with logic has both kinds.

### Test rules

- **Table-driven tests** whose case names read as sentences:
  `"returns error when pool name is empty"`.
- **No real OS calls in unit tests.** Code that runs commands or calls OS APIs depends on a
  small interface (for example a `CommandRunner`); tests use a fake that asserts the
  **exact** command and arguments.
- **Parsers are tested against real captured output** in `testdata/`.
- **Fuzz tests** for anything that parses untrusted or external input.
- **Deterministic time:** depend on a `Clock` interface. No `time.Sleep` in tests.
- **Security traceability:** every `SEC-xx` item has a test whose name contains its ID, such
  as `TestSEC05_LocksAccountAfterTenFailures` (Go) or a test titled `'SEC-05 ...'` (web).
  When you write the first code for a `SEC-xx` item, add it to `scripts/sec-items.txt`;
  `make check-sec-coverage` fails if a listed item has no test, or if code mentions an item
  that isn't listed.
- **Web tests** find elements by role and label, the way people do, never by CSS class, and
  include an accessibility check.

## Go rules

| Rule | Limit / detail |
|---|---|
| Function length | Aim ≤ 40 lines; the linter stops you at 60 |
| File length | Aim ≤ 300 lines; review flag at 500 |
| Nesting depth | ≤ 3 levels; use early returns |
| Parameters | ≤ 4; more than that, pass a named struct (`RestartOptions`) |
| Package docs | Every package has a `doc.go`: what it does, what calls it, what it calls |
| Exported identifiers | Every one has a doc comment |
| Names | Full words. Short names only for `i`, `j`, `w`, `r`, `ctx`, `err`, `tx` |
| Errors | Always wrap with context: `fmt.Errorf("restart container %s: %w", id, err)`. Never ignore one without a comment saying why |
| Panics | Never outside `main()` and tests |
| Global state | No package-level mutable variables; pass dependencies in struct fields |
| `init()` | Not allowed, except in tests |
| Generics | Only when they remove real duplication in ≥ 3 places, with a comment saying why |
| Interfaces | Small (1–3 methods), defined in the package that uses them |
| Goroutines | Every goroutine has an owner, stops when its context is canceled, and has a comment stating its lifetime |
| Logging | `log/slog`, structured: `logger.Info("container restarted", "container_id", id)` |
| External commands | `exec.CommandContext(ctx, name, args...)` with separate arguments, never a string built by concatenation |
| OS workarounds | Leave a comment with a link to the documentation that explains the odd behavior |

Platform-specific code lives in build-tagged files with identical function signatures
(`services_linux.go`, `services_darwin.go`, `services_windows.go`); the shared file holds the
types and docs, and each platform package has a fake for tests.

Comments explain **why**; code explains **what**. If a comment says what the next line
does, improve the line instead.

## Web UI rules

| Rule | Detail |
|---|---|
| One component per file | File name = component name: `ContainerList.tsx` |
| Component size | Aim ≤ 150 lines; split into children beyond that |
| Types | `strict: true`. No `any` (use `unknown` and narrow) |
| Data fetching | TanStack Query hooks in `web/src/api/`, one file per resource. Components never call `fetch` |
| State | Server state in TanStack Query, local UI state in `useState`, shared client state in a small zustand store per concern in `web/src/stores/` |
| Styling | CSS Modules using the tokens in `web/src/styles/tokens.css` |
| Effects | No `useEffect` for data fetching or derived state. Every `useEffect` gets a comment saying why it's needed |
| Naming | Handlers `handleX`, callback props `onX`, booleans `isX` / `hasX` |
| Class names | `clsx` for conditional classes |
| Primitives | Dialogs, popovers, menus, selects and tooltips come from Base UI, never hand-rolled |

**Motion.** Sentinel is used many times a day, so frequent actions (opening a machine,
switching tabs, refreshing a list, typing in the terminal) don't animate. Motion is for
orientation, feedback and state changes that would otherwise be missed. Every animation has
a `prefers-reduced-motion` version. Streaming data (live metrics, terminal output, the
desktop stream) never updates through React state on every frame.

**Phones.** Every screen must work at 390 × 844. Gate hover styles behind
`@media (hover: hover) and (pointer: fine)`, keep inputs at 16px or larger, pad fixed
elements with `env(safe-area-inset-*)`, and use `dvh`/`svh` rather than `vh`.

**Worst-case data.** Every component that renders server data has a worst-case fixture in
`web/src/test/fixtures/worst-case/` (very long names, unbreakable strings, non-Latin text
and emoji, empty lists, 1,000+ items, missing fields, huge and negative numbers, offline
agents), and its tests render both the normal and worst-case fixtures.

**Design variant pickers** are temporary tools for comparing design options. Mark a
picker's root element with a `data-variant-picker` attribute and remove it once a variant
is chosen; `make check-no-prototype` fails if one reaches the production build.

## Dependencies

Prefer the standard library. Before adding any dependency, add a row to
[DEPENDENCIES.md](DEPENDENCIES.md) saying what it's for and why the standard library isn't
enough. GPL, AGPL and LGPL code is never allowed, linked or copied.

## Decisions

When you make a decision someone might later ask "why?" about, write a short ADR in
[`docs/decisions/`](decisions/) using the template in
[0001](decisions/0001-record-architecture-decisions.md).

## Commits

- Work on `main`: `git checkout main && git pull --rebase origin main`. No feature branches.
- **Small commits**, one logical change each: a feature slice with its tests, a fix, a
  refactor.
- **Every commit is green**: run `make test-all` before committing.
- Push after each commit: `git pull --rebase origin main`, then `git push origin main`.
  Never force-push or rewrite pushed history.
- Check the CI run afterwards. If it fails, fixing it is the next commit.
- **Message format:** a [Conventional Commit](https://www.conventionalcommits.org/) subject
  (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`) under 72 characters. The body
  explains **why**, lists any `SEC-xx` items the change implements or affects (required for
  anything touching auth, the protocol, the agent's privileged code or updates), and says
  how it was tested.
- Never use `--no-verify`. If a hook blocks a commit, fix what it found.

## License and sign-off

Sentinel is licensed under Apache-2.0. Contributions are accepted under the same license
(inbound = outbound); there is no CLA.

Sign off every commit with `git commit -s`. This adds a `Signed-off-by` line certifying the
[Developer Certificate of Origin](https://developercertificate.org/): that you wrote the
change or otherwise have the right to submit it under the project's license.

Every source file starts with a license header:

```go
// SPDX-License-Identifier: Apache-2.0
```

(`#` or `/* */` in languages that use those). `make check-headers` checks it.

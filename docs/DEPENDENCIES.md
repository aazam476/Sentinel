# Dependencies

Every third-party dependency, what it's for, and why the standard library (or something we
already have) isn't enough. Add a row **before** adding a dependency. Only permissive
licenses are allowed (MIT, BSD, Apache-2.0, ISC, MPL-2.0); `make check-licenses` fails on
anything GPL-family.

## Go modules

| Module | Version | License | What it's for | Why not the standard library |
|---|---|---|---|---|
| github.com/BurntSushi/toml | v1.6.0 | MIT | Reading `server.toml` | The standard library has no TOML parser; TOML is easier for people to edit than JSON |
| golang.org/x/crypto | v0.58.0 | BSD-3-Clause | ACME certificates (`acme/autocert`) and Argon2id password hashing | Maintained by the Go team but kept outside the standard library |
| golang.org/x/net, golang.org/x/text | (indirect) | BSD-3-Clause | Required by `golang.org/x/crypto/acme` | Pulled in by the module above |
| github.com/pquerna/otp | v1.5.0 | Apache-2.0 | TOTP secrets, codes, otpauth URLs and QR codes for authenticator apps | TOTP and QR encoding aren't in the standard library |
| github.com/boombuler/barcode | (indirect) | MIT | Draws the QR code for `pquerna/otp` | Pulled in by the module above |
| github.com/go-webauthn/webauthn | v0.18.2 | BSD-3-Clause | Passkeys: creating and checking WebAuthn credentials | WebAuthn's CBOR, COSE keys and attestation formats are large and easy to get subtly wrong |
| github.com/fxamacker/cbor/v2, github.com/x448/float16, github.com/go-webauthn/x, github.com/golang-jwt/jwt/v5, github.com/google/go-tpm, github.com/go-viper/mapstructure/v2, github.com/tinylib/msgp, github.com/philhofer/fwd | (indirect) | MIT, BSD-3-Clause, Apache-2.0 | Encodings and attestation formats used by `go-webauthn` | Pulled in by the module above |
| github.com/coder/websocket | v1.8.15 | ISC | The agents' WebSocket connection to the server (and, later, the browser's terminal and file streams) | The standard library has no WebSocket implementation; this one is small, has no dependencies and takes a `context.Context` everywhere |
| github.com/shirou/gopsutil/v4 | v4.26.10 | BSD-3-Clause | The agent's metrics and machine details: CPU, memory, filesystems, network and disk counters, temperatures, uptime, OS | Reading these takes different system calls and files on Linux, macOS and Windows; gopsutil does all three in pure Go |
| github.com/tklauser/go-sysconf, github.com/tklauser/numcpus, github.com/ebitengine/purego, github.com/yusufpapurcu/wmi, github.com/go-ole/go-ole, github.com/lufia/plan9stats, github.com/power-devops/perfstat | (indirect) | BSD-3-Clause, Apache-2.0, MIT | Platform back ends for gopsutil (sysconf, macOS without cgo, Windows WMI) | Pulled in by gopsutil |
| golang.org/x/sys | v0.49.0 | BSD-3-Clause | Setting the agent state directory's access list on Windows (SEC-20) | The standard library has no API for Windows access lists |
| modernc.org/sqlite | v1.61.1 | BSD-3-Clause | The SQLite database, in pure Go | No embedded database in the standard library; pure Go keeps the server buildable with `CGO_ENABLED=0` |
| ↳ reviewed 2026-11-07 | | | `LICENSE-3RD-PARTY.md` in the module lists every linked component as BSD-3-Clause, MIT or public domain (SQLite itself). GPL text appears there only for tools used by its own test suite, which are never compiled into Sentinel | |
| modernc.org/libc, mathutil, memory; github.com/dustin/go-humanize, google/uuid, mattn/go-isatty, ncruces/go-strftime, remyoudompheng/bigfft | (indirect) | BSD-3-Clause, MIT | The C-to-Go runtime that modernc.org/sqlite is built on | Pulled in by modernc.org/sqlite |

## Go tools (never compiled into Sentinel)

| Module | Version | License | What it's for | Why not without it |
|---|---|---|---|---|
| github.com/gzuidhof/tygo | v0.2.21 | MIT | Generates `web/src/api/types.gen.ts` from the Go API structs (`make gen-types`) | Hand-written TypeScript copies of the API types would drift from the server |

## Web UI: shipped to the browser

| Package | Version | License | What it's for | Why not without it |
|---|---|---|---|---|
| react | 19.3.1 | MIT | Component model for the web UI | The chosen UI framework; hand-written DOM code wouldn't stay readable |
| react-dom | 19.3.1 | MIT | Renders React components into the page | Required by React in the browser |
| react-router | 8.5.1 | MIT | Pages and URLs in the web UI | The browser has no router; hand-written history handling is easy to get wrong |
| @tanstack/react-query | 5.107.1 | MIT | Fetching, caching and refreshing server data; loading and error states | Hand-written fetch-and-cache code in every component would be inconsistent and buggy |
| @base-ui/react | 1.9.0 | MIT | Accessible dialogs and other primitives, unstyled | Focus trapping, keyboard handling and ARIA for dialogs are hard to get right by hand |
| sonner | 2.0.8 | MIT | Toasts for action results ("Passkey added") | Stacking, swiping and timing toasts accessibly is a library's worth of work |
| clsx | 2.1.1 | MIT | Joining conditional CSS Module class names | Tiny and clearer than string concatenation |
| react-virtuoso | 4.18.20 | MIT | Rendering only the visible rows of long lists (the audit log) | Rendering thousands of rows at once makes the page slow |

## Web UI: build and test tools (never shipped)

| Package | Version | License | What it's for | Why not without it |
|---|---|---|---|---|
| vite | 8.3.6 | MIT | Dev server with hot reload and the production build | Bundling TypeScript, CSS Modules and assets by hand isn't practical |
| @vitejs/plugin-react | 6.2.1 | MIT | JSX and fast refresh in Vite | Vite doesn't compile React components on its own |
| typescript | 6.0.3 | Apache-2.0 | Type checking (`strict: true`) | Types catch whole classes of bugs before runtime. Held at 6.0 because the lint tooling doesn't support 7 yet (ADR 0006) |
| @types/react, @types/react-dom | 19.3.0 | MIT | Type definitions for React | React ships no types of its own |
| @types/node | 22.x | MIT | Type definitions for Node APIs used in config files | Config files run in Node |
| vitest | 5.1.0 | MIT | Unit and component test runner | Shares Vite's config and transforms, so tests compile like the app |
| @vitest/coverage-v8 | 5.1.0 | MIT | Coverage reports and the 80% threshold | Vitest needs a coverage provider |
| jsdom | 31.0.0 | MIT | A DOM for component tests in Node | Component tests need a document without a browser |
| @testing-library/react | 16.3.3 | MIT | Renders components and finds elements by role and label | Makes tests use the page the way people do, not CSS classes |
| @testing-library/dom | 10.4.2 | MIT | Queries used by @testing-library/react | Required peer of @testing-library/react |
| @testing-library/jest-dom | 7.0.1 | MIT | Readable assertions such as `toBeVisible()` | Plain assertions on DOM nodes are hard to read |
| axe-core | 4.14.0 | MPL-2.0 | Accessibility checks in component tests | No standard-library way to audit accessibility |
| @axe-core/playwright | 4.14.0 | MPL-2.0 | Accessibility checks in end-to-end tests | Runs axe inside the real browser, including color contrast |
| @playwright/test | 1.63.0 | Apache-2.0 | End-to-end tests in a real browser | Drives a real browser against the real server |
| eslint | 10.14.0 | MIT | Linting | Catches bugs and enforces the house rules |
| @eslint/js | 10.0.1 | MIT | ESLint's recommended rules | Part of ESLint's flat config |
| typescript-eslint | 8.72.1 | MIT | Type-aware lint rules (for example, no `any`) | ESLint doesn't understand TypeScript on its own |
| eslint-plugin-react-hooks | 7.1.1 | MIT | Rules of Hooks checks | Hook mistakes are easy to make and hard to spot |
| eslint-plugin-react-refresh | 0.5.7 | MIT | Keeps components compatible with hot reload | Prevents confusing hot-reload failures |
| globals | 17.14.0 | MIT | Lists of browser and Node global names for ESLint | Needed by ESLint's flat config |
| prettier | 3.9.10 | MIT | Code formatting | Ends formatting debates; CI checks it |
| msw | 2.15.0 | MIT | Answers the UI's API calls in component tests, typed from the generated API types | Mocking `fetch` by hand per test drifts from the real API |
| @testing-library/user-event | 14.6.8 | MIT | Typing and clicking in component tests the way a person does | `fireEvent` skips the events real input produces |

## Bundled data

| Data | License | What it's for |
|---|---|---|
| Common password list from [SecLists](https://github.com/danielmiessler/SecLists) | MIT | Rejecting commonly used passwords (`internal/server/auth/commonpasswords/`) |

`web/package.json` also forces `playwright-core` to 1.63.0 for every package, so
`@axe-core/playwright` uses the same Playwright as the tests (ADR 0006).

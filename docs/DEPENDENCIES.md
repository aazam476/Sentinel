# Dependencies

Every third-party dependency, what it's for, and why the standard library (or something we
already have) isn't enough. Add a row **before** adding a dependency. Only permissive
licenses are allowed (MIT, BSD, Apache-2.0, ISC, MPL-2.0); `make check-licenses` fails on
anything GPL-family.

## Go modules

The server and agent use only the Go standard library so far.

## Web UI: shipped to the browser

| Package | Version | License | What it's for | Why not without it |
|---|---|---|---|---|
| react | 19.3.1 | MIT | Component model for the web UI | The chosen UI framework; hand-written DOM code wouldn't stay readable |
| react-dom | 19.3.1 | MIT | Renders React components into the page | Required by React in the browser |

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

`web/package.json` also forces `playwright-core` to 1.63.0 for every package, so
`@axe-core/playwright` uses the same Playwright as the tests (ADR 0006).

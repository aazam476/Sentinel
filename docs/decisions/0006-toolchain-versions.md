# 0006 — Toolchain versions

**Date:** 2026-11-07 · **Status:** Accepted

## Context

We want the latest stable tools, but a few of them have to agree with each other.

## Decision

- **Go:** `go.mod` declares `go 1.27` and `toolchain go1.27.3`. Any Go 1.21 or later
  downloads that exact toolchain automatically, so everyone builds with the same compiler.
  CI reads the version from `go.mod`.
- **golangci-lint:** v2.15.0, installed with `go install` so it is built with the same Go
  version it lints. Older builds refuse to lint code that targets a newer Go.
- **TypeScript:** held at 6.0. TypeScript 7 is out, but `typescript-eslint` (needed for
  type-aware lint rules such as "no `any`") supports only versions below 6.1. Upgrade once it
  supports 7.
- **Playwright:** `@playwright/test` and `@axe-core/playwright` must use the same
  `playwright-core`, or their `Page` types don't match. `web/package.json` forces
  `playwright-core` to the version `@playwright/test` uses through an npm `overrides` entry.
  Upgrade both together.
- **Node.js:** CI uses the current LTS (24); anything from 22 works locally.

## Consequences

- Upgrading TypeScript or Playwright takes a deliberate step, recorded in
  `docs/DEPENDENCIES.md`.

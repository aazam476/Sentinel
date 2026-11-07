# 0001 — Record architecture decisions

**Date:** 2026-11-07 · **Status:** Accepted

## Context

Sentinel is meant to be readable by a student contributor within a day. Code shows *what* the
system does, but not *why* it was built that way, and the reasons behind early choices are
the first thing to be forgotten.

## Decision

We record every decision that someone might later ask "why?" about as a short Architecture
Decision Record in `docs/decisions/NNNN-title.md`, numbered in order, using this template:

```markdown
# NNNN — Title

**Date:** YYYY-MM-DD · **Status:** Accepted

## Context
What problem were we solving?

## Decision
What did we choose?

## Consequences
What gets easier? What gets harder?
```

ADRs are never deleted. If a decision changes, a new ADR replaces it, and the old one's
status becomes "Superseded by NNNN".

## Consequences

- New contributors can read the reasoning instead of guessing or asking.
- Code comments can link to an ADR instead of repeating a long explanation.
- Writing an ADR takes a few minutes per decision.

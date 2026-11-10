# 0035 — Remote desktop: the clipboard and several monitors

**Date:** 2026-11-10 · **Status:** Accepted

## Context

§9.3 lists clipboard sync (text) and multi-monitor switching among remote desktop's beta
features (M8-6).

## Decision

- **The clipboard travels as control messages** (`clipboard`, with `text`), either way, at
  most 64 KiB of UTF-8 (`protocol.MaxClipboardBytes`); bigger copies stay where they are. A
  view-only session shares no clipboard in either direction.
- **Pasting is Ctrl+V, as on the machine.** The browser lets its own paste happen instead of
  sending the key, sends the pasted text, then V, so the machine pastes what was just on
  this computer's clipboard. The browser's paste event needs no permission prompt. If no
  paste comes (nothing to paste), V is sent when the key is let go.
- **What the machine copies goes to the browser's clipboard** with `writeText`; where the
  browser refuses, the page says so.
- **On X11, the agent owns the CLIPBOARD selection** to offer pasted text (answering
  TARGETS, UTF8_STRING and STRING), and asks XFIXES to say when another app takes it, then
  reads its text as UTF-8. X has no clipboard store, so this is the only way in either
  direction. xgb's extension `Init` functions write shared maps without a lock, so the agent
  serializes them.
- **Monitors are offered when there are two or more** (RandR 1.5 monitors on X11). The whole
  desktop is shown to start with; picking one makes the session capture only that rectangle
  (X11 reads it directly; other sources are cropped) and move pointer input by its corner.
  A monitor that disappears falls back to the whole desktop.
- **The test pattern plays along**: it echoes each paste as a copy of its own and reports
  its halves as two monitors, so the end-to-end test covers both features through a real
  browser and agent.

## Consequences

- Files, images and rich text don't travel (§9.3 "later").
- Tests check, on a real Xvfb, that text another app copies is seen and that text the agent
  offers is pasted elsewhere, the TARGETS answer, RandR monitors and region capture; the
  Playwright test pastes, reads the copy back from the browser's clipboard, and clicks on
  one monitor.

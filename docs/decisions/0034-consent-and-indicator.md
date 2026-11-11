# 0034 — The consent prompt and the session indicator

**Date:** 2026-11-10 · **Status:** Accepted; Windows and macOS ask and show sessions through the tray (ADR 0038)

## Context

SEC-23 asks for a local policy setting, `desktop_requires_consent`, under which the logged-in
user accepts or denies each remote desktop session; SEC-24 for `show_session_indicator`, a
visible notice while any remote session, terminal or desktop, is open.

## Decision

- **Both are top-level keys of the policy file**, under the names SEC-23 and SEC-24 give
  them, and off by default. The server can't change them, like every policy setting.
- **On Linux X11, the agent draws its own windows** on the active session's display, the
  same connection remote desktop uses (ADR 0032): an override-redirect window the window
  manager can't hide or move, painted from an image. Text is drawn with `golang.org/x/image`'s
  bitmap font, because X's core fonts are often not installed and a toolkit would need cgo.
- **The prompt names who asks** (the server sends the user's name with the session request)
  and whether they want to see or also control the screen. Allow starts the session; Deny,
  no answer within 25 seconds (within the server's 30-second wait), or no one to ask refuses
  it with `policy_denied`, which the audit log records as denied.
- **Fail closed.** Where the agent can't ask (Windows, macOS and Wayland, until their
  desktop support lands), a policy that wants consent refuses every session.
- **The indicator follows every open shell and desktop session.** The terminal manager
  reports how many shells run, and desktop sessions report starting and ending; the bar is
  redrawn with who is on the screen and how many terminals are open, and taken away when
  nothing is. A shell waiting to be resumed still counts: it is still running. With no
  desktop to show it on (a server with no one logged in), the agent logs that instead.

## Consequences

- Tests click Allow and Deny on a real Xvfb through XTEST, check the windows are drawn and
  taken away, and check the agent refuses on a denial, a timeout and no one to ask.
- Windows (WTSSendMessage), macOS and Wayland prompts come with those platforms.

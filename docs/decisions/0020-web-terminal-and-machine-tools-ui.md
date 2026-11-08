# 0020 — The web terminal and machine tools in the UI

**Date:** 2026-11-08 · **Status:** Accepted

## Context

M3 puts a terminal, services, processes, logs and power actions on the machine page. The
terminal uses xterm.js, which (like Sonner, ADR 0013) styles itself with inline `<style>`
elements and `style` attributes that the Content-Security-Policy blocks. A WebSocket can't
tell a page why its upgrade was refused, so a missing second factor or an offline machine
would look like a broken connection. And a phone's connection drops often.

## Decision

- **xterm.js is patched at build time, like Sonner.** A Vite plugin points xterm's three
  `<style>` elements at `<sentinel-style>`, a custom element that applies the CSS through a
  constructed stylesheet (`adoptedStyleSheets`, which the policy allows), and turns its one
  `setAttribute("style", …)` into a CSSOM `style.cssText` change. The build fails if an
  xterm.js upgrade changes the patched code. The end-to-end test opens a real shell with the
  policy on and fails on any violation.
- **A plain GET to the terminal's WebSocket URL runs the same checks** and answers 204 or
  their error, without opening a shell or writing to the audit log. The UI calls it first, so
  it can ask for the second factor (SEC-07) or say the machine is offline before connecting.
- **The browser reconnects to the same shell for 30 seconds** after a drop, checking first
  each time: a shell that has ended stops it at once, an offline machine is waited for.
  Leaving the tab detaches rather than ends the shell, so coming back resumes it; "End
  session" ends it.
- **The machine page has tabs** (Overview, Terminal, Services, Processes, Logs), each its own
  route and code chunk, so xterm.js loads only for people who open a terminal. A tab is hidden
  when the user's role can't use it or the machine's local policy switches it off (SEC-22),
  rather than shown and failing.
- **Phones get a row of the keys their keyboards lack** (Esc, Tab, Ctrl-C, arrows).
- **Power actions follow the machine.** After a reboot the page says it is waiting for the
  machine and, when the event stream reports it back online, says so. The status comes from
  the event stream, so a reboot that doesn't happen leaves the message until the page is left.
- **Processes refresh every 5 seconds**, pausing while a menu or dialog is open so rows don't
  move under the user's finger. Lists of services, processes and log entries are virtualized.
- **State badges keep their text at full contrast** and show the state's color as a dot, so
  they pass WCAG AA in both themes and color is never the only cue.

## Consequences

- Upgrading xterm.js may need the plugin updated; the build says so.
- The terminal's colors are fixed (a dark theme) rather than following the page's theme.

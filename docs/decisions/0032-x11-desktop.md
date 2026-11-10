# 0032 — Remote desktop on Linux X11: the agent connects to the display itself

**Date:** 2026-11-10 · **Status:** Accepted

## Context

§9.3 lists XShm + XDamage for capture and XTest for input on X11, and a desktop helper
process for the platforms where the agent's service can't reach the user's screen. The agent
must stay pure Go (`CGO_ENABLED=0`, reproducible builds), which rules out Xlib.

## Decision

- **The agent connects to the X server itself**, with `github.com/jezek/xgb` (BSD-3-Clause,
  pure Go). On Linux the agent runs as root, which can open the display's socket and read the
  session's cookie, so X11 needs no helper process. Windows (session 0 isolation) and macOS
  (per-user permissions) will.
- **Finding the session.** `loginctl list-sessions` and `show-session` give the active local
  session of class `user` or `greeter` with `Type=x11`, and its display. Its cookie is read
  from where display managers keep it: GDM (`/run/user/UID/gdm/Xauthority`), SDDM
  (`/run/user/UID/xauth_*`), LightDM (`/run/lightdm/USER/xauthority`), then `~/.Xauthority`.
  The connection is made with that cookie passed directly, never through `$XAUTHORITY`. With
  no cookie, it connects without one, for servers started without access control. The local
  policy's `[desktop] display` and `xauthority` skip the search, for machines without
  logind.
- **Capture is `GetImage`** of the whole root window, converted from X's 32-bit BGRX. It
  costs a copy of the screen per frame over the Unix socket, which is fine at 15 frames a
  second on today's screens. XShm (shared memory) and XDamage (only what changed) are later
  optimisations; the tile encoder already sends only changed tiles.
- **Keys are physical.** The browser sends `KeyboardEvent.code`, which names a key by its
  place on the keyboard. It maps to the Linux input code of the same key, plus 8, which is
  the X keycode under the evdev driver every current X server uses. So the machine's own
  layout decides what a key types, as it does for someone sitting there.
- **Availability is checked every 15 seconds** and sent to the server as a
  `desktop.availability` event when it changes, so the Desktop tab appears after someone
  logs in and goes when they log out, without reconnecting. The reason shown is one of a few
  fixed sentences, never a command's output, and fits the server's 128-byte limit.
- **Wayland** says it isn't supported yet (M8-5, through the portal).

## Consequences

- The X11 package is tested against a real Xvfb in CI: capture, the pointer, buttons, the
  wheel, keys and the cookie. An end-to-end test drives an Xvfb desktop from the browser,
  directly and through the server.
- A machine at its login screen (the greeter's session) can be shared, which an unattended
  admin needs.

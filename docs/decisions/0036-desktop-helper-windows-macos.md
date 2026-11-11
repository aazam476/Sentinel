# 0036 — The desktop helper, and remote desktop on Windows and macOS

**Date:** 2026-11-10 · **Status:** Accepted

## Context

§9.3 asks for remote desktop on Windows (DXGI Desktop Duplication, SendInput, a helper started
into the user's session with the SYSTEM token so it follows the secure desktop, and
Ctrl+Alt+Del) and macOS (ScreenCaptureKit, CGEvent, permission detection). On both, the
agent's service can't see the screen: Windows services run in session 0, and a macOS launch
daemon is outside the user's graphical session. The agent is pure Go (`CGO_ENABLED=0`, ADR
0027).

## Decision

- **One desktop helper for both.** `sentinel-agent desktop-helper` serves the platform's
  screen to the agent over the child's standard input and output (never a network port,
  SEC-26). The messages are length-prefixed frames carrying one request at a time: name,
  size, a capture of a region (raw RGBA), input, the clipboard (with changes sent as they
  happen) and the monitors. The agent's side is a `desktop.Source` like any other, so the
  session, the clipboard and the monitor picker work the same through it. The protocol is
  platform-neutral and tested on Linux, with the test binary as the helper.
- **Windows.** The agent duplicates its own SYSTEM token, moves it into the active console
  session (`WTSGetActiveConsoleSessionId`; the login screen counts) and starts the helper with
  it. Go can't choose the new process's desktop, so the helper keeps its work on one OS
  thread and attaches it to the input desktop before each capture and each input event,
  which also follows UAC prompts and the login screen. Capture is GDI `BitBlt` from the
  virtual screen (every monitor), input `SendInput` with scancodes, Ctrl+Alt+Del `SendSAS`,
  the clipboard `CF_UNICODETEXT` watched through its sequence number, monitors
  `EnumDisplayMonitors`. All through `golang.org/x/sys/windows`. **Proposed spec change:**
  GDI instead of DXGI Desktop Duplication, which needs COM from Go; GDI is slower on large
  screens but captures the same desktops, and DXGI can replace it behind the same source.
- **macOS.** The agent finds whoever is logged in (the owner of `/dev/console`) and starts the
  helper in their session with `launchctl asuser`. Capture is the system's `screencapture`
  tool; input is CGEvent through `purego` (C calls without cgo); the clipboard is `pbcopy` and
  `pbpaste`. The agent checks `CGPreflightScreenCaptureAccess` and `AXIsProcessTrusted`, and
  until the person at the Mac allows both, the Desktop tab's reason says permission is
  needed. **Proposed spec change:** `screencapture` instead of ScreenCaptureKit, which is an
  Objective-C API; it gives a few frames a second rather than smooth video. One display, the
  main one, for now.
- **Wayland** stays unsupported: its portal (`xdg-desktop-portal` ScreenCast) delivers the
  screen through PipeWire, which has no pure-Go client. The machine page says so and points
  to an X11 session. **Proposed spec change:** Wayland moves after the beta.
- **The consent prompt and session indicator (SEC-23, SEC-24)** are X11-only for now; on
  Windows and macOS a policy that wants consent refuses every session (it fails closed).

## Consequences

- The Windows and macOS code is compiled and linted for both platforms in this repository,
  but can only be exercised on real machines; the helper protocol around it is tested.
- A slow macOS picture and GDI's cost on 4K screens are known; both are behind the same
  source interface as their faster replacements.

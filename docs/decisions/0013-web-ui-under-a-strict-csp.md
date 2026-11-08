# 0013 — Web UI libraries under a strict Content-Security-Policy

**Date:** 2026-11-08 · **Status:** Accepted

## Context

The server sends `style-src 'self'` and `script-src 'self'` (SEC-11), so the browser refuses
inline `<style>` and `<script>` elements. Several UI libraries add inline `<style>` elements at
run time: Sonner (its whole stylesheet), Base UI (some components), and input-otp (caret and
selection styles). Allowing `'unsafe-inline'` for styles would weaken the policy for every
page.

## Decision

- **Base UI** is wrapped in its `CSPProvider` with `disableStyleElements`; our CSS Modules
  style everything it renders.
- **Sonner**'s stylesheet is imported as a normal CSS file (`sonner/dist/styles.css`), and a
  small Vite plugin removes the call that would inject the same styles inline. The plugin only
  runs in production builds (the dev server doesn't send the CSP) and fails the build if a
  Sonner upgrade changes the code it removes, so the problem can't come back silently.
- **No input-otp.** The 6-digit code field is a plain input with `inputmode="numeric"` and
  `autocomplete="one-time-code"`, which phones and password managers already understand.
- **Sonner's rich colors** are darkened for the light theme in `styles/toasts.css`, because
  the defaults fall below WCAG AA contrast.
- The end-to-end tests fail on any CSP violation in the browser console.

## Consequences

- The policy stays strict: no inline styles or scripts on any page.
- Upgrading Sonner may need the plugin updated; the build says so.
- Any new UI library must be checked for inline `<style>` or `<script>` injection before it is
  added.

# 0047 — A stored secret stays with the server it was given for

**Date:** 2026-11-15 · **Status:** Accepted · **Supersedes:** "a change that leaves them empty keeps the stored ones" in ADR 0025

## Context

A notification channel's SMTP password and ntfy token are write-only: sealed with the secret
key (SEC-42) and never sent back to the browser. So that a channel can be edited without
typing them again, ADR 0025 has a change that leaves a secret empty keep the stored one.

It kept it whatever else the change did. Someone who can edit channels could change only the
mail server, or the ntfy server, to one of their own, leave the secret empty, and press
**Send test**: the server signed in there with the stored password, or sent the stored token
in a header. The secret nobody could read was handed to whoever asked, with one edit.

## Decision

- **A stored password is kept only for the same mail server and port, and a stored token only
  for the same ntfy server**, compared after the usual defaults are filled in (the port that
  goes with the security chosen, `https://ntfy.sh`). Host names are compared without regard to
  case; the ntfy address must match exactly, so going from `https` to `http` counts as another
  server.
- **Otherwise an empty secret means none.** The channel is saved without the old one, and the
  change has to give the new server's. It isn't refused: a new mail relay may need no password,
  and a new ntfy server no token.
- **The dialog says which it will be.** Under the password or token it reads "Leave empty to
  keep the one stored" until the server is changed, then "The one stored was for the server
  before. Enter this server's, or leave it empty for none."
- A webhook's address is its own secret and its own destination, so it is kept when left
  empty, as before. Registry logins can't be edited, and are sealed to their registry's name.

## Consequences

- Moving a channel from port 587 with STARTTLS to port 465 with TLS on the same mail server
  needs the password typed again, since the port changed. That is the price of a rule simple
  enough to state.
- Changing only the username, the recipients, the topic or the name keeps the secret.

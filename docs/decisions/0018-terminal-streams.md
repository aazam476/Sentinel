# 0018 — Streams and the web terminal

**Date:** 2026-11-08 · **Status:** Accepted

## Context

The web terminal (§9.2) is the first feature that carries a byte stream rather than
request–response messages: keystrokes go from the browser through the server to the agent,
and the shell's output comes back. Several terminals can run over one agent connection at
once, the browser's connection or the agent's can drop and come back, and a shell should
survive a short drop instead of losing the user's work.

## Decision

- **The server picks stream IDs.** The spec leaves it open which side numbers a stream. The
  server opens every stream (`terminal.open` carries the ID), so it numbers them per agent
  connection, counting up from 1. Neither side can then pick an ID the other already uses,
  and the agent never has to allocate anything.
- **Data travels as binary frames** (`[4-byte stream ID][bytes]`) on the agent connection,
  and control as messages: `terminal.open` and `terminal.resize` are ordinary requests, and
  either side ends a stream with `stream.close`.
- **A closed stream doesn't always end the shell.** `stream.close` carries `keep_session`:
  the server sets it when the browser went away without the user closing the terminal. The
  agent then keeps the shell for 30 seconds (`terminal.ResumeWindow`) and keeps recording its
  output. When the agent's own connection drops, it keeps every shell the same way.
- **Resuming replays recent output.** Each shell keeps its last 64 KB of output; a resumed
  terminal gets it first, so the browser can redraw the screen.
- **Only the user who opened a terminal can resume it**, and only within the window plus 5
  seconds of slack, since the agent's clock and the server's don't run in step. The server keeps this record in memory
  (`relay.Sessions`); a server restart ends every resumable session, which is the safe
  outcome.
- **Opening a terminal is checked before the WebSocket upgrade**: the `use_terminal`
  permission, a second factor within the last 15 minutes (SEC-07), the browser's Origin
  (SEC-13), that the machine is online and advertises the capability, and resume ownership.
  The agent checks its local policy again (SEC-22). Every open, refusal and close is audited.
- **At most 32 shells per machine**, so a runaway browser can't exhaust the agent.
- **A slow reader loses its stream.** A browser that can't keep up (1024 frames behind on the
  server, or 10 seconds to accept one frame on the agent) is treated as gone, which detaches
  the shell rather than blocking it or the agent's connection.

## Consequences

- The spec should say that the server allocates stream IDs.
- Resizes are requests, so a resize can arrive after output drawn at the old size. Terminals
  redraw on resize, so this is harmless.
- The resume record lives only in the server's memory; running several server instances
  behind one address would need it shared. Sentinel runs one server, so this is fine for now.

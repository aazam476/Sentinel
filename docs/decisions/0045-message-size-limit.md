# 0045 — Nothing too large to deliver is sent to or from an agent

**Date:** 2026-11-14 · **Status:** Accepted · **Supersedes:** the 1 MiB message limit in ADR 0014

## Context

ADR 0014 capped a control message at 1 MiB, since bulk data travels in chunks and stream
frames. Each side sets the cap as its WebSocket read limit, and a side that is sent more
closes the connection.

Nothing stopped either side from sending more, and ordinary answers did:

- **A text file for the editor.** §9.7 lets the browser edit files under 1 MB, and the file
  travels as one JSON string. JSON writes a quote or a newline as two bytes, and `<`, `>`, `&`,
  a control character or a byte that isn't UTF-8 as six. A 1 MB log file measured 1,061,828
  bytes as a message; 300 KB of HTML came to 820 KB, so a larger page was over the cap.
- **A folder listing.** 5,000 entries with 64-character names measured 1,350,203 bytes.
- **Logs.** The Logs page asks for 1,000 entries, which is over the cap once they average
  1 KB each, as lines from containers often do.

The result was worse than a failed request. The receiver closed the machine's connection, so
every terminal, desktop session and transfer on that machine ended, the machine showed as
offline until it connected again, and the request failed with "The machine went offline before
it answered". Opening the same file again did the same again.

The read limit also applied before the handshake: anyone who could reach `/agent/connect`
could have the server read 1 MiB per message without proving anything.

## Decision

- **Neither side sends what the other would refuse.** `protocol.Encode` and
  `protocol.WriteFrame` return `ErrTooLarge` for a message or frame over `MaxMessageSize`.
  Nothing is written, so the connection and everything else on it carry on.
- **An agent whose answer is too large says so**: it answers with the error `too_large`
  instead. The API reports that as 413 with a hint to ask for less. A request the server can't
  send for the same reason is a 413 too.
- **The limit is 8 MiB**, set by the largest thing one message has to carry: the editor's
  1 MiB of text at six bytes each, and room for the rest of the message. Lowering the editor's
  limit to fit the old cap would have broken §9.7.
- **A listing stops at 4 MiB of entries**, as it already stops at 5,000 entries, and says it
  left some out. Names can be long and paths longer, so the count alone doesn't bound it.
- **Before an agent has proven who it is, the server reads at most 64 KiB per message.** The
  messages of enrollment and the handshake are a few hundred bytes. The limit rises to
  `MaxMessageSize` once the handshake succeeds.

## Consequences

- The server may hold up to 8 MiB for a message from each connected agent, not 1 MiB. Only
  enrolled agents get that; everyone else gets 64 KiB.
- The message formats are unchanged, so the protocol version is too. An agent or a server
  from before this change still closes the connection over a message above 1 MiB, as it always
  did; the fix is complete on a machine once both sides have it.
- Answers without a size budget of their own (logs, container lists) fail with `too_large`
  when they are over 8 MiB, rather than being cut to fit. The person can ask for less: a
  shorter time, one service, fewer lines.

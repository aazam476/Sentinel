# 0031 — Remote desktop: JPEG tiles over a WebRTC data channel, with a WebSocket fallback

**Date:** 2026-11-09 · **Status:** Accepted

## Context

§9.3 asks for remote desktop on WebRTC with Pion: signaling through the server, a direct
connection where possible, the server's TURN relay where not, and the existing WebSockets as
the last resort when UDP can't get through at all. It speaks of "video". The agent is pure Go
(`CGO_ENABLED=0`, ADR 0027's reproducible builds), and there is no pure-Go H.264 or VP8
encoder; the platform encoders (Media Foundation, VideoToolbox, VA-API) are §9.3's "later:
hardware encoders".

## Decision

- **The screen is sent as JPEG tiles, not a video track.** The agent cuts each captured frame
  into 64-pixel tiles, encodes only the tiles that changed (with `image/jpeg`), and sends
  them followed by an end-of-frame marker, so the browser draws a frame all at once. The
  encoding is in `internal/protocol` (`EncodeTile` and friends, fuzzed): kind 1 is the screen
  size, kind 2 a tile (`u16` x, y, width, height, then the JPEG), kind 3 the end of a frame.
  Quality is the user's slider, 10 to 95. A desktop mostly stands still, so this sends little;
  full-screen video plays worse than with a real video codec. Hardware video encoders can come
  later as a second kind of update without changing the signaling.
- **Two data channels.** The browser opens `frames` (agent to browser) and `input` (browser to
  agent) and makes the offer; the agent answers. Frames skip while more than 4 MiB wait in the
  channel, so a slow link shows a recent screen instead of a growing backlog.
- **The fallback carries the same updates over the stream.** A stream's data starts with a byte
  saying what follows: `c` for a JSON control message (offer, answer, candidates, input,
  quality, keyframe, ack, fallback, error) and `f` for a screen update. The server relays `c`
  as WebSocket text and `f` as binary, and never looks inside. Over the fallback, the agent
  sends the next frame only after the browser's `ack`, so the WebSocket (and the agent's one
  connection to the server, which every other feature shares) is never flooded.
- **When to fall back.** The browser falls back if WebRTC can't start, the agent can't answer,
  the data channel doesn't open within 8 seconds, or the user ticks "Relay through the server".
- **mDNS.** Browsers hide local addresses behind `<uuid>.local` names. The agent resolves them
  but announces its own addresses plainly (query-only mode): a web page learning the agent's
  LAN address reveals nothing the admin doesn't know. Where multicast doesn't work, ICE can
  still succeed from the browser's checks; otherwise the session falls back. The end-to-end
  test turns Chromium's hiding off, because four sessions resolving names at once on one CI
  machine sometimes time out, and the test is about the direct path, not multicast.
- **A test pattern source**, turned on by the local policy (`[desktop] test_pattern = true`),
  stands in for a real screen in tests and for checking a deployment end to end. It draws
  colour bars, a moving square, the pointer and a mark where each click landed.
- **The tab is hidden** while the agent reports no desktop to share (§9.3); opening its URL
  directly says why.

## Consequences

- Moving pictures cost more bandwidth than with a video codec; this is the price of a pure-Go
  agent until hardware encoders arrive.
- The server's TURN relay (§9.3 step 4) is still to come (M8-6); until then a session that
  can't connect directly goes through the WebSocket.
- Every session is audited (`desktop.open`, `desktop.close` with its duration and bytes).

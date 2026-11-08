# 0014 — Agent protocol details

**Date:** 2026-11-08 · **Status:** Accepted

## Context

The spec describes the agent connection: a JSON envelope, binary stream frames, and a
handshake in which both sides sign each other's nonces. Some details that decide whether the
handshake is actually safe were left open: exactly which bytes are signed, how IDs look, and
how big a message may be.

## Decision

- **One package, `internal/protocol`,** defines every message, and both binaries import it.
  The JSON in `testdata/protocol` is a golden example of each message type; tests decode and
  re-encode every file, so a renamed field fails a test instead of breaking agents in the field.
- **Signed bytes are length-prefixed and domain-separated.** Each side signs
  `domain, role, first nonce, second nonce, agent ID`, where every part is preceded by its
  length as a varint. The domain (`sentinel-handshake-v1`) stops a handshake signature from
  being valid anywhere else; the role (`server` or `agent`) stops the server's signature from
  being replayed as the agent's; the length prefixes stop two different field splits from
  producing the same bytes. Plain concatenation, as sketched in the spec, has none of these
  guarantees.
- **Nonces are 32 random bytes** from `crypto/rand`. Signatures and keys are checked for their
  exact Ed25519 sizes before verifying.
- **IDs are ULIDs**: 26 characters, sortable by creation time, safe in URLs and file names.
- **Messages are capped at 1 MiB.** Anything bigger (file contents, terminal output) goes
  through stream frames, which carry a 4-byte big-endian stream ID and are relayed unread.
- **Fingerprints** are `SHA256:` followed by the unpadded base64url SHA-256 of the server's
  public key, in the style of SSH, so they fit on one line of an install command.
- **Versions.** The agent sends its protocol version in `enroll` and `hello`. The server
  accepts the current version and the one before it, so a fleet keeps working while agents
  update. Both are 1 today.

## Consequences

- Changing the signed bytes is a protocol version bump: old agents would fail the handshake.
- A protocol change shows up as a diff in `testdata/protocol`, which makes it easy to review.

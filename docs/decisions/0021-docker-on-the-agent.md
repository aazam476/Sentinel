# 0021 — Docker on the agent

**Date:** 2026-11-08 · **Status:** Accepted

## Context

M4 manages containers through the Docker Engine API (§9.4): Docker Engine, Podman through its
Docker-compatible socket, and Docker Desktop, OrbStack or Colima on macOS and Windows. The
agent runs as root, so access to the engine is as powerful as root; the local policy must be
able to switch it off (SEC-22), and nothing the server sends may make the agent reach for
another socket.

## Decision

- **The official SDK, at its current home** (`github.com/moby/moby/client`, API 1.56, version
  negotiated with older engines), as the spec says. Compose stacks use the `docker compose`
  CLI (M4-B).
- **The socket is set in the local policy file** (`[docker] socket = "..."`), not by the
  server. Without it, the agent tries Docker's socket, then Podman's, and reports
  `docker_unavailable` ("Docker not detected") if neither answers. It reconnects by itself
  after the engine restarts.
- **Every Docker action is refused when the policy switches Docker off**, listing included.
- **Names from the server are checked** before reaching the SDK: container, volume and
  network names against Docker's own pattern, image references with the reference parser
  Docker uses. Built-in networks are refused with a clear message.
- **Logs keep stdout and stderr in the order they were written.** The SDK's demultiplexer
  writes the two streams to separate places, losing the order; the agent reads the frames
  itself (fuzzed), caps line length and frame size, and makes text valid UTF-8.
- **CPU and memory are measured like `docker stats`**: CPU between the engine's two samples
  (100% is one core), memory without the page cache. Up to eight containers are measured at
  once, so a list of running containers takes about a second.
- **Registry logins travel with the one request that needs them** (pull, update check) and are
  never written to disk on the agent; the server stores them encrypted (M4-D).
- **Tests**: unit tests run the real SDK against a fake engine answering with responses
  captured from Docker 29.9; an integration test (`-tags integration`) drives a real container
  where Docker is installed, as on CI's Linux runners.

## Consequences

- A conflict (removing a running container, a volume in use) is its own error code,
  `conflict`, shown with Docker's message.
- Rootless Docker or Podman under a user account needs its socket set in the policy file.

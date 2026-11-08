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
- **Stacks** live one folder each in `/opt/sentinel/stacks/<name>/` (`compose.yaml` and
  `.env`; the folder is set with `[docker] stacks_dir`), readable by root only, since both
  files often hold passwords. Saving writes the new files beside the old ones, checks them
  with `docker compose config`, and only then renames them into place, so a rejected file
  never replaces a working one. Stack names follow compose's own rule for project names,
  which also keeps them inside the stacks folder.
- **Projects started outside Sentinel** are found with `docker compose ls` and controlled
  with the compose files it reports. Their compose file can be edited where it is if the
  agent can read it; their `.env` is never shown. "Pull" pulls newer images and redeploys.
- **Tests**: unit tests run the real SDK against a fake engine answering with responses
  captured from Docker 29.9; an integration test (`-tags integration`) drives a real container
  where Docker is installed, as on CI's Linux runners.

- **On the server**, registry logins are kept encrypted with the server's secret key, bound
  to their registry's host, and decrypted only to go with one pull or update check for an
  image from that registry; the API never returns a password. Admins manage them. The
  registry package's own tests cover it to 93%: what's left are SQL errors a working database
  can't produce; the encryption itself is in the security-critical secrets package.
- **Who may do what**: viewers see containers, images, volumes, networks, stacks and container
  logs (§8.6: inventory and logs); everything that changes something, and anything that shows
  secrets (inspect, a stack's files), needs `manage_docker`; a shell in a container also needs
  a second factor within 15 minutes, like the terminal (SEC-07). Every change is audited, with
  names but never file contents.
- **Image updates** are checked daily for every online machine, and on demand: the agent asks
  the image's registry for the digest its tag points to now (a manifest request, which
  doesn't count as a pull) and the server keeps the latest answer per machine and tag.
- **Long actions** (pulls, deploys) get their own time limits on the server: 15 and 5 minutes,
  rather than the 30 seconds of other requests.
- **Followed logs** reach the browser as server-sent events (the recent lines, then each new
  one, then the reason it ended), relayed from a stream on the agent's connection.

## Consequences

- A conflict (removing a running container, a volume in use) is its own error code,
  `conflict`, shown with Docker's message.
- Stack pulls use the engine's own registry logins (`docker login` on the machine), not the
  server's: `docker compose pull` reads logins from a file, and logins must not be written to
  disk on the agent. Pulling a stack's images one by one through the Engine API with the
  server's logins would close this gap; until then, the spec should say that registry logins
  apply to image pulls and update checks.
- Rootless Docker or Podman under a user account needs its socket set in the policy file.

#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Starts a local development environment:
#
#   - sentinel-server on http://127.0.0.1:7443, serving the embedded web UI
#   - the Vite dev server on http://127.0.0.1:5173, with hot reload, which
#     forwards API and agent traffic to sentinel-server
#
# Open the Vite URL while working on the UI; changes appear without a
# rebuild. Press Ctrl+C to stop both.
#
# The server is plain HTTP on loopback until TLS is implemented
# (docs/decisions/0002-loopback-only-until-tls.md). A local agent will be
# started and enrolled here once agents exist.
set -euo pipefail

cd "$(dirname "$0")/.."

server_address="${SENTINEL_DEV_ADDRESS:-127.0.0.1:7443}"

echo "==> Building the web UI and the server"
make build

echo "==> Starting sentinel-server on http://$server_address"
server_binary="bin/sentinel-server"
if [ -f "$server_binary.exe" ]; then
  server_binary="$server_binary.exe"
fi
"$server_binary" --listen "$server_address" &
server_pid=$!

# Stop the server however this script exits (Ctrl+C, an error, or Vite quitting).
stop_server() {
  if kill -0 "$server_pid" 2>/dev/null; then
    kill "$server_pid"
    wait "$server_pid" 2>/dev/null || true
  fi
}
trap stop_server EXIT

echo "==> Starting the Vite dev server on http://127.0.0.1:5173"
(cd web && SENTINEL_DEV_BACKEND="http://$server_address" npm run dev)

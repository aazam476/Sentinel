#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Starts a local development environment:
#
#   - sentinel-server on https://127.0.0.1:7443 (self-signed certificate),
#     keeping its data in .dev/data
#   - the Vite dev server on http://localhost:5173, with hot reload, which
#     forwards API and agent traffic to sentinel-server
#
# Work in the browser at http://localhost:5173. That is the server's
# configured public URL in development, so logins and forms only work there.
# Press Ctrl+C to stop both.
#
# Delete .dev/ to start again from a fresh install.
# A local agent will be started and enrolled here once agents exist.
set -euo pipefail

cd "$(dirname "$0")/.."

dev_dir=".dev"
config_file="$dev_dir/server.toml"
mkdir -p "$dev_dir"
if [ ! -f "$config_file" ]; then
  cat > "$config_file" <<'TOML'
# Development settings written by scripts/run-dev.sh. Edit freely.
listen = "127.0.0.1:7443"
data_dir = ".dev/data"
# The browser talks to the Vite dev server, so that is the public origin.
# A host name rather than 127.0.0.1, because passkeys need one.
public_url = "http://localhost:5173"
TOML
fi

echo "==> Building the web UI and the server"
make build

echo "==> Starting sentinel-server on https://127.0.0.1:7443"
server_binary="bin/sentinel-server"
if [ -f "$server_binary.exe" ]; then
  server_binary="$server_binary.exe"
fi
"$server_binary" --config "$config_file" &
server_pid=$!

# Stop the server however this script exits (Ctrl+C, an error, or Vite quitting).
stop_server() {
  if kill -0 "$server_pid" 2>/dev/null; then
    kill "$server_pid"
    wait "$server_pid" 2>/dev/null || true
  fi
}
trap stop_server EXIT

echo "==> Starting the Vite dev server on http://localhost:5173"
(cd web && SENTINEL_DEV_BACKEND="https://127.0.0.1:7443" npm run dev)

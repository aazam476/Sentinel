#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Builds the agent and server twice, from two copies of the source in
# different folders, and fails unless the binaries are identical (SEC-33).
set -euo pipefail

cd "$(dirname "$0")/.."

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
for copy in one two; do
  mkdir -p "$work/$copy"
  git ls-files -z --cached --others --exclude-standard | xargs -0 cp --parents -t "$work/$copy"
  cp -r internal/server/web/dist "$work/$copy/internal/server/web/" 2>/dev/null || true
  for product in sentinel-agent sentinel-server; do
    (cd "$work/$copy" && CGO_ENABLED=0 GOFLAGS=-buildvcs=false go build -trimpath -ldflags=-buildid= -o "$work/$copy.$product" "./cmd/$product")
  done
done
for product in sentinel-agent sentinel-server; do
  if ! cmp -s "$work/one.$product" "$work/two.$product"; then
    echo "check-reproducible: $product differs between two builds of the same source" >&2
    exit 1
  fi
done
echo "check-reproducible: both binaries are identical across builds"

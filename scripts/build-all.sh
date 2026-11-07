#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Cross-compiles both binaries for each platform given as an argument
# (for example linux/amd64) into bin/<os>-<arch>/.
#
# Usage: scripts/build-all.sh linux/amd64 darwin/arm64 windows/amd64 ...
#
# Both binaries build without cgo. The remote desktop code, which will need
# cgo, is built separately on native runners when it exists.
set -euo pipefail

cd "$(dirname "$0")/.."

if [ "$#" -eq 0 ]; then
  echo "usage: build-all.sh <os>/<arch> ..." >&2
  exit 2
fi

for platform in "$@"; do
  os="${platform%/*}"
  arch="${platform#*/}"
  extension=""
  if [ "$os" = "windows" ]; then
    extension=".exe"
  fi

  echo "building $os/$arch"
  for binary in sentinel-server sentinel-agent; do
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
      go build -o "bin/$os-$arch/$binary$extension" "./cmd/$binary"
  done
done

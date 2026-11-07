#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Copies the Vite build output into the folder the server binary embeds.
#
# Usage: scripts/copy-web-build.sh <vite output dir> <embed dir>
#
# The embed dir keeps a committed .gitkeep so the Go embed directive compiles
# on a fresh checkout. Everything else in it is replaced, so files from an
# older build never linger in the binary.
set -euo pipefail

source_dir="${1:?usage: copy-web-build.sh <vite output dir> <embed dir>}"
target_dir="${2:?usage: copy-web-build.sh <vite output dir> <embed dir>}"

if [ ! -f "$source_dir/index.html" ]; then
  echo "copy-web-build: $source_dir/index.html not found; did the web build run?" >&2
  exit 1
fi

find "$target_dir" -mindepth 1 ! -name .gitkeep -delete
cp -R "$source_dir/." "$target_dir/"

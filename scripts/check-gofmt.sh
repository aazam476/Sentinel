#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Fails if any Go file isn't formatted with gofmt.
set -euo pipefail

cd "$(dirname "$0")/.."

unformatted="$(gofmt -l cmd internal)"
if [ -n "$unformatted" ]; then
  echo "These files need formatting (run: gofmt -w <file>):"
  echo "$unformatted"
  exit 1
fi

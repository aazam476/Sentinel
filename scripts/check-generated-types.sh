#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Fails if web/src/api/types.gen.ts doesn't match what the Go API structs
# generate, so the web UI's types can never drift from the server's. When it
# fails it leaves the regenerated file in place, ready to commit.
set -euo pipefail

cd "$(dirname "$0")/.."

generated="web/src/api/types.gen.ts"
before="$(mktemp)"
trap 'rm -f "$before"' EXIT
cp "$generated" "$before"

scripts/gen-types.sh

if ! cmp -s "$before" "$generated"; then
  echo "check-generated-types: $generated was out of date and has been regenerated; commit it."
  diff -u "$before" "$generated" || true
  exit 1
fi
echo "check-generated-types: $generated is up to date"

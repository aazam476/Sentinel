#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Fails if the web UI's generated types (web/src/api/*.gen.ts) don't match
# what the Go API and protocol structs generate, so the web UI's types can
# never drift from the server's. When it fails it leaves the regenerated
# files in place, ready to commit.
set -euo pipefail

cd "$(dirname "$0")/.."

before="$(mktemp -d)"
trap 'rm -rf "$before"' EXIT
cp web/src/api/*.gen.ts "$before/"

scripts/gen-types.sh

status=0
for generated in web/src/api/*.gen.ts; do
  if ! cmp -s "$before/$(basename "$generated")" "$generated"; then
    echo "check-generated-types: $generated was out of date and has been regenerated; commit it."
    diff -u "$before/$(basename "$generated")" "$generated" || true
    status=1
  fi
done
[ "$status" -eq 0 ] && echo "check-generated-types: the generated types are up to date"
exit "$status"

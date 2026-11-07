#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Runs every Go fuzz target in the repository, one after another, for the
# given duration each.
#
# Usage: scripts/fuzz.sh [duration]     (default 30s; CI smoke runs use 10s)
#
# Go can only fuzz one target per `go test` run, so this script finds each
# "func FuzzXxx" and runs it separately.
set -euo pipefail

cd "$(dirname "$0")/.."

fuzz_time="${1:-30s}"
target_count=0

while IFS= read -r test_file; do
  package_dir="./$(dirname "$test_file")"
  while IFS= read -r target; do
    echo "fuzzing $target in $package_dir for $fuzz_time"
    go test -run='^$' -fuzz="^${target}\$" -fuzztime="$fuzz_time" "$package_dir"
    target_count=$((target_count + 1))
  done < <(grep -oE '^func Fuzz[A-Za-z0-9_]+' "$test_file" | sed 's/^func //')
done < <(git ls-files --cached --others --exclude-standard -- 'cmd/*_test.go' 'internal/*_test.go')

echo "fuzz: $target_count target(s) passed"

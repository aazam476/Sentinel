#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Security traceability check. Fails when:
#
#   1. an ID listed in scripts/sec-items.txt has no test named after it
#      (Go: func TestSEC05_... or FuzzSEC05_...; web: a test title containing
#      "SEC-05"), or
#   2. non-test code mentions a SEC ID that isn't listed in sec-items.txt,
#      which means someone started implementing it without registering it.
set -euo pipefail

cd "$(dirname "$0")/.."

items_file="scripts/sec-items.txt"
mapfile -t listed_ids < <(grep -oE '^SEC-[0-9]{2}' "$items_file" | sort -u)

tracked_files() {
  git ls-files --cached --others --exclude-standard -- "$@"
}

go_test_files="$(tracked_files 'cmd/*_test.go' 'internal/*_test.go')"
web_test_files="$(tracked_files 'web/src/*.test.ts' 'web/src/*.test.tsx' 'web/e2e/*.spec.ts')"
code_files="$(tracked_files 'cmd/*.go' 'internal/*.go' 'web/src/*.ts' 'web/src/*.tsx' \
  ':!:*_test.go' ':!:*.test.ts' ':!:*.test.tsx')"

problems=0

for id in "${listed_ids[@]}"; do
  number="${id#SEC-}"
  found=""
  if [ -n "$go_test_files" ] &&
    echo "$go_test_files" | xargs grep -lE "^func (Test|Fuzz)SEC${number}_" >/dev/null 2>&1; then
    found="yes"
  fi
  if [ -z "$found" ] && [ -n "$web_test_files" ] &&
    echo "$web_test_files" | xargs grep -lE "(it|test|describe)\(['\"\`][^'\"\`]*${id}\b" >/dev/null 2>&1; then
    found="yes"
  fi

  if [ -n "$found" ]; then
    echo "ok       $id"
  else
    echo "MISSING  $id has no test named after it (Go: func TestSEC${number}_..., web: a test title containing $id)"
    problems=$((problems + 1))
  fi
done

if [ -n "$code_files" ]; then
  mapfile -t mentioned_ids < <(echo "$code_files" | xargs grep -ohE 'SEC-[0-9]{2}' 2>/dev/null | sort -u || true)
  for id in "${mentioned_ids[@]}"; do
    [ -n "$id" ] || continue
    if ! printf '%s\n' "${listed_ids[@]}" | grep -qx "$id"; then
      echo "UNLISTED $id is mentioned in code but not listed in $items_file"
      problems=$((problems + 1))
    fi
  done
fi

if [ "$problems" -gt 0 ]; then
  echo
  echo "check-sec-coverage: $problems problem(s)"
  exit 1
fi
echo
echo "check-sec-coverage: all ${#listed_ids[@]} listed SEC item(s) have tests"

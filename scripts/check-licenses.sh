#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Fails if any Go module or npm package the project depends on is under the
# GPL, AGPL or LGPL, which can't be combined with Sentinel's Apache-2.0
# license. See docs/DEPENDENCIES.md for the list of dependencies and why each
# one is needed.
set -euo pipefail

cd "$(dirname "$0")/.."

problems=0

echo "Go modules:"
# Only modules whose code is compiled into a binary matter, so the list comes
# from the packages the two commands import. Each line is "<module path>
# <directory in the module cache>"; the main module itself is skipped.
while read -r module_path module_dir; do
  [ -n "$module_dir" ] || continue
  # Only the module's own license. Some modules also ship an inventory of
  # third-party notices (LICENSE-3RD-PARTY.md and similar) that mentions
  # test-only tools; those are reviewed by hand in docs/DEPENDENCIES.md.
  license_files="$(find "$module_dir" -maxdepth 1 -type f \( -iname 'LICENSE*' -o -iname 'COPYING*' \) \
    ! -iname '*3RD*PARTY*' ! -iname '*THIRD*PARTY*')"
  if [ -z "$license_files" ]; then
    echo "  UNKNOWN  $module_path has no LICENSE file"
    problems=$((problems + 1))
    continue
  fi
  # shellcheck disable=SC2086 # license_files is a newline-separated list of paths without spaces
  if grep -qiE 'GNU (AFFERO |LESSER )?GENERAL PUBLIC LICENSE' $license_files; then
    echo "  DENIED   $module_path is GPL-family licensed"
    problems=$((problems + 1))
  else
    echo "  ok       $module_path"
  fi
done < <(go list -deps -f '{{with .Module}}{{if .Version}}{{.Path}} {{.Dir}}{{end}}{{end}}' ./cmd/... | sort -u)

echo "npm packages:"
if ! node scripts/check-npm-licenses.mjs web; then
  problems=$((problems + 1))
fi

if [ "$problems" -gt 0 ]; then
  echo
  echo "check-licenses: found license problems"
  exit 1
fi
echo
echo "check-licenses: no GPL-family dependencies"

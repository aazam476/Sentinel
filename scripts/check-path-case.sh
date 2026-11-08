#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Fails if two files differ only in letter case, such as installCommands.ts
# and InstallCommands.tsx's import "./InstallCommands" meeting
# "./installCommands". macOS and Windows treat such names as the same file,
# so the build breaks there while it works on Linux.
set -euo pipefail

cd "$(dirname "$0")/.."

# Compare paths without their extension, since imports leave it out.
clashes="$(git ls-files --cached --others --exclude-standard \
  | sed -E 's/\.[^./]+$//' | sort -u | tr '[:upper:]' '[:lower:]' | sort | uniq -d)"

if [ -n "$clashes" ]; then
  echo "check-path-case: these paths differ only in letter case:"
  echo "$clashes"
  exit 1
fi
echo "check-path-case: no paths that differ only in letter case"

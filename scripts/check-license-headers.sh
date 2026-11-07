#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Fails if a source file in the repository doesn't declare its license with
# "SPDX-License-Identifier: Apache-2.0" in its first five lines.
#
# Only files tracked by git (or staged) are checked, so build output and
# node_modules are never scanned. Files that can't hold comments (JSON) and
# text that isn't source code (Markdown, the LICENSE itself) are skipped.
set -euo pipefail

cd "$(dirname "$0")/.."

header="SPDX-License-Identifier: Apache-2.0"
missing=()

while IFS= read -r file; do
  [ -f "$file" ] || continue
  if ! head -n 5 "$file" | grep -qF "$header"; then
    missing+=("$file")
  fi
done < <(
  git ls-files --cached --others --exclude-standard -- \
    '*.go' '*.ts' '*.tsx' '*.js' '*.mjs' '*.css' '*.sh' '*.html' '*.yml' '*.yaml' 'Makefile' \
    ':!:internal/server/web/dist/**'
)

if [ "${#missing[@]}" -gt 0 ]; then
  echo "These files are missing the '$header' header:"
  printf '  %s\n' "${missing[@]}"
  exit 1
fi

echo "check-license-headers: every source file has the license header"

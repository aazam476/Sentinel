#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Runs the Go unit tests with coverage and fails if any package is below its
# minimum statement coverage:
#
#   95%  security-critical packages (listed in security_critical below)
#   85%  every other package with code
#
# cmd/ (wiring only) and internal/testutil/ (helpers used only by tests) are
# excluded. The web UI's 80% minimum is enforced by vitest (web/vitest.config.ts).
set -euo pipefail

cd "$(dirname "$0")/.."

module="$(go list -m)"

# Packages whose bugs would weaken security: authentication of users and
# agents (the handshake lives in protocol, hub and agent/core), enrollment, keys,
# secrets, the audit log, the agent's policy and updates.
security_critical=(
  "$module/internal/server/secrets"
  "$module/internal/server/backup"
  "$module/internal/server/auth"
  "$module/internal/server/audit"
  "$module/internal/server/fleet"
  "$module/internal/server/hub"
  "$module/internal/server/serverkey"
  "$module/internal/shared/keyfile"
  "$module/internal/protocol"
  "$module/internal/agent/core"
  "$module/internal/agent/identity"
  "$module/internal/agent/policy"
  "$module/internal/agent/update"
)

is_security_critical() {
  local package="$1"
  for critical in "${security_critical[@]}"; do
    if [ "$package" = "$critical" ] || [[ "$package" == "$critical/"* ]]; then
      return 0
    fi
  done
  return 1
}

is_excluded() {
  local package="$1"
  [[ "$package" == "$module/cmd/"* ]] || [[ "$package" == "$module/internal/testutil"* ]]
}

failures=0
# A failing test fails this script too, but only after its output is
# printed: otherwise `set -e` would stop here and hide which test failed.
test_status=0
output="$(go test -race -cover ./... 2>&1)" || test_status=$?
echo "$output"
echo
if [ "$test_status" -ne 0 ]; then
  echo "check-coverage: tests failed; see above"
  exit "$test_status"
fi

while IFS= read -r line; do
  # Lines look like:  ok   <package>  0.5s  coverage: 91.2% of statements
  # or, for a package without tests:   <package>  coverage: 0.0% of statements
  [[ "$line" =~ coverage:\ ([0-9.]+)%\ of\ statements ]] || continue
  percent="${BASH_REMATCH[1]}"
  package="$(echo "$line" | awk '{ print ($1 == "ok" ? $2 : $1) }')"

  if is_excluded "$package"; then
    continue
  fi

  minimum=85
  if is_security_critical "$package"; then
    minimum=95
  fi

  if awk -v got="$percent" -v want="$minimum" 'BEGIN { exit !(got < want) }'; then
    echo "FAIL  $package: $percent% < $minimum%"
    failures=$((failures + 1))
  else
    echo "ok    $package: $percent% >= $minimum%"
  fi
done <<< "$output"

if [ "$failures" -gt 0 ]; then
  echo
  echo "check-coverage: $failures package(s) below the minimum"
  exit 1
fi
echo
echo "check-coverage: every Go package meets its minimum"

#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Removes build and test output. Run from the repository root (`make clean`).
set -euo pipefail

cd "$(dirname "$0")/.."

rm -rf -- bin web/dist web/coverage web/playwright-report web/test-results coverage.out
find internal/server/web/dist -mindepth 1 ! -name .gitkeep -delete

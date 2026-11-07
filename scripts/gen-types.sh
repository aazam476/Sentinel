#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Regenerates web/src/api/types.gen.ts from the Go API structs in
# internal/server/api (configured in tygo.yaml). Run it after changing an
# API type and commit the result.
set -euo pipefail

cd "$(dirname "$0")/.."
go tool tygo generate

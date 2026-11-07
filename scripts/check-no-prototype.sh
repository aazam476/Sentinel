#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Fails if the production web build contains a design variant picker.
#
# Variant pickers are throwaway tools for comparing design options side by
# side. They must be removed once a variant is chosen. By convention every
# picker marks its root element with a data-variant-picker attribute (see
# docs/CONTRIBUTING.md), which survives minification, so this script looks
# for that marker in the built files.
#
# Usage: scripts/check-no-prototype.sh <web build dir>
set -euo pipefail

build_dir="${1:?usage: check-no-prototype.sh <web build dir>}"

if [ ! -f "$build_dir/index.html" ]; then
  echo "check-no-prototype: $build_dir/index.html not found; build the web UI first" >&2
  exit 1
fi

if grep -rlF "data-variant-picker" "$build_dir"; then
  echo "check-no-prototype: the files above contain a design variant picker; remove it before shipping"
  exit 1
fi

echo "check-no-prototype: no variant picker in the production build"

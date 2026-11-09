#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Builds the macOS agent installer (§11.2, §11.4) from dist/: a universal
# sentinel-agent (Apple silicon and Intel) in /Library/Sentinel/bin. Run on
# macOS, after `scripts/release.sh build` and before `sign`:
#
#   scripts/build-pkg.sh <version>
#
# With an Apple Developer ID it is signed and notarized as Apple requires:
#   MACOS_APP_IDENTITY        "Developer ID Application: …" (signs the binary,
#                             with the hardened runtime and a timestamp)
#   MACOS_INSTALLER_IDENTITY  "Developer ID Installer: …" (signs the .pkg)
#   MACOS_NOTARY_PROFILE      a notarytool keychain profile (xcrun notarytool
#                             store-credentials); the ticket is stapled on
# Without them it builds an unsigned package, for testing.
#
# Codesigning changes the binary, so the darwin binaries in dist/ are
# replaced by the signed ones; `release.sh sign` then signs those with the
# release key.
set -euo pipefail

cd "$(dirname "$0")/.."

version="${1:?usage: build-pkg.sh <version>}"
[ "$(uname -s)" = Darwin ] || { echo "build-pkg.sh runs on macOS" >&2; exit 1; }

identifier="io.github.aazam476.sentinel.agent"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

amd64="dist/sentinel-agent_${version}_darwin_amd64"
arm64="dist/sentinel-agent_${version}_darwin_arm64"
for binary in "$amd64" "$arm64"; do
  [ -f "$binary" ] || { echo "build-pkg.sh: $binary is missing; run release.sh build first" >&2; exit 1; }
done

if [ -n "${MACOS_APP_IDENTITY:-}" ]; then
  for binary in "$amd64" "$arm64"; do
    codesign --force --options runtime --timestamp --identifier "$identifier" --sign "$MACOS_APP_IDENTITY" "$binary"
  done
fi

mkdir -p "$work/root/Library/Sentinel/bin" "$work/scripts"
lipo -create -output "$work/root/Library/Sentinel/bin/sentinel-agent" "$amd64" "$arm64"
if [ -n "${MACOS_APP_IDENTITY:-}" ]; then
  codesign --force --options runtime --timestamp --identifier "$identifier" --sign "$MACOS_APP_IDENTITY" \
    "$work/root/Library/Sentinel/bin/sentinel-agent"
fi
cp packaging/macos/postinstall "$work/scripts/postinstall"
chmod 755 "$work/scripts/postinstall"

pkg="dist/sentinel-agent_${version}_darwin.pkg"
component="$work/sentinel-agent.pkg"
pkgbuild --root "$work/root" --identifier "$identifier" --version "${version#v}" \
  --scripts "$work/scripts" --install-location / "$component"
sign_args=()
if [ -n "${MACOS_INSTALLER_IDENTITY:-}" ]; then
  sign_args=(--sign "$MACOS_INSTALLER_IDENTITY" --timestamp)
fi
productbuild --package "$component" --identifier "$identifier" --version "${version#v}" "${sign_args[@]}" "$pkg"

if [ -n "${MACOS_NOTARY_PROFILE:-}" ]; then
  xcrun notarytool submit "$pkg" --keychain-profile "$MACOS_NOTARY_PROFILE" --wait
  xcrun stapler staple "$pkg"
fi
echo "built $pkg"

#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Authenticode-signs the Windows binaries in dist/ with a code signing
# certificate, when one is configured (ADR 0040). Without one it changes
# nothing: Windows releases are unsigned by default (§11.4), and Sentinel's
# own Ed25519 signatures (SEC-30) protect them either way. Run it after
# `scripts/release.sh build` and before `sign`: Authenticode changes the
# binary, and the Ed25519 signature must cover the signed file.
#
#   WINDOWS_CERTIFICATE_PFX       the certificate and its key, a base64 .pfx
#   WINDOWS_CERTIFICATE_PASSWORD  the .pfx's password
#   WINDOWS_TIMESTAMP_URL         an RFC 3161 timestamp server (default
#                                 http://timestamp.digicert.com), so the
#                                 signature outlives the certificate; set
#                                 it empty to skip, for a test certificate
#
# Needs osslsigncode (apt-get install osslsigncode).
set -euo pipefail

dist="${1:-dist}"

if [ -z "${WINDOWS_CERTIFICATE_PFX:-}" ]; then
  echo "No Windows code signing certificate: the Windows binaries stay unsigned."
  exit 0
fi
if ! command -v osslsigncode >/dev/null; then
  echo "sign-windows: osslsigncode is needed to sign the Windows binaries" >&2
  exit 1
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
printf '%s' "$WINDOWS_CERTIFICATE_PFX" | base64 --decode > "$work/certificate.pfx"
# The password goes in a file, not on the command line, where other
# processes could read it.
printf '%s' "${WINDOWS_CERTIFICATE_PASSWORD:-}" > "$work/password"

timestamp=()
url="${WINDOWS_TIMESTAMP_URL-http://timestamp.digicert.com}"
if [ -n "$url" ]; then
  timestamp=(-ts "$url")
fi

signed=0
for binary in "$dist"/sentinel-*_windows_*.exe; do
  [ -f "$binary" ] || continue
  echo "signing $binary"
  osslsigncode sign -pkcs12 "$work/certificate.pfx" -readpass "$work/password" \
    -n "Sentinel" -i "https://github.com/aazam476/sentinel" -h sha256 "${timestamp[@]}" \
    -in "$binary" -out "$work/signed.exe"
  mv "$work/signed.exe" "$binary"
  signed=$((signed + 1))
done
if [ "$signed" -eq 0 ]; then
  echo "sign-windows: no Windows binaries in $dist" >&2
  exit 1
fi
echo "Signed $signed Windows binaries."

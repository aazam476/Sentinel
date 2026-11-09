#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Builds, signs and describes a release (SEC-30, SEC-33) into dist/:
#
#   sentinel-{server,agent}_<version>_<os>_<arch>[.exe]   the binaries
#   <binary>.sig                                         Ed25519 signatures
#   <binary>.spdx.json                                   SBOMs
#   SHA256SUMS, SHA256SUMS.sig                           checksums of everything
#
# Usage: scripts/release.sh <version> [os/arch ...]
#
# The private key comes from $SENTINEL_RELEASE_KEY. Builds are reproducible:
# the same commit and Go version give the same bytes (-trimpath, no build
# ID, cgo off, and the commit's time for anything dated). Set
# ALLOW_DEVELOPMENT_KEY=1 to try it with the development key; such a build
# must never be published.
set -euo pipefail

cd "$(dirname "$0")/.."

version="${1:?usage: release.sh <version> [os/arch ...]}"
shift
platforms=("$@")
if [ "${#platforms[@]}" -eq 0 ]; then
  platforms=(linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64)
fi

export SOURCE_DATE_EPOCH
SOURCE_DATE_EPOCH="$(git log -1 --format=%ct)"
export CGO_ENABLED=0
build_flags=(-trimpath -ldflags=-buildid=)

tool="$(mktemp -d)/sentinel-release"
go build -o "$tool" ./cmd/sentinel-release
if [ "${ALLOW_DEVELOPMENT_KEY:-}" != "1" ]; then
  "$tool" check-key
fi

rm -rf dist
mkdir -p dist
for platform in "${platforms[@]}"; do
  os="${platform%/*}"
  arch="${platform#*/}"
  extension=""
  if [ "$os" = "windows" ]; then
    extension=".exe"
  fi
  for product in sentinel-server sentinel-agent; do
    file="dist/${product}_${version}_${os}_${arch}${extension}"
    echo "building $file"
    GOOS="$os" GOARCH="$arch" go build "${build_flags[@]}" -o "$file" "./cmd/$product"
    "$tool" sign --name "$product" --version "$version" --os "$os" --arch "$arch" "$file"
    lock=()
    if [ "$product" = "sentinel-server" ]; then
      lock=(--npm-lock web/package-lock.json)
    fi
    "$tool" sbom --name "$product" --version "$version" "${lock[@]}" "$file" > "$file.spdx.json"
  done
done

(cd dist && "$tool" sums ./*) > SHA256SUMS.tmp
mv SHA256SUMS.tmp dist/SHA256SUMS
"$tool" sign --name SHA256SUMS --version "$version" dist/SHA256SUMS
echo "release $version is in dist/"

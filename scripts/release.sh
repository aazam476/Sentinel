#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Builds, signs and describes a release (SEC-30, SEC-33) into dist/:
#
#   sentinel-{server,agent}_<version>_<os>_<arch>[.exe]   the binaries
#   <file>.sig                                           Ed25519 signatures
#   <binary>.spdx.json                                   SBOMs
#   install-server.sh                                    the server's install script
#   SHA256SUMS                                           checksums of everything
#
# Usage:
#   scripts/release.sh build <version> [os/arch ...]   build the binaries into dist/
#   scripts/release.sh sign <version>                  sign, describe and list what is in dist/
#   scripts/release.sh all <version> [os/arch ...]     both
#
# The two halves are separate so the release workflow can codesign the
# macOS binaries (which changes them) between building and signing.
#
# The private key comes from $SENTINEL_RELEASE_KEY. Builds are reproducible:
# the same commit and Go version give the same bytes (-trimpath, no build
# ID, cgo off, the version stamped in, and the commit's time for anything
# dated). Set ALLOW_DEVELOPMENT_KEY=1 to try it with the development key;
# such a build must never be published.
set -euo pipefail

cd "$(dirname "$0")/.."

usage="usage: release.sh build|sign|all <version> [os/arch ...]"
phase="${1:?$usage}"
version="${2:?$usage}"
shift 2
platforms=("$@")
if [ "${#platforms[@]}" -eq 0 ]; then
  platforms=(linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64)
fi

export SOURCE_DATE_EPOCH
SOURCE_DATE_EPOCH="$(git log -1 --format=%ct)"
export CGO_ENABLED=0

tool="$(mktemp -d)/sentinel-release"
go build -o "$tool" ./cmd/sentinel-release
if [ "${ALLOW_DEVELOPMENT_KEY:-}" != "1" ]; then
  "$tool" check-key
fi

build() {
  # The version is stamped in, so it is right even when the tree isn't
  # exactly the tagged commit (Go would otherwise record a pseudo-version).
  local flags=(-trimpath "-ldflags=-buildid= -X github.com/aazam476/sentinel/internal/shared/buildinfo.stamped=$version")
  rm -rf dist
  mkdir -p dist
  for platform in "${platforms[@]}"; do
    local os="${platform%/*}" arch="${platform#*/}" extension=""
    if [ "$os" = "windows" ]; then
      extension=".exe"
    fi
    for product in sentinel-server sentinel-agent; do
      local file="dist/${product}_${version}_${os}_${arch}${extension}"
      echo "building $file"
      GOOS="$os" GOARCH="$arch" go build "${flags[@]}" -o "$file" "./cmd/$product"
    done
  done
}

# release_keys reads a key file's keys onto one line (grep finds nothing in
# production.pub until a key is added, hence || true).
release_keys() {
  { grep -v '^#' "$1" || true; } | tr -s ' \n' ' ' | sed 's/^ *//; s/ *$//'
}

sign() {
  shopt -s nullglob
  for file in dist/sentinel-server_* dist/sentinel-agent_*; do
    case "$file" in *.sig | *.json | *.pkg) continue ;; esac
    local name product os arch
    name="$(basename "$file")"
    name="${name%.exe}"
    product="${name%%_*}"
    arch="${name##*_}"
    os="${name%_*}"
    os="${os##*_}"
    "$tool" sign --name "$product" --version "$version" --os "$os" --arch "$arch" "$file"
    local lock=()
    if [ "$product" = "sentinel-server" ]; then
      lock=(--npm-lock web/package-lock.json)
    fi
    "$tool" sbom --name "$product" --version "$version" "${lock[@]}" "$file" > "$file.spdx.json"
  done
  # The macOS installer, if the workflow built one.
  for file in dist/*.pkg; do
    "$tool" sign --name "$(basename "$file")" --version "$version" --os darwin "$file"
  done

  # The server's install script carries the release keys it checks with.
  local keys
  keys="$(release_keys internal/shared/release/keys/production.pub)"
  if [ -z "$keys" ]; then
    keys="$(release_keys internal/shared/release/keys/development.pub)"
  fi
  sed "s|@RELEASE_KEYS@|$keys|" scripts/install-server.sh > dist/install-server.sh
  "$tool" sign --name install-server.sh --version "$version" dist/install-server.sh

  rm -f dist/SHA256SUMS dist/SHA256SUMS.sig
  (cd dist && "$tool" sums ./*) > SHA256SUMS.tmp
  mv SHA256SUMS.tmp dist/SHA256SUMS
  "$tool" sign --name SHA256SUMS --version "$version" dist/SHA256SUMS
  echo "release $version is in dist/"
}

case "$phase" in
build) build ;;
sign) sign ;;
all) build && sign ;;
*) echo "$usage" >&2; exit 2 ;;
esac

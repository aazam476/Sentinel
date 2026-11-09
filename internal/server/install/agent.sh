#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
#
# Installs the Sentinel agent on Linux or macOS (§11.2). Run the command
# from the server's Add machine page:
#
#   curl -fsSL https://<server>/install/agent.sh | sudo sh -s -- \
#     --server https://<server> --token <token> --fingerprint <fp>
#
# It downloads the newest agent build the server hosts for this machine,
# checks its Ed25519 release signature with openssl before installing
# anything (SEC-34), installs it, then runs `sentinel-agent install`, which
# enrolls the machine and starts the service (systemd, OpenRC or launchd).
#
# Options besides the enrollment flags:
#   --install-dir DIR   where to put the binary (default /usr/local/bin,
#                       /Library/Sentinel/bin on macOS)
#   --dry-run           download and check, then describe the install
#                       without changing anything
set -eu

# The release keys this server trusts, written in when it serves the script.
RELEASE_KEYS="@RELEASE_KEYS@"

fail() {
	printf 'sentinel install: %s\n' "$*" >&2
	exit 1
}

server="" token="" fingerprint="" tls_fingerprint="" install_dir="" dry_run=""
while [ "$#" -gt 0 ]; do
	case "$1" in
	--server) server="${2:-}"; shift 2 ;;
	--token) token="${2:-}"; shift 2 ;;
	--fingerprint) fingerprint="${2:-}"; shift 2 ;;
	--tls-fingerprint) tls_fingerprint="${2:-}"; shift 2 ;;
	--install-dir) install_dir="${2:-}"; shift 2 ;;
	--dry-run) dry_run="--dry-run"; shift ;;
	*) fail "unknown option $1; copy the command from the server's Add machine page" ;;
	esac
done
[ -n "$server" ] && [ -n "$token" ] && [ -n "$fingerprint" ] ||
	fail "--server, --token and --fingerprint are required; copy the command from the server's Add machine page"

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) fail "this script is for Linux and macOS; on Windows, use the PowerShell command" ;;
esac
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) fail "Sentinel has no agent for $(uname -m) machines" ;;
esac
if [ -z "$install_dir" ]; then
	install_dir=/usr/local/bin
	[ "$os" = darwin ] && install_dir=/Library/Sentinel/bin
fi
if [ -z "$dry_run" ] && [ "$(id -u)" -ne 0 ]; then
	fail "run it as root: pipe it to sudo sh, as the Add machine page shows"
fi
command -v curl >/dev/null 2>&1 || fail "curl is needed"
command -v openssl >/dev/null 2>&1 || fail "openssl is needed to check the agent's signature"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT INT TERM

# download fetches a file from the server, pinning its self-signed
# certificate when the command carries its fingerprint.
download() {
	if [ -n "$tls_fingerprint" ]; then
		pin="$(printf '%s' "${tls_fingerprint#SHA256:}" | tr '_-' '/+')"
		while [ $((${#pin} % 4)) -ne 0 ]; do pin="$pin="; done
		curl -fsSL --insecure --pinnedpubkey "sha256//$pin" -o "$2" "$server$1"
	else
		curl -fsSL -o "$2" "$server$1"
	fi
}

download "/install/agent/$os/$arch/sentinel-agent" "$work/sentinel-agent" ||
	fail "couldn't download the agent from $server; if the server says it has no build, an admin can fetch the latest release on its Updates page"
download "/install/agent/$os/$arch/sentinel-agent.sig" "$work/sentinel-agent.sig" ||
	fail "couldn't download the agent's signature from $server"

# verify checks the build against its release signature (SEC-34): the
# signed manifest names this product and system and the build's SHA-256,
# and the signature over it is valid for one of the trusted keys.
verify() {
	signature_file="$work/sentinel-agent.sig"
	[ "$(sed -n 1p "$signature_file")" = "sentinel-signature v1" ] || return 1
	[ "$(sed -n 2p "$signature_file")" = "name: sentinel-agent" ] || return 1
	[ "$(sed -n 4p "$signature_file")" = "os: $os" ] || return 1
	[ "$(sed -n 5p "$signature_file")" = "arch: $arch" ] || return 1
	[ "$(wc -l <"$signature_file" | tr -d ' ')" = 7 ] || return 1
	digest="$(openssl dgst -sha256 -r "$work/sentinel-agent" | cut -d ' ' -f 1)"
	[ "$(sed -n 6p "$signature_file")" = "sha256: $digest" ] || return 1
	head -n 6 "$signature_file" >"$work/manifest"
	sed -n 's/^signature: //p' "$signature_file" | openssl base64 -d -A >"$work/signature" || return 1
	for key in $RELEASE_KEYS; do
		# An Ed25519 public key in the DER form openssl reads: a fixed
		# 12-byte prefix, then the 32-byte key.
		{
			printf '\060\052\060\005\006\003\053\145\160\003\041\000'
			printf '%s' "$key" | openssl base64 -d -A
		} >"$work/key.der"
		if openssl pkeyutl -verify -pubin -keyform DER -inkey "$work/key.der" -rawin \
			-in "$work/manifest" -sigfile "$work/signature" >/dev/null 2>&1; then
			return 0
		fi
	done
	return 1
}
verify || fail "the downloaded agent's release signature doesn't check out; nothing was installed"
version="$(sed -n 's/^version: //p' "$work/sentinel-agent.sig")"
printf 'Downloaded sentinel-agent %s for %s/%s; its signature checks out.\n' "$version" "$os" "$arch"

if [ -n "$dry_run" ]; then
	printf 'Would install it to %s/sentinel-agent.\n' "$install_dir"
	agent="$work/sentinel-agent"
else
	mkdir -p "$install_dir"
	agent="$install_dir/sentinel-agent"
	cp "$work/sentinel-agent" "$agent.new"
	chmod 755 "$agent.new"
	mv -f "$agent.new" "$agent"
	printf 'Installed %s.\n' "$agent"
fi
chmod 755 "$agent"

set -- install --server "$server" --token "$token" --fingerprint "$fingerprint"
[ -n "$tls_fingerprint" ] && set -- "$@" --tls-fingerprint "$tls_fingerprint"
[ -n "$dry_run" ] && set -- "$@" --dry-run
exec "$agent" "$@" ${SENTINEL_AGENT_FLAGS:-}

#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
#
# Installs the Sentinel server on Linux (§11.1):
#
#   curl -fsSL https://github.com/aazam476/sentinel/releases/latest/download/install-server.sh | sudo sh
#
# It downloads the server for this machine from the release, checks its
# Ed25519 release signature with openssl before installing anything
# (SEC-34), creates the `sentinel` system user, installs the binary to
# /usr/local/bin, writes /etc/sentinel/server.toml (unless there is one),
# creates /var/lib/sentinel (0700), installs and starts a systemd unit, and
# prints the address to open to create the first admin.
#
# Options:
#   --release-url URL   where the release's files are (default: the latest
#                       GitHub release)
#   --dry-run           download and check, then describe the install
#                       without changing anything
set -eu

# The release keys, written in by scripts/release.sh.
RELEASE_KEYS="@RELEASE_KEYS@"
release_url="https://github.com/aazam476/sentinel/releases/latest/download"
dry_run=""

fail() {
	printf 'sentinel install: %s\n' "$*" >&2
	exit 1
}

while [ "$#" -gt 0 ]; do
	case "$1" in
	--release-url) release_url="${2:-}"; shift 2 ;;
	--dry-run) dry_run=1; shift ;;
	*) fail "unknown option $1" ;;
	esac
done

[ "$(uname -s)" = Linux ] || fail "this script installs the server on Linux; see INSTALL.md for other systems"
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) fail "Sentinel has no server for $(uname -m) machines" ;;
esac
if [ -z "$dry_run" ]; then
	[ "$(id -u)" -eq 0 ] || fail "run it as root: pipe it to sudo sh"
	command -v systemctl >/dev/null 2>&1 || fail "this script needs systemd; see INSTALL.md to run the server another way"
fi
command -v curl >/dev/null 2>&1 || fail "curl is needed"
command -v openssl >/dev/null 2>&1 || fail "openssl is needed to check the server's signature"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT INT TERM

# The checksum list names this release's files; it is signed too.
curl -fsSL -o "$work/SHA256SUMS" "$release_url/SHA256SUMS" || fail "couldn't download the release's checksum list"
name="$(sed -n "s/^[0-9a-f]\{64\}  \(sentinel-server_[^ ]*_linux_${arch}\)\$/\1/p" "$work/SHA256SUMS" | head -n 1)"
[ -n "$name" ] || fail "the release has no server for linux/$arch"
curl -fsSL -o "$work/sentinel-server" "$release_url/$name" || fail "couldn't download $name"
curl -fsSL -o "$work/sentinel-server.sig" "$release_url/$name.sig" || fail "couldn't download $name's signature"

# verify checks the binary against its release signature (SEC-34).
verify() {
	signature_file="$work/sentinel-server.sig"
	[ "$(sed -n 1p "$signature_file")" = "sentinel-signature v1" ] || return 1
	[ "$(sed -n 2p "$signature_file")" = "name: sentinel-server" ] || return 1
	[ "$(sed -n 4p "$signature_file")" = "os: linux" ] || return 1
	[ "$(sed -n 5p "$signature_file")" = "arch: $arch" ] || return 1
	[ "$(wc -l <"$signature_file" | tr -d ' ')" = 7 ] || return 1
	digest="$(openssl dgst -sha256 -r "$work/sentinel-server" | cut -d ' ' -f 1)"
	[ "$(sed -n 6p "$signature_file")" = "sha256: $digest" ] || return 1
	head -n 6 "$signature_file" >"$work/manifest"
	sed -n 's/^signature: //p' "$signature_file" | openssl base64 -d -A >"$work/signature" || return 1
	for key in $RELEASE_KEYS; do
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
verify || fail "the downloaded server's release signature doesn't check out; nothing was installed"
version="$(sed -n 's/^version: //p' "$work/sentinel-server.sig")"
printf 'Downloaded sentinel-server %s for linux/%s; its signature checks out.\n' "$version" "$arch"

if [ -n "$dry_run" ]; then
	printf 'Would create the sentinel user, install /usr/local/bin/sentinel-server, write /etc/sentinel/server.toml,\n'
	printf 'create /var/lib/sentinel and start the sentinel-server systemd service.\n'
	exit 0
fi

id sentinel >/dev/null 2>&1 || useradd --system --home-dir /var/lib/sentinel --shell /usr/sbin/nologin sentinel
install -m 0755 "$work/sentinel-server" /usr/local/bin/sentinel-server
install -d -m 0755 /etc/sentinel
if [ ! -f /etc/sentinel/server.toml ]; then
	cat >/etc/sentinel/server.toml <<'EOF'
# Sentinel server settings. See INSTALL.md for every option.
listen = ":7443"
data_dir = "/var/lib/sentinel"
EOF
fi
install -d -m 0700 -o sentinel -g sentinel /var/lib/sentinel
cat >/etc/systemd/system/sentinel-server.service <<'EOF'
[Unit]
Description=Sentinel server
After=network-online.target
Wants=network-online.target

[Service]
User=sentinel
Group=sentinel
ExecStart=/usr/local/bin/sentinel-server --config /etc/sentinel/server.toml
Restart=on-failure
# Port 7443 needs no privileges; a port below 1024 would need this:
# AmbientCapabilities=CAP_NET_BIND_SERVICE
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/var/lib/sentinel
ProtectHome=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now sentinel-server

address="https://$(hostname -f 2>/dev/null || hostname):7443"
printf '\nSentinel %s is running at %s\n' "$version" "$address"
printf 'Open it now to create the first admin: until then, anyone who reaches it can.\n'

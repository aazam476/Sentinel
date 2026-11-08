// SPDX-License-Identifier: Apache-2.0

// Package docker manages containers, images, volumes and networks through
// the Docker Engine API (§9.4), with the official Go SDK. Podman works
// through its Docker-compatible socket. The socket is found by looking in
// the usual places, or set in the local policy file.
package docker

// SPDX-License-Identifier: Apache-2.0

// Package release signs and verifies Sentinel's release artifacts (SEC-30,
// SEC-31).
//
// Every artifact (a binary, an installer, the checksum list) has a
// signature file beside it, "<artifact>.sig". It holds a manifest naming
// what was signed (the artifact's name, version, operating system,
// architecture and SHA-256) and an Ed25519 signature over that manifest
// with the offline release key. Checking a signature checks the manifest,
// then that the artifact matches its SHA-256 and is the one wanted (the
// agent for this system, say), so a signature can't be moved to another
// file, platform or product.
//
// The public keys that are trusted are compiled into both binaries from
// keys/production.pub. Until a production key has been added there, builds
// trust keys/development.pub instead, whose private half is in this
// repository's testdata for tests and local builds; the release workflow
// refuses to publish such a build.
package release

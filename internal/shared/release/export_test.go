// SPDX-License-Identifier: Apache-2.0

package release

import "crypto/ed25519"

// TrustedKeysFrom exposes trustedKeys to tests.
func TrustedKeysFrom(production, development string) []ed25519.PublicKey {
	return trustedKeys(production, development)
}

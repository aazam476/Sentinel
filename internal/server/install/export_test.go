// SPDX-License-Identifier: Apache-2.0

package install

import "crypto/ed25519"

// ScriptKeysForTest is the keys the scripts carry.
func (h *Handler) ScriptKeysForTest() []ed25519.PublicKey { return h.keys }

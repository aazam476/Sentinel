// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// tokenBytes is the randomness in session tokens: 256 bits (SEC-06).
const tokenBytes = 32

// newToken returns a random token for a client to hold, and the hash the
// server stores. Storing only the hash means a copy of the database can't be
// used to take over sessions.
func newToken() (token, tokenHash string) {
	raw := make([]byte, tokenBytes)
	// crypto/rand.Read never returns an error: if the system's random source
	// fails, Go stops the program rather than hand out predictable bytes.
	_, _ = rand.Read(raw)
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token)
}

// hashToken returns the stored form of a token. The token has 256 random
// bits, so a fast hash is enough; nobody can guess one to match a hash.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

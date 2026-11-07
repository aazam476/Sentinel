// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"strings"
)

// recoveryCodeCount is how many one-time recovery codes a user gets (SEC-02).
const recoveryCodeCount = 10

// recoveryCodeBytes is the randomness in each code: 80 bits, which makes
// guessing hopeless even against a stolen copy of the stored hashes, so a
// fast hash (SHA-256) is safe for them, unlike for passwords.
const recoveryCodeBytes = 10

// newRecoveryCodes returns recoveryCodeCount fresh codes, formatted for
// people as "ABCD-EFGH-IJKL-MNOP", and their hashes for storage.
func newRecoveryCodes() (codes, hashes []string) {
	for range recoveryCodeCount {
		raw := make([]byte, recoveryCodeBytes)
		// crypto/rand.Read never returns an error: if the system's random
		// source fails, Go stops the program rather than hand out predictable
		// bytes.
		_, _ = rand.Read(raw)
		text := base32.StdEncoding.EncodeToString(raw) // 16 characters, no padding for 10 bytes

		code := text[0:4] + "-" + text[4:8] + "-" + text[8:12] + "-" + text[12:16]
		codes = append(codes, code)
		hashes = append(hashes, hashRecoveryCode(code))
	}
	return codes, hashes
}

// hashRecoveryCode returns the stored form of a recovery code. Letter case,
// dashes and spaces are ignored, so "abcd efgh ijkl mnop" matches
// "ABCD-EFGH-IJKL-MNOP".
func hashRecoveryCode(code string) string {
	normalized := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(code))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

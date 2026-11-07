// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id settings for new password hashes (SEC-03). Tuning and the reasons
// are in docs/decisions/0010-password-hashing.md. Stored hashes record their
// own settings, so changing these only affects passwords set afterwards.
const (
	argon2Memory      = 64 * 1024 // KiB, so 64 MiB
	argon2Iterations  = 3
	argon2Parallelism = 2
	argon2SaltLength  = 16
	argon2KeyLength   = 32
)

// maxConcurrentHashes limits how many password hashes run at once. Each one
// holds 64 MiB of memory, so a burst of login attempts could otherwise
// exhaust the server's memory.
const maxConcurrentHashes = 2

// errMalformedHash means a stored password hash couldn't be parsed. It is
// never reassigned.
var errMalformedHash = errors.New("stored password hash is malformed")

// passwordHasher hashes and checks passwords with Argon2id.
type passwordHasher struct {
	// slots is a counting semaphore: a hash may only run while it holds one
	// of its buffer slots.
	slots chan struct{}
	// dummyHash is checked against when a login names a user that doesn't
	// exist, so the response takes as long as for a real user and doesn't
	// reveal which usernames exist.
	dummyHash string
}

func newPasswordHasher() *passwordHasher {
	return &passwordHasher{slots: make(chan struct{}, maxConcurrentHashes), dummyHash: newDummyHash()}
}

// newDummyHash returns a hash string with the current settings and a random
// salt and key. Checking a password against it costs exactly as much as
// against a real hash, and no password matches it, so there is no need to
// spend the time deriving it from a real password.
func newDummyHash() string {
	salt := make([]byte, argon2SaltLength)
	key := make([]byte, argon2KeyLength)
	// crypto/rand.Read never returns an error: if the system's random source
	// fails, Go stops the program rather than hand out predictable bytes.
	_, _ = rand.Read(salt)
	_, _ = rand.Read(key)
	return encodeArgon2Hash(salt, key)
}

// Hash returns the encoded Argon2id hash of password, in the standard
// "$argon2id$v=19$m=...,t=...,p=...$salt$hash" format.
func (h *passwordHasher) Hash(ctx context.Context, password string) (string, error) {
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.release()
	return h.hashWithoutLimit(password), nil
}

func (h *passwordHasher) hashWithoutLimit(password string) string {
	salt := make([]byte, argon2SaltLength)
	// crypto/rand.Read never returns an error: if the system's random source
	// fails, Go stops the program rather than hand out predictable bytes.
	_, _ = rand.Read(salt)
	key := argon2.IDKey([]byte(password), salt, argon2Iterations, argon2Memory, argon2Parallelism, argon2KeyLength)
	return encodeArgon2Hash(salt, key)
}

// encodeArgon2Hash formats a hash made with the current settings.
func encodeArgon2Hash(salt, key []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argon2Memory, argon2Iterations,
		argon2Parallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

// Verify reports whether password matches encodedHash. An empty encodedHash
// is checked against the dummy hash and never matches.
func (h *passwordHasher) Verify(ctx context.Context, password, encodedHash string) (bool, error) {
	matchPossible := encodedHash != ""
	if !matchPossible {
		encodedHash = h.dummyHash
	}

	parsed, err := parseArgon2Hash(encodedHash)
	if err != nil {
		return false, err
	}
	if err := h.acquire(ctx); err != nil {
		return false, err
	}
	defer h.release()

	key := argon2.IDKey([]byte(password), parsed.salt, parsed.iterations, parsed.memory, parsed.parallelism,
		uint32(len(parsed.key))) //nolint:gosec // G115: the key length came from a stored 32-byte hash
	return matchPossible && subtle.ConstantTimeCompare(key, parsed.key) == 1, nil
}

func (h *passwordHasher) acquire(ctx context.Context) error {
	select {
	case h.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait to hash password: %w", ctx.Err())
	}
}

func (h *passwordHasher) release() {
	<-h.slots
}

// argon2Hash is a parsed "$argon2id$..." string.
type argon2Hash struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	salt        []byte
	key         []byte
}

func parseArgon2Hash(encoded string) (argon2Hash, error) {
	parts := strings.Split(encoded, "$")
	// "", "argon2id", "v=19", "m=...,t=...,p=...", salt, key
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != fmt.Sprintf("v=%d", argon2.Version) {
		return argon2Hash{}, errMalformedHash
	}

	var parsed argon2Hash
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &parsed.memory, &parsed.iterations, &parsed.parallelism); err != nil {
		return argon2Hash{}, errMalformedHash
	}
	var err error
	if parsed.salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil {
		return argon2Hash{}, errMalformedHash
	}
	if parsed.key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil || len(parsed.key) == 0 {
		return argon2Hash{}, errMalformedHash
	}
	if parsed.memory == 0 || parsed.iterations == 0 || parsed.parallelism == 0 {
		return argon2Hash{}, errMalformedHash
	}
	return parsed, nil
}

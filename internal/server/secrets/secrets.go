// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// KeySize is the length of the key in bytes (AES-256).
const KeySize = 32

// formatVersion is the first byte of every ciphertext. It lets a future key
// or algorithm change recognize values written by this version.
const formatVersion byte = 1

// ErrDecrypt means a ciphertext couldn't be decrypted: it was changed, was
// encrypted with another key, or belongs to different associated data. It is
// never reassigned; it exists so callers can check for it with errors.Is.
var ErrDecrypt = errors.New("decrypt secret: ciphertext is corrupt, from another key, or for another purpose")

// Box encrypts and decrypts secrets with one key.
type Box struct {
	aead cipher.AEAD
}

// NewBox returns a Box that uses key, which must be KeySize bytes.
func NewBox(key []byte) (*Box, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("secret key is %d bytes, want %d", len(key), KeySize)
	}
	// aes.NewCipher only fails for key lengths other than 16, 24 or 32
	// bytes, which the check above rules out.
	block, _ := aes.NewCipher(key)
	// cipher.NewGCM only fails for ciphers whose block size isn't 16 bytes;
	// AES's always is.
	aead, _ := cipher.NewGCM(block)
	return &Box{aead: aead}, nil
}

// Encrypt returns plaintext encrypted and authenticated, bound to
// associatedData. The result is: version byte, random nonce, ciphertext.
func (b *Box) Encrypt(plaintext, associatedData []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	// crypto/rand.Read never returns an error: if the system's random source
	// fails, Go stops the program rather than hand out predictable bytes.
	_, _ = rand.Read(nonce)

	sealed := make([]byte, 0, 1+len(nonce)+len(plaintext)+b.aead.Overhead())
	sealed = append(sealed, formatVersion)
	sealed = append(sealed, nonce...)
	return b.aead.Seal(sealed, nonce, plaintext, associatedData), nil
}

// Decrypt reverses Encrypt. It returns ErrDecrypt if sealed was modified,
// was made with another key, or was encrypted with different
// associatedData.
func (b *Box) Decrypt(sealed, associatedData []byte) ([]byte, error) {
	nonceSize := b.aead.NonceSize()
	if len(sealed) < 1+nonceSize+b.aead.Overhead() || sealed[0] != formatVersion {
		return nil, ErrDecrypt
	}

	nonce := sealed[1 : 1+nonceSize]
	plaintext, err := b.aead.Open(nil, nonce, sealed[1+nonceSize:], associatedData)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}

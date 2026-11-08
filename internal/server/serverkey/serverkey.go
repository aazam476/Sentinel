// SPDX-License-Identifier: Apache-2.0

package serverkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"path/filepath"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/shared/keyfile"
)

// FileName is the key's file name inside the data directory.
const FileName = "server-key"

// Key is the server's identity key pair.
type Key struct {
	private ed25519.PrivateKey
}

// LoadOrCreate reads <dataDir>/server-key, creating a new key pair on first
// start. The file holds the 32-byte Ed25519 seed and only the server's
// user may read it.
//
// Losing or replacing this file means every agent refuses the server, by
// design: re-enroll them, or restore the file from a backup.
func LoadOrCreate(dataDir string) (Key, error) {
	seed, err := keyfile.LoadOrCreate(filepath.Join(dataDir, FileName), "server key", newSeed)
	if err != nil {
		return Key{}, err
	}
	if len(seed) != ed25519.SeedSize {
		return Key{}, fmt.Errorf("server key %s is %d bytes, want %d", filepath.Join(dataDir, FileName), len(seed), ed25519.SeedSize)
	}
	return Key{private: ed25519.NewKeyFromSeed(seed)}, nil
}

// FromPrivateKey wraps an existing private key; tests use it.
func FromPrivateKey(private ed25519.PrivateKey) Key {
	return Key{private: private}
}

// Private signs the server's half of each handshake.
func (k Key) Private() ed25519.PrivateKey {
	return k.private
}

// Public is the key agents save at enrollment.
func (k Key) Public() ed25519.PublicKey {
	public, _ := k.private.Public().(ed25519.PublicKey) // always an ed25519.PublicKey
	return public
}

// Fingerprint is the short form of the public key shown in install
// commands.
func (k Key) Fingerprint() string {
	return protocol.Fingerprint(k.Public())
}

func newSeed() []byte {
	seed := make([]byte, ed25519.SeedSize)
	// crypto/rand.Read never returns an error: Go stops the program if the
	// system's random source fails.
	_, _ = rand.Read(seed)
	return seed
}

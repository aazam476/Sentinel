// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"crypto/rand"
	"path/filepath"

	"github.com/aazam476/sentinel/internal/shared/keyfile"
)

// KeyFileName is the key's file name inside the data directory.
const KeyFileName = "secret.key"

// LoadOrCreateKey reads <dataDir>/secret.key, creating it with a new random
// key (readable only by the server's user) if it doesn't exist, and returns
// a Box using it.
//
// It refuses a key file that other users can read: anyone who can read the
// key can decrypt every stored secret.
func LoadOrCreateKey(dataDir string) (*Box, error) {
	key, err := keyfile.LoadOrCreate(filepath.Join(dataDir, KeyFileName), "secret key", newKey)
	if err != nil {
		return nil, err
	}
	return NewBox(key)
}

func newKey() []byte {
	key := make([]byte, KeySize)
	// crypto/rand.Read never returns an error: Go stops the program if the
	// system's random source fails.
	_, _ = rand.Read(key)
	return key
}

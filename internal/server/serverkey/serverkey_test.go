// SPDX-License-Identifier: Apache-2.0

package serverkey_test

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/serverkey"
)

func TestLoadOrCreate(t *testing.T) {
	t.Run("creates a key on first start and keeps it across restarts", func(t *testing.T) {
		dataDir := t.TempDir()
		first, err := serverkey.LoadOrCreate(dataDir)
		if err != nil {
			t.Fatal(err)
		}
		second, err := serverkey.LoadOrCreate(dataDir)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first.Public(), second.Public()) {
			t.Error("the key changed between starts; every agent would refuse the server")
		}
		if _, err := os.Stat(filepath.Join(dataDir, serverkey.FileName)); err != nil {
			t.Errorf("no key file: %v", err)
		}
	})

	t.Run("signs with the private half of its public key", func(t *testing.T) {
		key, err := serverkey.LoadOrCreate(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		signature := ed25519.Sign(key.Private(), []byte("message"))
		if !ed25519.Verify(key.Public(), []byte("message"), signature) {
			t.Error("signature doesn't verify with the public key")
		}
	})

	t.Run("refuses a key file of the wrong size", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dataDir, serverkey.FileName), []byte("short"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := serverkey.LoadOrCreate(dataDir)
		if err == nil || !strings.Contains(err.Error(), "5 bytes") {
			t.Errorf("err = %v, want a size error", err)
		}
	})

	t.Run("reports a data directory it can't use", func(t *testing.T) {
		if _, err := serverkey.LoadOrCreate(filepath.Join(t.TempDir(), "missing")); err == nil {
			t.Error("LoadOrCreate returned nil")
		}
	})
}

func TestFingerprint(t *testing.T) {
	t.Run("is the protocol fingerprint of the public key", func(t *testing.T) {
		_, private, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		key := serverkey.FromPrivateKey(private)
		if !protocol.FingerprintMatches(key.Fingerprint(), key.Public()) {
			t.Errorf("fingerprint %s doesn't match the public key", key.Fingerprint())
		}
	})
}

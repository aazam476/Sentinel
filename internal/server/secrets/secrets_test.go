// SPDX-License-Identifier: Apache-2.0

package secrets_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/server/secrets"
)

func TestSEC42_SecretsAreEncryptedWithAKeyOutsideTheDatabase(t *testing.T) {
	dataDir := t.TempDir()
	box, err := secrets.LoadOrCreateKey(dataDir)
	if err != nil {
		t.Fatalf("LoadOrCreateKey returned %v", err)
	}
	plaintext := []byte("JBSWY3DPEHPK3PXP")
	purpose := []byte("totp-secret:user:7")

	sealed, err := box.Encrypt(plaintext, purpose)
	if err != nil {
		t.Fatalf("Encrypt returned %v", err)
	}

	t.Run("does not contain the plaintext", func(t *testing.T) {
		if bytes.Contains(sealed, plaintext) {
			t.Error("ciphertext contains the plaintext")
		}
	})

	t.Run("decrypts back to the plaintext", func(t *testing.T) {
		got, err := box.Decrypt(sealed, purpose)
		if err != nil || !bytes.Equal(got, plaintext) {
			t.Errorf("Decrypt = %q, %v; want %q", got, err, plaintext)
		}
	})

	t.Run("stores the key in secret.key readable only by the server", func(t *testing.T) {
		info, err := os.Stat(filepath.Join(dataDir, "secret.key"))
		if err != nil {
			t.Fatalf("stat key: %v", err)
		}
		if info.Size() != secrets.KeySize {
			t.Errorf("key file is %d bytes, want %d", info.Size(), secrets.KeySize)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Errorf("key file mode = %v, want 0600", info.Mode().Perm())
		}
	})

	t.Run("reuses the same key after a restart", func(t *testing.T) {
		reopened, err := secrets.LoadOrCreateKey(dataDir)
		if err != nil {
			t.Fatalf("LoadOrCreateKey returned %v", err)
		}
		got, err := reopened.Decrypt(sealed, purpose)
		if err != nil || !bytes.Equal(got, plaintext) {
			t.Errorf("Decrypt after reload = %q, %v; want %q", got, err, plaintext)
		}
	})

	t.Run("refuses a ciphertext moved to another row or purpose", func(t *testing.T) {
		if _, err := box.Decrypt(sealed, []byte("totp-secret:user:8")); !errors.Is(err, secrets.ErrDecrypt) {
			t.Errorf("Decrypt with other associated data error = %v, want ErrDecrypt", err)
		}
	})

	t.Run("refuses a ciphertext from another key", func(t *testing.T) {
		other, err := secrets.LoadOrCreateKey(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := other.Decrypt(sealed, purpose); !errors.Is(err, secrets.ErrDecrypt) {
			t.Errorf("Decrypt with another key error = %v, want ErrDecrypt", err)
		}
	})
}

func TestEncrypt(t *testing.T) {
	t.Run("gives different ciphertexts for the same plaintext", func(t *testing.T) {
		box := newBox(t)
		first, _ := box.Encrypt([]byte("same"), nil)  // errors are covered by the round-trip tests
		second, _ := box.Encrypt([]byte("same"), nil) // errors are covered by the round-trip tests

		if bytes.Equal(first, second) {
			t.Error("two encryptions of the same plaintext are identical; the nonce is not random")
		}
	})

	t.Run("round-trips an empty value", func(t *testing.T) {
		box := newBox(t)
		sealed, err := box.Encrypt(nil, []byte("empty"))
		if err != nil {
			t.Fatal(err)
		}
		got, err := box.Decrypt(sealed, []byte("empty"))
		if err != nil || len(got) != 0 {
			t.Errorf("Decrypt = %q, %v; want empty", got, err)
		}
	})
}

func TestDecrypt(t *testing.T) {
	box := newBox(t)
	sealed, err := box.Encrypt([]byte("secret"), []byte("purpose"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		sealed []byte
	}{
		{name: "rejects an empty value", sealed: nil},
		{name: "rejects a value shorter than nonce plus tag", sealed: sealed[:10]},
		{name: "rejects an unknown format version", sealed: withByte(sealed, 0, 2)},
		{name: "rejects a changed nonce", sealed: withByte(sealed, 1, sealed[1]^0xff)},
		{name: "rejects a changed ciphertext", sealed: withByte(sealed, len(sealed)-1, sealed[len(sealed)-1]^0x01)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := box.Decrypt(test.sealed, []byte("purpose")); !errors.Is(err, secrets.ErrDecrypt) {
				t.Errorf("Decrypt error = %v, want ErrDecrypt", err)
			}
		})
	}
}

func TestNewBox(t *testing.T) {
	t.Run("rejects a key of the wrong length", func(t *testing.T) {
		if _, err := secrets.NewBox(make([]byte, 16)); err == nil {
			t.Error("NewBox accepted a 16-byte key")
		}
	})
}

func TestLoadOrCreateKey(t *testing.T) {
	t.Run("refuses a key file other users can read", func(t *testing.T) {
		dataDir := t.TempDir()
		path := filepath.Join(dataDir, "secret.key")
		if err := os.WriteFile(path, make([]byte, secrets.KeySize), 0o644); err != nil { //nolint:gosec // G306: the test needs a file with unsafe permissions
			t.Fatal(err)
		}

		_, err := secrets.LoadOrCreateKey(dataDir)

		// Windows has no Unix permission bits (the data directory's ACL
		// protects the key there), so the same file is accepted.
		if runtime.GOOS == "windows" {
			if err != nil {
				t.Errorf("error = %v, want nil on Windows", err)
			}
			return
		}
		if err == nil || !strings.Contains(err.Error(), "readable by other users") {
			t.Errorf("error = %v, want one about permissions", err)
		}
	})

	t.Run("returns an error when secret.key is a directory", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dataDir, "secret.key"), 0o700); err != nil {
			t.Fatal(err)
		}

		if _, err := secrets.LoadOrCreateKey(dataDir); err == nil {
			t.Error("LoadOrCreateKey returned nil when secret.key is a directory")
		}
	})

	t.Run("refuses a key file of the wrong length", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dataDir, "secret.key"), []byte("short"), 0o600); err != nil {
			t.Fatal(err)
		}

		if _, err := secrets.LoadOrCreateKey(dataDir); err == nil {
			t.Error("LoadOrCreateKey accepted a 5-byte key")
		}
	})

	t.Run("returns an error when the data directory is missing", func(t *testing.T) {
		if _, err := secrets.LoadOrCreateKey(filepath.Join(t.TempDir(), "missing")); err == nil {
			t.Error("LoadOrCreateKey returned nil for a missing directory")
		}
	})
}

func FuzzDecrypt(f *testing.F) {
	box, err := secrets.NewBox(make([]byte, secrets.KeySize))
	if err != nil {
		f.Fatal(err)
	}
	sealed, err := box.Encrypt([]byte("seed"), []byte("purpose"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(sealed, []byte("purpose"))
	f.Add([]byte{1}, []byte(nil))

	f.Fuzz(func(t *testing.T, input, associatedData []byte) {
		plaintext, err := box.Decrypt(input, associatedData)
		if err != nil && !errors.Is(err, secrets.ErrDecrypt) {
			t.Fatalf("Decrypt returned an unexpected error type: %v", err)
		}
		// Each fuzz worker process encrypts its own copy of the seed with a
		// different random nonce, so the only thing to compare against is
		// what the seed contains. A forgery would decrypt to anything else.
		if err == nil && (string(plaintext) != "seed" || string(associatedData) != "purpose") {
			t.Fatalf("Decrypt accepted a forged ciphertext and returned %q", plaintext)
		}
	})
}

func newBox(t *testing.T) *secrets.Box {
	t.Helper()
	box, err := secrets.LoadOrCreateKey(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func withByte(original []byte, index int, value byte) []byte {
	changed := bytes.Clone(original)
	changed[index] = value
	return changed
}

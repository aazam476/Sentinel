// SPDX-License-Identifier: Apache-2.0

package servertls

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNeedsRenewal(t *testing.T) {
	t.Run("renews a certificate without a parsed leaf", func(t *testing.T) {
		if !needsRenewal(tls.Certificate{}, SelfSignedOptions{}) {
			t.Error("needsRenewal returned false for a certificate without a leaf")
		}
	})
}

func TestWriteKeyPair(t *testing.T) {
	tests := []struct {
		name              string
		occupyKeyPath     bool
		occupyCertificate bool
	}{
		{name: "returns an error when the key path is a directory", occupyKeyPath: true},
		{name: "returns an error when the certificate path is a directory", occupyCertificate: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			certificatePath := filepath.Join(directory, "cert.pem")
			privateKeyPath := filepath.Join(directory, "key.pem")
			if test.occupyKeyPath {
				mustMkdir(t, privateKeyPath)
			}
			if test.occupyCertificate {
				mustMkdir(t, certificatePath)
			}

			err := writeKeyPair(certificatePath, []byte("cert"), privateKeyPath, []byte("key"))

			if err == nil {
				t.Error("writeKeyPair returned nil, want an error")
			}
		})
	}
}

func TestCertificateTemplate(t *testing.T) {
	t.Run("lists a repeated host name only once", func(t *testing.T) {
		template, err := certificateTemplate(SelfSignedOptions{
			Hosts: []string{"localhost", "localhost", "127.0.0.1"},
			Clock: fixedClock{},
		})
		if err != nil {
			t.Fatalf("certificateTemplate returned %v", err)
		}
		if len(template.DNSNames) != 1 || len(template.IPAddresses) != 1 {
			t.Errorf("DNSNames = %v, IPAddresses = %v; want one of each", template.DNSNames, template.IPAddresses)
		}
	})
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

// fixedClock always returns the zero time; certificateTemplate only needs a
// value, not a particular one.
type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Time{} }

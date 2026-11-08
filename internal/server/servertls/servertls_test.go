// SPDX-License-Identifier: Apache-2.0

package servertls_test

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/servertls"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

// startTime is when every test's fake clock starts.
func startTime() time.Time {
	return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
}

func TestSEC10_SelfSignedCertificateIsCreatedOnFirstStart(t *testing.T) {
	dataDir := t.TempDir()
	clock := fakeclock.New(startTime())

	config, err := servertls.SelfSigned(servertls.SelfSignedOptions{
		DataDir: dataDir,
		Hosts:   []string{"localhost", "127.0.0.1", "::1", "sentinel.lan"},
		Clock:   clock,
	})
	if err != nil {
		t.Fatalf("SelfSigned returned %v", err)
	}

	t.Run("allows only TLS 1.2 and newer", func(t *testing.T) {
		if config.MinVersion != tls.VersionTLS12 {
			t.Errorf("MinVersion = %x, want TLS 1.2", config.MinVersion)
		}
	})

	t.Run("covers every requested host", func(t *testing.T) {
		leaf := leafOf(t, config)
		for _, host := range []string{"localhost", "127.0.0.1", "::1", "sentinel.lan"} {
			if err := leaf.VerifyHostname(host); err != nil {
				t.Errorf("certificate does not cover %s: %v", host, err)
			}
		}
	})

	t.Run("is valid from now for a year", func(t *testing.T) {
		leaf := leafOf(t, config)
		if leaf.NotBefore.After(startTime()) || leaf.NotAfter.Before(startTime().Add(364*24*time.Hour)) {
			t.Errorf("validity = %v to %v, want it to include now and the next year", leaf.NotBefore, leaf.NotAfter)
		}
	})

	t.Run("stores the private key readable only by the server", func(t *testing.T) {
		info, err := os.Stat(filepath.Join(dataDir, "tls", "self-signed.key"))
		if err != nil {
			t.Fatalf("stat key: %v", err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Errorf("key file mode = %v, want 0600", info.Mode().Perm())
		}
	})
}

func TestSelfSignedRenewal(t *testing.T) {
	tests := []struct {
		name        string
		advance     time.Duration
		secondHosts []string
		wantRenewed bool
	}{
		{name: "reuses a certificate that is still valid", advance: 24 * time.Hour, secondHosts: []string{"localhost"}, wantRenewed: false},
		{name: "renews a certificate that expires within 30 days", advance: 340 * 24 * time.Hour, secondHosts: []string{"localhost"}, wantRenewed: true},
		{name: "renews a certificate that does not cover a new host", advance: 0, secondHosts: []string{"localhost", "new.example.com"}, wantRenewed: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dataDir := t.TempDir()
			clock := fakeclock.New(startTime())
			first := mustSelfSigned(t, dataDir, []string{"localhost"}, clock)

			clock.Advance(test.advance)
			second := mustSelfSigned(t, dataDir, test.secondHosts, clock)

			renewed := leafOf(t, first).SerialNumber.Cmp(leafOf(t, second).SerialNumber) != 0
			if renewed != test.wantRenewed {
				t.Errorf("renewed = %v, want %v", renewed, test.wantRenewed)
			}
			// Agents pin the public key, so it survives renewal.
			if servertls.PublicKeyFingerprint(leafOf(t, first)) != servertls.PublicKeyFingerprint(leafOf(t, second)) {
				t.Error("the certificate's key changed; every agent would refuse the server")
			}
		})
	}

	t.Run("makes a new key when the old one can't be read", func(t *testing.T) {
		dataDir := t.TempDir()
		clock := fakeclock.New(startTime())
		first := mustSelfSigned(t, dataDir, []string{"localhost"}, clock)
		if err := os.WriteFile(filepath.Join(dataDir, "tls", "self-signed.key"), []byte("garbage"), 0o600); err != nil {
			t.Fatal(err)
		}
		second := mustSelfSigned(t, dataDir, []string{"localhost"}, clock)
		if servertls.PublicKeyFingerprint(leafOf(t, first)) == servertls.PublicKeyFingerprint(leafOf(t, second)) {
			t.Error("kept a key it couldn't have read")
		}
	})
}

func TestPublicKeyFingerprint(t *testing.T) {
	t.Run("is SHA256: and 43 base64url characters", func(t *testing.T) {
		fingerprint := servertls.PublicKeyFingerprint(leafOf(t, mustSelfSigned(t, t.TempDir(), []string{"localhost"}, fakeclock.New(startTime()))))
		if !strings.HasPrefix(fingerprint, "SHA256:") || len(fingerprint) != len("SHA256:")+43 {
			t.Errorf("fingerprint = %q", fingerprint)
		}
	})
}

func TestSelfSignedErrors(t *testing.T) {
	t.Run("returns an error when the data directory can't be written", func(t *testing.T) {
		notADirectory := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(notADirectory, nil, 0o600); err != nil {
			t.Fatal(err)
		}

		_, err := servertls.SelfSigned(servertls.SelfSignedOptions{
			DataDir: notADirectory,
			Hosts:   []string{"localhost"},
			Clock:   fakeclock.New(startTime()),
		})

		if err == nil {
			t.Error("SelfSigned returned nil, want an error")
		}
	})

	t.Run("replaces a corrupt certificate file", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dataDir, "tls"), 0o700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"self-signed.crt", "self-signed.key"} {
			if err := os.WriteFile(filepath.Join(dataDir, "tls", name), []byte("garbage"), 0o600); err != nil {
				t.Fatal(err)
			}
		}

		config := mustSelfSigned(t, dataDir, []string{"localhost"}, fakeclock.New(startTime()))

		if err := leafOf(t, config).VerifyHostname("localhost"); err != nil {
			t.Errorf("replacement certificate does not cover localhost: %v", err)
		}
	})
}

func TestACME(t *testing.T) {
	config := servertls.ACME(servertls.ACMEOptions{
		DataDir:  t.TempDir(),
		HostName: "sentinel.example.com",
		Email:    "admin@example.com",
	})

	t.Run("allows only TLS 1.2 and newer", func(t *testing.T) {
		if config.MinVersion != tls.VersionTLS12 {
			t.Errorf("MinVersion = %x, want TLS 1.2", config.MinVersion)
		}
	})

	t.Run("obtains certificates on demand", func(t *testing.T) {
		if config.GetCertificate == nil {
			t.Error("GetCertificate is nil")
		}
	})

	t.Run("answers the TLS-ALPN-01 challenge", func(t *testing.T) {
		if !slices.Contains(config.NextProtos, "acme-tls/1") {
			t.Errorf("NextProtos = %v, want it to include acme-tls/1", config.NextProtos)
		}
	})

	t.Run("refuses certificates for other host names", func(t *testing.T) {
		_, err := config.GetCertificate(&tls.ClientHelloInfo{ServerName: "attacker.example.com"})
		if err == nil {
			t.Error("GetCertificate for an unlisted host returned nil error")
		}
	})
}

func mustSelfSigned(t *testing.T, dataDir string, hosts []string, clock *fakeclock.Clock) *tls.Config {
	t.Helper()
	config, err := servertls.SelfSigned(servertls.SelfSignedOptions{DataDir: dataDir, Hosts: hosts, Clock: clock})
	if err != nil {
		t.Fatalf("SelfSigned returned %v", err)
	}
	return config
}

func leafOf(t *testing.T, config *tls.Config) *x509.Certificate {
	t.Helper()
	if len(config.Certificates) != 1 || config.Certificates[0].Leaf == nil {
		t.Fatalf("config has %d certificates, want 1 with a parsed leaf", len(config.Certificates))
	}
	return config.Certificates[0].Leaf
}

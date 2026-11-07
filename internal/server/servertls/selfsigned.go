// SPDX-License-Identifier: Apache-2.0

package servertls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Certificate lifetime and renewal. A self-signed certificate isn't checked
// against any authority, so a long lifetime costs nothing; renewing well
// before expiry means a long-running server never serves an expired one at
// startup.
const (
	selfSignedLifetime    = 365 * 24 * time.Hour
	selfSignedRenewBefore = 30 * 24 * time.Hour
)

// File names inside <data dir>/tls/.
const (
	certificateFileName = "self-signed.crt"
	privateKeyFileName  = "self-signed.key"
)

// Clock tells the current time.
type Clock interface {
	Now() time.Time
}

// SelfSignedOptions configures SelfSigned.
type SelfSignedOptions struct {
	// DataDir is the server's data directory; the certificate lives in its
	// tls/ subdirectory.
	DataDir string
	// Hosts are the DNS names and IP addresses the certificate must cover.
	Hosts []string
	// Clock decides whether the certificate needs renewing.
	Clock Clock
}

// SelfSigned returns a TLS configuration that serves a self-signed
// certificate. It reuses the certificate on disk when it is still valid for
// more than 30 days and covers every host in options.Hosts; otherwise it
// creates a new one.
func SelfSigned(options SelfSignedOptions) (*tls.Config, error) {
	directory := filepath.Join(options.DataDir, "tls")
	certificatePath := filepath.Join(directory, certificateFileName)
	privateKeyPath := filepath.Join(directory, privateKeyFileName)

	certificate, err := tls.LoadX509KeyPair(certificatePath, privateKeyPath)
	if err != nil || needsRenewal(certificate, options) {
		certificate, err = createSelfSigned(options, certificatePath, privateKeyPath)
		if err != nil {
			return nil, err
		}
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{certificate},
	}, nil
}

// needsRenewal reports whether certificate expires within the renewal window
// or doesn't cover one of the requested hosts.
func needsRenewal(certificate tls.Certificate, options SelfSignedOptions) bool {
	leaf := certificate.Leaf
	if leaf == nil {
		return true
	}
	if options.Clock.Now().Add(selfSignedRenewBefore).After(leaf.NotAfter) {
		return true
	}
	for _, host := range options.Hosts {
		if leaf.VerifyHostname(host) != nil {
			return true
		}
	}
	return false
}

// createSelfSigned generates a new key and certificate, writes both to disk
// readable only by the server's user, and returns them.
func createSelfSigned(options SelfSignedOptions, certificatePath, privateKeyPath string) (tls.Certificate, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate TLS key: %w", err)
	}

	template, err := certificateTemplate(options)
	if err != nil {
		return tls.Certificate{}, err
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create self-signed certificate: %w", err)
	}
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("encode TLS key: %w", err)
	}

	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER})
	if err := writeKeyPair(certificatePath, certificatePEM, privateKeyPath, privateKeyPEM); err != nil {
		return tls.Certificate{}, err
	}

	certificate, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("load new self-signed certificate: %w", err)
	}
	return certificate, nil
}

func certificateTemplate(options SelfSignedOptions) (*x509.Certificate, error) {
	// 128 random bits, as recommended for certificate serial numbers.
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate certificate serial number: %w", err)
	}

	now := options.Clock.Now()
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      pkix.Name{Organization: []string{"Sentinel"}, CommonName: "Sentinel self-signed"},
		// Start an hour early so a client whose clock is slightly behind
		// still accepts the certificate.
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(selfSignedLifetime),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	for _, host := range options.Hosts {
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else if !slices.Contains(template.DNSNames, host) {
			template.DNSNames = append(template.DNSNames, host)
		}
	}
	return template, nil
}

func writeKeyPair(certificatePath string, certificatePEM []byte, privateKeyPath string, privateKeyPEM []byte) error {
	if err := os.MkdirAll(filepath.Dir(certificatePath), 0o700); err != nil {
		return fmt.Errorf("create TLS directory: %w", err)
	}
	if err := os.WriteFile(privateKeyPath, privateKeyPEM, 0o600); err != nil {
		return fmt.Errorf("write TLS key: %w", err)
	}
	if err := os.WriteFile(certificatePath, certificatePEM, 0o600); err != nil {
		return fmt.Errorf("write TLS certificate: %w", err)
	}
	return nil
}

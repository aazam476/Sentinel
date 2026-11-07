// SPDX-License-Identifier: Apache-2.0

package servertls

import (
	"crypto/tls"
	"path/filepath"

	"golang.org/x/crypto/acme/autocert"
)

// ACMEOptions configures ACME.
type ACMEOptions struct {
	// DataDir is the server's data directory; certificates are cached in its
	// tls/acme/ subdirectory so restarts don't request new ones.
	DataDir string
	// HostName is the public host name to obtain a certificate for.
	HostName string
	// Email is the optional contact address for the certificate authority.
	Email string
}

// ACME returns a TLS configuration that obtains certificates from Let's
// Encrypt for options.HostName and renews them automatically.
//
// Let's Encrypt proves domain ownership with the TLS-ALPN-01 challenge, which
// connects to port 443 of the host name. The server must therefore be
// reachable on port 443 from the internet, either by listening there or by a
// port forward to its listen port.
// https://letsencrypt.org/docs/challenge-types/#tls-alpn-01
func ACME(options ACMEOptions) *tls.Config {
	manager := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(filepath.Join(options.DataDir, "tls", "acme")),
		HostPolicy: autocert.HostWhitelist(options.HostName),
		Email:      options.Email,
	}

	config := manager.TLSConfig()
	config.MinVersion = tls.VersionTLS12
	return config
}

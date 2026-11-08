// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"path/filepath"
)

// Defaults for settings that the configuration file leaves out.
const (
	DefaultListen  = ":7443"
	DefaultDataDir = "/var/lib/sentinel"
)

// TLSMode says how the server provides HTTPS.
type TLSMode string

// The supported TLS modes.
const (
	// TLSSelfSigned generates and serves a self-signed certificate.
	TLSSelfSigned TLSMode = "self-signed"
	// TLSACME obtains a certificate from Let's Encrypt (or another ACME
	// certificate authority) for the public URL's host name.
	TLSACME TLSMode = "acme"
	// TLSOff serves plain HTTP. Only allowed on loopback or behind a
	// TLS-terminating proxy.
	TLSOff TLSMode = "off"
)

// Config is the validated server configuration.
type Config struct {
	// ListenAddress is the host:port to listen on.
	ListenAddress string
	// DataDir is an absolute path.
	DataDir string
	// PublicURL is the external origin (scheme, host and port only), or nil
	// when not configured.
	PublicURL *url.URL
	// BasePath starts with "/" and has no trailing slash, except that the
	// root is "/".
	BasePath string
	// TrustedProxies are the networks whose X-Forwarded-* headers count.
	TrustedProxies []netip.Prefix
	// TLS holds the HTTPS settings.
	TLS TLS
	// MaxFileBytes is the largest file the file manager moves (§9.7).
	MaxFileBytes int64
}

// DefaultMaxFileGB is the file manager's size limit when none is set.
const DefaultMaxFileGB = 10

// TLS is the validated [tls] table.
type TLS struct {
	Mode           TLSMode
	ACMEEmail      string
	BehindTLSProxy bool
}

// Resolve fills in defaults, validates every setting and returns the
// resulting Config.
func (file File) Resolve() (Config, error) {
	config := Config{
		ListenAddress: valueOrDefault(file.Listen, DefaultListen),
		TLS: TLS{
			Mode:           TLSMode(valueOrDefault(file.TLS.Mode, string(TLSSelfSigned))),
			ACMEEmail:      file.TLS.ACMEEmail,
			BehindTLSProxy: file.TLS.BehindTLSProxy,
		},
	}

	dataDir, err := filepath.Abs(valueOrDefault(file.DataDir, DefaultDataDir))
	if err != nil {
		return Config{}, fmt.Errorf("data_dir: %w", err)
	}
	config.DataDir = dataDir

	if config.PublicURL, err = parsePublicURL(file.PublicURL); err != nil {
		return Config{}, err
	}
	if config.BasePath, err = normalizeBasePath(file.BasePath); err != nil {
		return Config{}, err
	}
	if config.TrustedProxies, err = parseTrustedProxies(file.TrustedProxies); err != nil {
		return Config{}, err
	}
	if err := validateTransport(config); err != nil {
		return Config{}, err
	}
	if config.MaxFileBytes, err = maxFileBytes(file.Files.MaxSizeGB); err != nil {
		return Config{}, err
	}

	return config, nil
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// maxFileBytes checks files.max_size_gb, from 1 to 1,024 gigabytes.
func maxFileBytes(gigabytes int64) (int64, error) {
	switch {
	case gigabytes == 0:
		return DefaultMaxFileGB << 30, nil
	case gigabytes < 0 || gigabytes > 1024:
		return 0, fmt.Errorf("files.max_size_gb: %d isn't between 1 and 1024", gigabytes)
	}
	return gigabytes << 30, nil
}

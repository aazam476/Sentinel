// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
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
	// ReleaseURL is where agent builds are fetched from; "" when fetching
	// is off.
	ReleaseURL string
	// TURN is the remote desktop relay.
	TURN TURN
}

// TURN is the validated [turn] table.
type TURN struct {
	// Listen is the relay's UDP address; "" when the relay is off.
	Listen string
	// RelayIP is the address it is reached at; invalid means find it.
	RelayIP netip.Addr
	// FirstPort and LastPort bound the UDP ports relayed connections use.
	FirstPort, LastPort uint16
}

// DefaultTURNListen is where the relay listens unless the config says.
const DefaultTURNListen = ":3478"

// DefaultRelayPorts are the relay's ports for relayed connections: 100,
// for about 50 sessions relayed at both ends.
const DefaultRelayPorts = "49152-49251"

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
	if config.ReleaseURL, err = releaseURL(file.Updates.ReleaseURL); err != nil {
		return Config{}, err
	}
	if config.TURN, err = turnSettings(file.TURN); err != nil {
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

// DefaultReleaseURL is where agent builds come from unless the config says
// otherwise.
const DefaultReleaseURL = "https://github.com/aazam476/sentinel/releases/latest/download/"

// releaseURL checks updates.release_url.
func releaseURL(value string) (string, error) {
	switch value {
	case "":
		return DefaultReleaseURL, nil
	case "off":
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return "", fmt.Errorf("updates.release_url: %q isn't an http or https address", value)
	}
	return value, nil
}

// turnSettings checks the [turn] table.
func turnSettings(file TURNFile) (TURN, error) {
	var settings TURN
	var err error
	switch file.Listen {
	case "":
		settings.Listen = DefaultTURNListen
	case "off":
		return TURN{}, nil
	default:
		if _, port, err := net.SplitHostPort(file.Listen); err != nil || port == "" {
			return TURN{}, fmt.Errorf("turn.listen: %q isn't host:port", file.Listen)
		}
		settings.Listen = file.Listen
	}
	if settings.FirstPort, settings.LastPort, err = portRange(valueOrDefault(file.RelayPorts, DefaultRelayPorts)); err != nil {
		return TURN{}, err
	}
	if file.RelayIP != "" {
		address, err := netip.ParseAddr(file.RelayIP)
		if err != nil || !address.Is4() || address.IsUnspecified() {
			return TURN{}, fmt.Errorf("turn.relay_ip: %q isn't an IPv4 address", file.RelayIP)
		}
		settings.RelayIP = address
	}
	return settings, nil
}

// portRange reads turn.relay_ports, "first-last".
func portRange(value string) (uint16, uint16, error) {
	first, last, ok := strings.Cut(value, "-")
	low, lowErr := strconv.ParseUint(first, 10, 16)
	high, highErr := strconv.ParseUint(last, 10, 16)
	if !ok || lowErr != nil || highErr != nil || low < 1024 || low > high {
		return 0, 0, fmt.Errorf("turn.relay_ports: %q isn't a range of ports such as \"49152-49251\"", value)
	}
	return uint16(low), uint16(high), nil
}

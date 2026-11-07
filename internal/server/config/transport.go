// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// validateTransport enforces the rules for how the server may be reached
// (SEC-10):
//
//   - TLS is on unless mode is "off".
//   - Plain HTTP is only allowed on a loopback address, or anywhere when
//     behind_tls_proxy says a reverse proxy in front handles HTTPS.
//   - ACME needs a public https URL with a real host name to request a
//     certificate for.
func validateTransport(config Config) error {
	host, err := listenHost(config.ListenAddress)
	if err != nil {
		return err
	}

	switch config.TLS.Mode {
	case TLSSelfSigned:
	case TLSACME:
		if err := validateACME(config); err != nil {
			return err
		}
	case TLSOff:
		if !config.TLS.BehindTLSProxy && !isLoopbackHost(host) {
			return fmt.Errorf("tls.mode is \"off\" but listen address %q isn't loopback: "+
				"plain HTTP is only allowed on 127.0.0.1, ::1 or localhost, "+
				"or with tls.behind_tls_proxy = true when a reverse proxy provides HTTPS", config.ListenAddress)
		}
	default:
		return fmt.Errorf("tls.mode %q is not one of \"self-signed\", \"acme\" or \"off\"", config.TLS.Mode)
	}

	if config.TLS.BehindTLSProxy && config.TLS.Mode != TLSOff {
		return errors.New("tls.behind_tls_proxy = true requires tls.mode = \"off\": " +
			"the proxy provides HTTPS, so Sentinel must serve plain HTTP")
	}
	return nil
}

// listenHost checks that address is host:port and returns the host.
func listenHost(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("listen address %q: %w", address, err)
	}
	if port == "" {
		return "", fmt.Errorf("listen address %q has no port", address)
	}
	return host, nil
}

// isLoopbackHost reports whether host only accepts connections from this
// machine. An empty host means every interface, so it isn't loopback.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

func validateACME(config Config) error {
	if config.PublicURL == nil {
		return errors.New("tls.mode \"acme\" needs public_url, the address to request a certificate for")
	}
	if config.PublicURL.Scheme != "https" {
		return errors.New("tls.mode \"acme\" needs public_url to start with https://")
	}
	if _, err := netip.ParseAddr(config.PublicURL.Hostname()); err == nil {
		return errors.New("tls.mode \"acme\" needs public_url to use a host name; certificate authorities don't issue certificates for IP addresses")
	}
	return nil
}

// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
)

// basePathPattern allows letters, digits and a few separators in each path
// segment. Anything fancier (spaces, percent-encoding) only causes trouble in
// proxies and redirects. A compiled pattern is never modified after this
// line, so it isn't mutable state.
var basePathPattern = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)+$`)

// parsePublicURL accepts an absolute http or https URL with nothing after
// the host and port, and returns it, or nil when value is empty.
func parsePublicURL(value string) (*url.URL, error) {
	if value == "" {
		return nil, nil
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("public_url %q: %w", value, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("public_url %q must start with https:// or http://", value)
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return nil, fmt.Errorf("public_url %q has no host", value)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("public_url %q must not contain a user name, query or fragment", value)
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return nil, fmt.Errorf("public_url %q must not contain a path; set base_path instead", value)
	}

	return &url.URL{Scheme: parsed.Scheme, Host: strings.ToLower(parsed.Host)}, nil
}

// normalizeBasePath turns "", "/" into "/" and "sentinel/" or "/sentinel/"
// into "/sentinel".
func normalizeBasePath(value string) (string, error) {
	trimmed := strings.Trim(value, "/")
	if trimmed == "" {
		return "/", nil
	}

	candidate := "/" + trimmed
	if !basePathPattern.MatchString(candidate) {
		return "", fmt.Errorf("base_path %q may only contain letters, digits, '.', '_', '~', '-' and '/' between segments", value)
	}
	for _, segment := range strings.Split(trimmed, "/") {
		if segment == "." || segment == ".." {
			return "", fmt.Errorf("base_path %q must not contain . or .. segments", value)
		}
	}
	return candidate, nil
}

// parseTrustedProxies accepts IP addresses ("10.0.0.5") and CIDR ranges
// ("10.0.0.0/8").
func parseTrustedProxies(values []string) ([]netip.Prefix, error) {
	return parseNetworks("trusted_proxies", values)
}

// parseNetworks reads a list of IP addresses and CIDR ranges.
func parseNetworks(field string, values []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		if strings.Contains(value, "/") {
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				return nil, fmt.Errorf("%s entry %q: %w", field, value, err)
			}
			prefixes = append(prefixes, prefix.Masked())
			continue
		}

		address, err := netip.ParseAddr(value)
		if err != nil {
			return nil, fmt.Errorf("%s entry %q is neither an IP address nor a CIDR range: %w", field, value, err)
		}
		prefixes = append(prefixes, netip.PrefixFrom(address.Unmap(), address.Unmap().BitLen()))
	}
	return prefixes, nil
}

// DefaultSetupNetworks are where first-time setup may be done from unless
// server.toml says otherwise: this machine, private networks (including
// the carrier-grade NAT range VPNs such as Tailscale use) and link-local
// addresses. A server reachable from the internet can't be claimed from it.
func DefaultSetupNetworks() []netip.Prefix {
	var prefixes []netip.Prefix
	for _, value := range []string{"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
		"100.64.0.0/10", "169.254.0.0/16", "fc00::/7", "fe80::/10"} {
		prefixes = append(prefixes, netip.MustParsePrefix(value))
	}
	return prefixes
}

// setupNetworks reads setup_allowed_from, or gives the defaults.
func setupNetworks(values []string) ([]netip.Prefix, error) {
	if len(values) == 0 {
		return DefaultSetupNetworks(), nil
	}
	return parseNetworks("setup_allowed_from", values)
}

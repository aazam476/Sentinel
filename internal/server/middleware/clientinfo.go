// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ClientInfo describes who sent a request, after taking trusted proxies into
// account.
type ClientInfo struct {
	// IP is the client's address. Behind a trusted proxy this comes from
	// X-Forwarded-For; otherwise it is the address of the TCP connection.
	IP netip.Addr
	// IsHTTPS reports whether the client used HTTPS to reach the server or
	// the trusted proxy in front of it.
	IsHTTPS bool
	// Host is the host (and port) the client asked for.
	Host string
}

// clientInfoKey is the context key for ClientInfo. A private type means no
// other package can read or overwrite the value by accident.
type clientInfoKey struct{}

// ClientInfoFrom returns the ClientInfo that WithClientInfo stored in ctx,
// or a zero ClientInfo if there is none.
func ClientInfoFrom(ctx context.Context) ClientInfo {
	info, _ := ctx.Value(clientInfoKey{}).(ClientInfo) // missing means zero value, by design
	return info
}

// WithClientInfo works out each request's ClientInfo and stores it in the
// request context.
//
// X-Forwarded-For, X-Forwarded-Proto and X-Forwarded-Host are only believed
// when the TCP connection comes from one of trustedProxies (SEC-14). Anyone
// can send those headers, so believing them from an untrusted client would
// let it fake its IP address, dodge rate limits and forge audit entries.
func WithClientInfo(next http.Handler, trustedProxies []netip.Prefix) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := clientInfoFor(r, trustedProxies)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientInfoKey{}, info)))
	})
}

func clientInfoFor(r *http.Request, trustedProxies []netip.Prefix) ClientInfo {
	info := ClientInfo{
		IP:      remoteIP(r.RemoteAddr),
		IsHTTPS: r.TLS != nil,
		Host:    r.Host,
	}
	if !isTrusted(info.IP, trustedProxies) {
		return info
	}

	info.IP = forwardedClientIP(r.Header.Values("X-Forwarded-For"), info.IP, trustedProxies)
	if proto := firstListValue(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		info.IsHTTPS = strings.EqualFold(proto, "https")
	}
	if host := firstListValue(r.Header.Get("X-Forwarded-Host")); host != "" {
		info.Host = host
	}
	return info
}

// forwardedClientIP walks the X-Forwarded-For chain from right to left. Each
// trusted proxy appends the address it received the request from, so the
// first address that isn't a trusted proxy is the real client. Addresses to
// its left were supplied by the client and can't be believed.
func forwardedClientIP(headerValues []string, connectionIP netip.Addr, trustedProxies []netip.Prefix) netip.Addr {
	hops := strings.Split(strings.Join(headerValues, ","), ",")
	client := connectionIP
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			// A malformed entry means the chain can't be followed further;
			// the last trustworthy address is the best answer.
			return client
		}
		client = hop.Unmap()
		if !isTrusted(client, trustedProxies) {
			return client
		}
	}
	return client
}

func remoteIP(remoteAddr string) netip.Addr {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return ip.Unmap()
}

func isTrusted(ip netip.Addr, trustedProxies []netip.Prefix) bool {
	if !ip.IsValid() {
		return false
	}
	for _, prefix := range trustedProxies {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

// firstListValue returns the first entry of a comma-separated header value.
// The proxy closest to the client writes the first entry.
func firstListValue(value string) string {
	first, _, _ := strings.Cut(value, ",")
	return strings.TrimSpace(first)
}

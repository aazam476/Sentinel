// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
)

// AgentPath is the server's agent endpoint, under its base path.
const AgentPath = "/agent/connect"

// ErrWrongTLSKey means the server's TLS certificate doesn't carry the
// pinned key. It is never reassigned.
var ErrWrongTLSKey = errors.New("the server's TLS certificate doesn't match the pinned fingerprint")

// ConnectURL turns the server's URL (https://host/base) into the agent
// endpoint's WebSocket URL (wss://host/base/agent/connect).
func ConnectURL(serverURL string) (string, error) {
	parsed, err := url.Parse(serverURL)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("server URL %q isn't a URL", serverURL)
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	default:
		return "", fmt.Errorf("server URL %q must start with https:// (or http:// on a trusted network)", serverURL)
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + AgentPath
	parsed.RawQuery, parsed.Fragment = "", ""
	return parsed.String(), nil
}

// TLSConfig is how the agent checks the server's certificate. Without a pin
// it is checked like a browser would, against the system's trusted
// authorities. With one, only a certificate with that key is accepted,
// whoever signed it: that is how a self-signed server is trusted without
// turning checks off.
func TLSConfig(pin string) *tls.Config {
	if pin == "" {
		return &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		// The usual chain-of-trust check is replaced, not skipped:
		// VerifyConnection below accepts exactly one key. It runs on every
		// handshake, resumed sessions included, unlike VerifyPeerCertificate.
		InsecureSkipVerify: true, //nolint:gosec // G402: the certificate's key is pinned instead
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 || protocol.CertificateKeyFingerprint(state.PeerCertificates[0]) != pin {
				return ErrWrongTLSKey
			}
			return nil
		},
	}
}

// dial opens the WebSocket to the server.
func dial(ctx context.Context, serverURL string, tlsConfig *tls.Config) (*websocket.Conn, error) {
	endpoint, err := ConnectURL(serverURL)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig, Proxy: http.ProxyFromEnvironment}}
	conn, response, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPClient: client})
	if response != nil && response.Body != nil {
		_ = response.Body.Close() // nothing left to read
	}
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", endpoint, err)
	}
	conn.SetReadLimit(protocol.MaxMessageSize)
	return conn, nil
}

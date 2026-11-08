// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"errors"
	"fmt"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/agent/identity"
	"github.com/aazam476/sentinel/internal/protocol"
)

// Enrollment errors. They are never reassigned.
var (
	// ErrEnrollmentRefused: the server refused the token.
	ErrEnrollmentRefused = errors.New("the server refused the enrollment token: it is wrong, expired, revoked or used up")
	// ErrWrongServer: the server's key doesn't match the fingerprint in
	// the install command. Someone may be intercepting the connection.
	ErrWrongServer = errors.New("the server's key doesn't match the fingerprint in the install command; " +
		"the connection may be intercepted, so nothing was saved")
)

// EnrollOptions is what the install command passes to Enroll.
type EnrollOptions struct {
	ServerURL string
	Token     string
	// Fingerprint is the server key fingerprint from the install command.
	Fingerprint string
	// TLSFingerprint pins a self-signed server certificate; may be empty.
	TLSFingerprint string
	Key            ed25519.PrivateKey
	Hostname       string
	OS             string
	Arch           string
	AgentVersion   string
	// TLSConfig overrides the TLS settings built from TLSFingerprint.
	TLSConfig *tls.Config
}

// Enroll registers this agent with the server and returns the state to
// save. Nothing is returned unless the server's key matches the
// fingerprint.
func Enroll(ctx context.Context, options EnrollOptions) (identity.State, error) {
	tlsConfig := options.TLSConfig
	if tlsConfig == nil {
		tlsConfig = TLSConfig(options.TLSFingerprint)
	}
	conn, err := dial(ctx, options.ServerURL, tlsConfig)
	if err != nil {
		return identity.State{}, err
	}
	defer conn.CloseNow() //nolint:errcheck // the connection is done either way

	public, _ := options.Key.Public().(ed25519.PublicKey) // always an ed25519.PublicKey
	err = protocol.WriteMessage(ctx, conn, protocol.Message{Type: protocol.TypeEnroll}, protocol.EnrollPayload{
		Token: options.Token, PublicKey: public, Hostname: options.Hostname, OS: options.OS, Arch: options.Arch,
		AgentVersion: options.AgentVersion, ProtocolVersion: protocol.CurrentVersion,
	})
	if err != nil {
		return identity.State{}, err
	}
	var enrolled protocol.EnrolledPayload
	if err := protocol.ReadPayload(ctx, conn, protocol.TypeEnrolled, &enrolled); err != nil {
		return identity.State{}, explainRefusal(err)
	}
	if !protocol.FingerprintMatches(options.Fingerprint, enrolled.ServerPublicKey) {
		_ = conn.Close(websocket.StatusPolicyViolation, "server key doesn't match the fingerprint") // best effort
		return identity.State{}, ErrWrongServer
	}
	_ = conn.Close(websocket.StatusNormalClosure, "enrolled") // best effort; enrollment is done
	return identity.State{
		ServerURL: options.ServerURL, AgentID: enrolled.AgentID,
		ServerPublicKey: enrolled.ServerPublicKey, TLSFingerprint: options.TLSFingerprint,
	}, nil
}

// explainRefusal turns the server's close code into an error a person can
// act on.
func explainRefusal(err error) error {
	switch websocket.CloseStatus(err) {
	case protocol.CloseEnrollmentRefused:
		return ErrEnrollmentRefused
	case protocol.CloseUnsupportedVersion:
		return fmt.Errorf("the server doesn't support this agent version; install the agent the server offers: %w", err)
	default:
		return fmt.Errorf("enrollment failed: %w", err)
	}
}

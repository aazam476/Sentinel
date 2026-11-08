// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
)

// Protocol versions. The agent sends its version in hello; the server
// accepts the current and the previous one, so a fleet keeps working while
// agents update one by one.
const (
	CurrentVersion         = 1
	OldestSupportedVersion = 1
)

// NonceSize is the length of the random nonces in the handshake.
const NonceSize = 32

// domain is prepended to everything signed during the handshake, so a
// signature made here can never be passed off as one for another purpose.
const domain = "sentinel-handshake-v1"

// Who signs, also part of the signed bytes, so the server's signature can't
// be replayed as the agent's or the other way round.
const (
	signerServer = "server"
	signerAgent  = "agent"
)

// ErrBadSignature means a handshake signature didn't verify. It is never
// reassigned.
var ErrBadSignature = errors.New("handshake signature is invalid")

// EnrollPayload is the agent's first message on its very first connection.
type EnrollPayload struct {
	// Token is the enrollment token from the install command.
	Token string `json:"token"`
	// PublicKey is the agent's new Ed25519 public key.
	PublicKey []byte `json:"publicKey"`
	Hostname  string `json:"hostname"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	// AgentVersion is the agent's build version, e.g. "v0.1.0-beta.1".
	AgentVersion    string `json:"agentVersion"`
	ProtocolVersion int    `json:"protocolVersion"`
}

// EnrolledPayload is the server's answer to a successful enrollment.
type EnrolledPayload struct {
	AgentID         string `json:"agentId"`
	ServerPublicKey []byte `json:"serverPublicKey"`
}

// HelloPayload opens every connection after enrollment.
type HelloPayload struct {
	AgentID         string `json:"agentId"`
	AgentVersion    string `json:"agentVersion"`
	ProtocolVersion int    `json:"protocolVersion"`
	AgentNonce      []byte `json:"agentNonce"`
}

// ChallengePayload is the server's proof of identity and its nonce for the
// agent to sign.
type ChallengePayload struct {
	ServerNonce []byte `json:"serverNonce"`
	Signature   []byte `json:"signature"`
}

// ProvePayload is the agent's proof of identity.
type ProvePayload struct {
	Signature []byte `json:"signature"`
}

// WelcomePayload ends the handshake; both sides now trust the connection.
type WelcomePayload struct {
	// HeartbeatSeconds is how often the agent must ping.
	HeartbeatSeconds int `json:"heartbeatSeconds"`
	// MetricsIntervalSeconds is how often the agent sends a metrics sample.
	MetricsIntervalSeconds int `json:"metricsIntervalSeconds"`
}

// NewNonce returns NonceSize random bytes.
func NewNonce() []byte {
	nonce := make([]byte, NonceSize)
	// crypto/rand.Read never returns an error: if the system's random source
	// fails, Go stops the program rather than hand out predictable bytes.
	_, _ = rand.Read(nonce)
	return nonce
}

// signedBytes builds what one side signs: the domain, the signer's role, the
// two nonces in that side's order, and the agent ID, each prefixed with its
// length so no field can bleed into the next.
func signedBytes(signer string, first, second []byte, agentID string) []byte {
	var message []byte
	for _, part := range [][]byte{[]byte(domain), []byte(signer), first, second, []byte(agentID)} {
		message = binary.AppendUvarint(message, uint64(len(part)))
		message = append(message, part...)
	}
	return message
}

// SignServerChallenge is the server's signature over
// agentNonce || serverNonce || agentID (§7.3, step 2).
func SignServerChallenge(serverKey ed25519.PrivateKey, agentNonce, serverNonce []byte, agentID string) []byte {
	return ed25519.Sign(serverKey, signedBytes(signerServer, agentNonce, serverNonce, agentID))
}

// VerifyServerChallenge is the agent's check of that signature against the
// server key it saved at enrollment (§7.3, step 3).
func VerifyServerChallenge(serverPublicKey ed25519.PublicKey, agentNonce, serverNonce []byte, agentID string, signature []byte) error {
	return verify(serverPublicKey, signedBytes(signerServer, agentNonce, serverNonce, agentID), signature)
}

// SignAgentProof is the agent's signature over
// serverNonce || agentNonce || agentID (§7.3, step 4).
func SignAgentProof(agentKey ed25519.PrivateKey, serverNonce, agentNonce []byte, agentID string) []byte {
	return ed25519.Sign(agentKey, signedBytes(signerAgent, serverNonce, agentNonce, agentID))
}

// VerifyAgentProof is the server's check of the agent's signature against
// the agent's stored public key (§7.3, step 5).
func VerifyAgentProof(agentPublicKey ed25519.PublicKey, serverNonce, agentNonce []byte, agentID string, signature []byte) error {
	return verify(agentPublicKey, signedBytes(signerAgent, serverNonce, agentNonce, agentID), signature)
}

func verify(publicKey ed25519.PublicKey, message, signature []byte) error {
	if len(publicKey) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize {
		return ErrBadSignature
	}
	if !ed25519.Verify(publicKey, message, signature) {
		return ErrBadSignature
	}
	return nil
}

// fingerprintPrefix marks the hash algorithm, as SSH fingerprints do.
const fingerprintPrefix = "SHA256:"

// Fingerprint is a short, copyable identifier of a public key, printed in
// the install command so the agent can check it reached the right server
// even if someone else terminates TLS (§7.2).
func Fingerprint(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return fingerprintPrefix + base64.RawURLEncoding.EncodeToString(sum[:])
}

// FingerprintMatches compares a fingerprint typed or pasted by a person
// with a public key. Surrounding spaces are ignored.
func FingerprintMatches(fingerprint string, publicKey ed25519.PublicKey) bool {
	return strings.TrimSpace(fingerprint) == Fingerprint(publicKey)
}

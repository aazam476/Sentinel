// SPDX-License-Identifier: Apache-2.0

package protocol_test

import (
	"crypto/ed25519"
	"crypto/x509"
	"errors"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/protocol"
)

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return public, private
}

func TestSEC21_HandshakeSignaturesProveBothSides(t *testing.T) {
	serverPublic, serverPrivate := newKey(t)
	agentPublic, agentPrivate := newKey(t)
	_, impostor := newKey(t)
	agentNonce, serverNonce := protocol.NewNonce(), protocol.NewNonce()
	const agentID = exampleID

	serverSignature := protocol.SignServerChallenge(serverPrivate, agentNonce, serverNonce, agentID)
	agentSignature := protocol.SignAgentProof(agentPrivate, serverNonce, agentNonce, agentID)

	tests := []struct {
		name   string
		verify func() error
		valid  bool
	}{
		{name: "the agent accepts its server's signature", valid: true, verify: func() error {
			return protocol.VerifyServerChallenge(serverPublic, agentNonce, serverNonce, agentID, serverSignature)
		}},
		{name: "the server accepts its agent's signature", valid: true, verify: func() error {
			return protocol.VerifyAgentProof(agentPublic, serverNonce, agentNonce, agentID, agentSignature)
		}},
		{name: "the agent refuses an impostor server", verify: func() error {
			forged := protocol.SignServerChallenge(impostor, agentNonce, serverNonce, agentID)
			return protocol.VerifyServerChallenge(serverPublic, agentNonce, serverNonce, agentID, forged)
		}},
		{name: "the server refuses an impostor agent", verify: func() error {
			forged := protocol.SignAgentProof(impostor, serverNonce, agentNonce, agentID)
			return protocol.VerifyAgentProof(agentPublic, serverNonce, agentNonce, agentID, forged)
		}},
		{name: "a server signature replayed with a new agent nonce fails", verify: func() error {
			return protocol.VerifyServerChallenge(serverPublic, protocol.NewNonce(), serverNonce, agentID, serverSignature)
		}},
		{name: "an agent signature replayed with a new server nonce fails", verify: func() error {
			return protocol.VerifyAgentProof(agentPublic, protocol.NewNonce(), agentNonce, agentID, agentSignature)
		}},
		{name: "a signature for another agent ID fails", verify: func() error {
			return protocol.VerifyAgentProof(agentPublic, serverNonce, agentNonce, "01JBX3K5Q7Z8M4N6P8R0T2V4W7", agentSignature)
		}},
		{name: "the server's signature can't pass as the agent's", verify: func() error {
			// Same key on both sides, so only the role label tells them apart.
			sameKeySignature := protocol.SignServerChallenge(agentPrivate, serverNonce, agentNonce, agentID)
			return protocol.VerifyAgentProof(agentPublic, serverNonce, agentNonce, agentID, sameKeySignature)
		}},
		{name: "a truncated signature fails", verify: func() error {
			return protocol.VerifyAgentProof(agentPublic, serverNonce, agentNonce, agentID, agentSignature[:10])
		}},
		{name: "a malformed public key fails", verify: func() error {
			return protocol.VerifyAgentProof(agentPublic[:10], serverNonce, agentNonce, agentID, agentSignature)
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.verify()
			if test.valid && err != nil {
				t.Errorf("verify returned %v, want nil", err)
			}
			if !test.valid && !errors.Is(err, protocol.ErrBadSignature) {
				t.Errorf("verify returned %v, want ErrBadSignature", err)
			}
		})
	}
}

func TestNonces(t *testing.T) {
	first, second := protocol.NewNonce(), protocol.NewNonce()
	if len(first) != protocol.NonceSize || string(first) == string(second) {
		t.Errorf("nonces %x and %x are not 32 fresh random bytes", first, second)
	}
}

func TestFingerprint(t *testing.T) {
	public, _ := newKey(t)
	other, _ := newKey(t)
	fingerprint := protocol.Fingerprint(public)

	if !strings.HasPrefix(fingerprint, "SHA256:") || len(fingerprint) != len("SHA256:")+43 {
		t.Errorf("fingerprint %q isn't SHA256: plus 43 base64url characters", fingerprint)
	}
	if !protocol.FingerprintMatches("  "+fingerprint+"\n", public) {
		t.Error("a fingerprint with surrounding spaces didn't match its key")
	}
	if protocol.FingerprintMatches(fingerprint, other) {
		t.Error("a fingerprint matched a different key")
	}
}

func FuzzVerifyAgentProof(f *testing.F) {
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		f.Fatal(err)
	}
	nonce := make([]byte, protocol.NonceSize)
	f.Add(nonce, nonce, exampleID, protocol.SignAgentProof(private, nonce, nonce, exampleID))
	f.Fuzz(func(t *testing.T, serverNonce, agentNonce []byte, agentID string, signature []byte) {
		err := protocol.VerifyAgentProof(public, serverNonce, agentNonce, agentID, signature)
		if err != nil && !errors.Is(err, protocol.ErrBadSignature) {
			t.Fatalf("unexpected error type: %v", err)
		}
		// Only the real signature over these exact inputs may verify.
		if err == nil && string(protocol.SignAgentProof(private, serverNonce, agentNonce, agentID)) != string(signature) {
			t.Fatalf("a forged signature verified")
		}
	})
}

func TestCertificateKeyFingerprint(t *testing.T) {
	t.Run("hashes the certificate's public key info, not the whole certificate", func(t *testing.T) {
		first := &x509.Certificate{Raw: []byte("certificate 1"), RawSubjectPublicKeyInfo: []byte("same key")}
		renewed := &x509.Certificate{Raw: []byte("certificate 2"), RawSubjectPublicKeyInfo: []byte("same key")}
		other := &x509.Certificate{Raw: []byte("certificate 1"), RawSubjectPublicKeyInfo: []byte("other key")}
		if protocol.CertificateKeyFingerprint(first) != protocol.CertificateKeyFingerprint(renewed) {
			t.Error("a renewed certificate with the same key got a different fingerprint")
		}
		if protocol.CertificateKeyFingerprint(first) == protocol.CertificateKeyFingerprint(other) {
			t.Error("different keys got the same fingerprint")
		}
		if got := protocol.CertificateKeyFingerprint(first); !strings.HasPrefix(got, "SHA256:") {
			t.Errorf("fingerprint = %q", got)
		}
	})
}

// SPDX-License-Identifier: Apache-2.0

package softauthn

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
)

// Authenticator flags (WebAuthn §6.1): user present, user verified,
// attested credential data included.
const (
	flagUserPresent  = 0x01
	flagUserVerified = 0x04
	flagAttestedData = 0x40
)

// Authenticator holds one passkey.
type Authenticator struct {
	// Origin is what the browser would report, e.g. "https://127.0.0.1:7443".
	Origin       string
	key          *ecdsa.PrivateKey
	credentialID []byte
	userHandle   []byte
	signCount    uint32
}

// New returns an authenticator that answers as a browser at origin would.
func New(origin string) *Authenticator {
	// ecdsa.GenerateKey only fails if the random source fails, which Go
	// treats as fatal anyway.
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	credentialID := make([]byte, 16)
	_, _ = rand.Read(credentialID) // crypto/rand.Read never returns an error
	return &Authenticator{Origin: origin, key: key, credentialID: credentialID}
}

// creationOptions is the part of the server's creation options used here.
type creationOptions struct {
	PublicKey struct {
		Challenge string `json:"challenge"`
		RP        struct {
			ID string `json:"id"`
		} `json:"rp"`
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	} `json:"publicKey"`
}

// requestOptions is the part of the server's request options used here.
type requestOptions struct {
	PublicKey struct {
		Challenge string `json:"challenge"`
		RPID      string `json:"rpId"`
	} `json:"publicKey"`
}

// Register answers navigator.credentials.create() options with a new
// credential, returning the JSON a browser would send to the server.
func (a *Authenticator) Register(optionsJSON []byte) ([]byte, error) {
	var options creationOptions
	if err := json.Unmarshal(optionsJSON, &options); err != nil {
		return nil, fmt.Errorf("parse creation options: %w", err)
	}
	userHandle, err := base64.RawURLEncoding.DecodeString(options.PublicKey.User.ID)
	if err != nil {
		return nil, fmt.Errorf("decode user handle: %w", err)
	}
	a.userHandle = userHandle

	clientData := a.clientData("webauthn.create", options.PublicKey.Challenge)
	authenticatorData, err := a.authenticatorData(options.PublicKey.RP.ID, true)
	if err != nil {
		return nil, err
	}
	attestation, err := webauthncbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": authenticatorData})
	if err != nil {
		return nil, fmt.Errorf("encode attestation: %w", err)
	}

	return json.Marshal(map[string]any{
		"id":                      encode(a.credentialID),
		"rawId":                   encode(a.credentialID),
		"type":                    "public-key",
		"authenticatorAttachment": "platform",
		"clientExtensionResults":  map[string]any{},
		"response": map[string]any{
			"clientDataJSON":    encode(clientData),
			"attestationObject": encode(attestation),
			"transports":        []string{"internal"},
		},
	})
}

// Sign answers navigator.credentials.get() options, returning the JSON a
// browser would send to the server.
func (a *Authenticator) Sign(optionsJSON []byte) ([]byte, error) {
	var options requestOptions
	if err := json.Unmarshal(optionsJSON, &options); err != nil {
		return nil, fmt.Errorf("parse request options: %w", err)
	}

	clientData := a.clientData("webauthn.get", options.PublicKey.Challenge)
	authenticatorData, err := a.authenticatorData(options.PublicKey.RPID, false)
	if err != nil {
		return nil, err
	}
	clientDataHash := sha256.Sum256(clientData)
	signed := sha256.Sum256(append(append([]byte{}, authenticatorData...), clientDataHash[:]...))
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, signed[:])
	if err != nil {
		return nil, fmt.Errorf("sign assertion: %w", err)
	}

	return json.Marshal(map[string]any{
		"id":                     encode(a.credentialID),
		"rawId":                  encode(a.credentialID),
		"type":                   "public-key",
		"clientExtensionResults": map[string]any{},
		"response": map[string]any{
			"clientDataJSON":    encode(clientData),
			"authenticatorData": encode(authenticatorData),
			"signature":         encode(signature),
			"userHandle":        encode(a.userHandle),
		},
	})
}

func (a *Authenticator) clientData(ceremonyType, challenge string) []byte {
	// Marshaling a map of strings can't fail.
	data, _ := json.Marshal(map[string]any{"type": ceremonyType, "challenge": challenge, "origin": a.Origin, "crossOrigin": false})
	return data
}

// authenticatorData builds the authenticator data: the relying party ID's
// hash, flags, the signature counter and, when registering, the credential.
func (a *Authenticator) authenticatorData(rpID string, includeCredential bool) ([]byte, error) {
	a.signCount++
	rpIDHash := sha256.Sum256([]byte(rpID))
	data := append([]byte{}, rpIDHash[:]...)
	flags := byte(flagUserPresent | flagUserVerified)
	if includeCredential {
		flags |= flagAttestedData
	}
	data = append(data, flags)
	data = binary.BigEndian.AppendUint32(data, a.signCount)
	if !includeCredential {
		return data, nil
	}

	publicKey, err := webauthncbor.Marshal(webauthncose.EC2PublicKeyData{
		PublicKeyData: webauthncose.PublicKeyData{KeyType: int64(webauthncose.EllipticKey), Algorithm: int64(webauthncose.AlgES256)},
		Curve:         1, // P-256
		XCoord:        a.key.X.FillBytes(make([]byte, 32)),
		YCoord:        a.key.Y.FillBytes(make([]byte, 32)),
	})
	if err != nil {
		return nil, fmt.Errorf("encode public key: %w", err)
	}
	data = append(data, make([]byte, 16)...)                                // AAGUID: all zeros for "none" attestation
	data = binary.BigEndian.AppendUint16(data, uint16(len(a.credentialID))) //nolint:gosec // G115: the ID is 16 bytes
	data = append(data, a.credentialID...)
	return append(data, publicKey...), nil
}

func encode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

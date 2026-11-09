// SPDX-License-Identifier: Apache-2.0

package release

import (
	"crypto/ed25519"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

//go:embed keys/production.pub
var productionKeys string

//go:embed keys/development.pub
var developmentKeys string

// TrustedKeys returns the release keys this build trusts: the production
// keys, or the development key in a build that has none.
func TrustedKeys() []ed25519.PublicKey {
	return trustedKeys(productionKeys, developmentKeys)
}

// trustedKeys picks the production keys if there are any. A broken key file
// trusts nothing, so every update is refused: the tests catch it first.
func trustedKeys(production, development string) []ed25519.PublicKey {
	keys, err := ParsePublicKeys(production)
	if err != nil {
		return nil
	}
	if len(keys) > 0 {
		return keys
	}
	keys, err = ParsePublicKeys(development)
	if err != nil {
		return nil
	}
	return keys
}

// IsDevelopmentBuild says whether this build trusts only the development
// key, so it must never be published.
func IsDevelopmentBuild() bool {
	keys, err := ParsePublicKeys(productionKeys)
	return err == nil && len(keys) == 0
}

// ParsePublicKeys reads base64 Ed25519 public keys, one per line; blank
// lines and lines starting with # are skipped.
func ParsePublicKeys(text string) ([]ed25519.PublicKey, error) {
	var keys []ed25519.PublicKey
	for number, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(line)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("line %d isn't a base64 Ed25519 public key", number+1)
		}
		keys = append(keys, raw)
	}
	return keys, nil
}

// ParsePrivateKey reads a private key file: a base64 Ed25519 seed, after
// any comment lines.
func ParsePrivateKey(text string) (ed25519.PrivateKey, error) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		seed, err := base64.StdEncoding.DecodeString(line)
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, errors.New("the private key isn't a base64 Ed25519 seed")
		}
		return ed25519.NewKeyFromSeed(seed), nil
	}
	return nil, errors.New("the private key file is empty")
}

// FormatPrivateKey writes a private key the way ParsePrivateKey reads it.
func FormatPrivateKey(key ed25519.PrivateKey) string {
	return "# Sentinel release private key: keep it offline\n" + base64.StdEncoding.EncodeToString(key.Seed()) + "\n"
}

// FormatPublicKey writes a public key as one line of keys/production.pub.
func FormatPublicKey(key ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(key)
}

// BuildNote is what `version` adds about the release key: a warning for a
// build that trusts only the development key, else nothing.
func BuildNote() string {
	if IsDevelopmentBuild() {
		return " (development build: trusts only the development release key)"
	}
	return ""
}

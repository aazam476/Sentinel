// SPDX-License-Identifier: Apache-2.0

package release

import (
	"bufio"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// header starts every signature file and names its format.
const header = "sentinel-signature v1"

// maxSignatureSize bounds a signature file; real ones are a few hundred bytes.
const maxSignatureSize = 4 << 10

// Manifest says what an artifact is.
type Manifest struct {
	// Name is the product: "sentinel-agent", "sentinel-server",
	// "SHA256SUMS", an installer's name.
	Name    string
	Version string
	// OS and Arch are as Go names them, or "any" for an artifact that
	// isn't for one platform.
	OS     string
	Arch   string
	SHA256 [sha256.Size]byte
}

// Expect is what the caller wants an artifact to be. Empty fields aren't
// checked.
type Expect struct {
	Name, Version, OS, Arch string
}

// Errors from Verify. They are never reassigned.
var (
	// ErrBadSignature: the signature isn't valid for any trusted key, or
	// the file isn't a signature file.
	ErrBadSignature = errors.New("the release signature is missing or invalid")
	// ErrWrongArtifact: the signature is valid, but for something else, or
	// the file doesn't match its checksum.
	ErrWrongArtifact = errors.New("the file isn't the release its signature describes")
)

// field values may not contain anything that would make the manifest mean
// two things.
var fieldValue = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,100}$`)

func (m Manifest) validate() error {
	for _, value := range []string{m.Name, m.Version, m.OS, m.Arch} {
		if !fieldValue.MatchString(value) {
			return fmt.Errorf("%q can't be part of a release manifest", value)
		}
	}
	return nil
}

// signed is the exact text the signature covers.
func (m Manifest) signed() []byte {
	return []byte(header + "\nname: " + m.Name + "\nversion: " + m.Version + "\nos: " + m.OS +
		"\narch: " + m.Arch + "\nsha256: " + hex.EncodeToString(m.SHA256[:]) + "\n")
}

// Sign returns the signature file for an artifact with this manifest.
func Sign(manifest Manifest, key ed25519.PrivateKey) ([]byte, error) {
	if err := manifest.validate(); err != nil {
		return nil, err
	}
	signed := manifest.signed()
	signature := ed25519.Sign(key, signed)
	return append(signed, []byte("signature: "+base64.StdEncoding.EncodeToString(signature)+"\n")...), nil
}

// ManifestFor describes data, read to the end, as an artifact.
func ManifestFor(data io.Reader, name, version, osName, arch string) (Manifest, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, data); err != nil {
		return Manifest{}, fmt.Errorf("read the artifact: %w", err)
	}
	manifest := Manifest{Name: name, Version: version, OS: osName, Arch: arch}
	copy(manifest.SHA256[:], hash.Sum(nil))
	return manifest, manifest.validate()
}

// ParseSignature reads a signature file and checks it against the keys.
// It doesn't look at the artifact; Verify does both.
func ParseSignature(file []byte, keys []ed25519.PublicKey) (Manifest, error) {
	if len(file) > maxSignatureSize {
		return Manifest{}, ErrBadSignature
	}
	fields, err := parseFields(file)
	if err != nil {
		return Manifest{}, ErrBadSignature
	}
	manifest := Manifest{Name: fields["name"], Version: fields["version"], OS: fields["os"], Arch: fields["arch"]}
	digest, err := hex.DecodeString(fields["sha256"])
	// Only the canonical encodings are accepted, so one manifest has exactly
	// one signature file (lowercase hex, padded base64 with zero spare bits).
	if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != fields["sha256"] {
		return Manifest{}, ErrBadSignature
	}
	copy(manifest.SHA256[:], digest)
	signature, err := base64.StdEncoding.Strict().DecodeString(fields["signature"])
	if err != nil || base64.StdEncoding.EncodeToString(signature) != fields["signature"] || manifest.validate() != nil {
		return Manifest{}, ErrBadSignature
	}
	signed := manifest.signed()
	for _, key := range keys {
		if ed25519.Verify(key, signed, signature) {
			return manifest, nil
		}
	}
	return Manifest{}, ErrBadSignature
}

// parseFields reads the header line and exactly the fields a signature
// file has, in order.
func parseFields(file []byte) (map[string]string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(file)))
	if !scanner.Scan() || scanner.Text() != header {
		return nil, ErrBadSignature
	}
	fields := map[string]string{}
	for _, name := range []string{"name", "version", "os", "arch", "sha256", "signature"} {
		if !scanner.Scan() {
			return nil, ErrBadSignature
		}
		value, found := strings.CutPrefix(scanner.Text(), name+": ")
		if !found {
			return nil, ErrBadSignature
		}
		fields[name] = value
	}
	if scanner.Scan() {
		return nil, ErrBadSignature // anything after the signature
	}
	return fields, nil
}

// Verify checks an artifact against its signature file with the trusted
// keys: the signature, the checksum, and that it is what the caller
// expects. It reads data to the end.
func Verify(data io.Reader, signatureFile []byte, want Expect) (Manifest, error) {
	return VerifyWith(data, signatureFile, want, TrustedKeys())
}

// VerifyWith is Verify with the given keys.
func VerifyWith(data io.Reader, signatureFile []byte, want Expect, keys []ed25519.PublicKey) (Manifest, error) {
	manifest, err := ParseSignature(signatureFile, keys)
	if err != nil {
		return Manifest{}, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, data); err != nil {
		return Manifest{}, fmt.Errorf("read the artifact: %w", err)
	}
	var actual [sha256.Size]byte
	copy(actual[:], hash.Sum(nil))
	if actual != manifest.SHA256 || !want.matches(manifest) {
		return Manifest{}, ErrWrongArtifact
	}
	return manifest, nil
}

func (e Expect) matches(m Manifest) bool {
	return (e.Name == "" || e.Name == m.Name) && (e.Version == "" || e.Version == m.Version) &&
		(e.OS == "" || e.OS == m.OS) && (e.Arch == "" || e.Arch == m.Arch)
}

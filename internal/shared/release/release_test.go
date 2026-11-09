// SPDX-License-Identifier: Apache-2.0

package release_test

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/shared/release"
)

// developmentKey is the private half of keys/development.pub.
func developmentKey(t testing.TB) ed25519.PrivateKey {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	text, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "release", "development.key"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := release.ParsePrivateKey(string(text))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func agentBinary() []byte { return []byte("\x7fELF pretend agent binary") }

func signedAgent(t testing.TB, key ed25519.PrivateKey) []byte {
	t.Helper()
	manifest, err := release.ManifestFor(bytes.NewReader(agentBinary()), "sentinel-agent", "v1.2.0", "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	signature, err := release.Sign(manifest, key)
	if err != nil {
		t.Fatal(err)
	}
	return signature
}

func TestSEC30_ReleaseKeysAreCompiledIn(t *testing.T) {
	t.Run("trusts the compiled-in keys, which this repository can sign for in development", func(t *testing.T) {
		keys := release.TrustedKeys()
		if len(keys) == 0 {
			t.Fatal("no release key is compiled in")
		}
		if !release.IsDevelopmentBuild() {
			t.Skip("a production key is compiled in")
		}
		if !keys[0].Equal(developmentKey(t).Public()) {
			t.Error("keys/development.pub doesn't match testdata/release/development.key")
		}
	})

	t.Run("signs a manifest that names the artifact, version, platform and checksum", func(t *testing.T) {
		signature := string(signedAgent(t, developmentKey(t)))
		for _, want := range []string{"sentinel-signature v1\n", "name: sentinel-agent\n", "version: v1.2.0\n", "os: linux\n", "arch: amd64\n", "sha256: ", "signature: "} {
			if !strings.Contains(signature, want) {
				t.Errorf("the signature has no %q:\n%s", want, signature)
			}
		}
	})

	t.Run("generates and reads key files", func(t *testing.T) {
		public, private, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := release.ParsePrivateKey(release.FormatPrivateKey(private))
		if err != nil || !parsed.Equal(private) {
			t.Errorf("private key round trip: %v", err)
		}
		keys, err := release.ParsePublicKeys("# comment\n\n" + release.FormatPublicKey(public) + "\n")
		if err != nil || len(keys) != 1 || !keys[0].Equal(public) {
			t.Errorf("public key round trip: %v, %v", keys, err)
		}
	})

	for _, text := range []string{"not base64!", "AAAA"} {
		t.Run("refuses a key file holding "+text, func(t *testing.T) {
			if _, err := release.ParsePublicKeys(text); err == nil {
				t.Error("a broken public key was accepted")
			}
			if _, err := release.ParsePrivateKey(text); err == nil {
				t.Error("a broken private key was accepted")
			}
		})
	}
	t.Run("refuses an empty private key file", func(t *testing.T) {
		if _, err := release.ParsePrivateKey("# nothing\n"); err == nil {
			t.Error("expected an error")
		}
	})
	t.Run("refuses to sign a manifest with a field that could be misread", func(t *testing.T) {
		if _, err := release.Sign(release.Manifest{Name: "agent\nos: windows", Version: "v1", OS: "linux", Arch: "amd64"}, developmentKey(t)); err == nil {
			t.Error("expected an error")
		}
	})
}

func TestSEC31_UpdatesAreVerifiedBeforeInstalling(t *testing.T) {
	key := developmentKey(t)
	signature := signedAgent(t, key)
	agent := release.Expect{Name: "sentinel-agent", OS: "linux", Arch: "amd64"}

	t.Run("accepts the artifact its signature describes", func(t *testing.T) {
		manifest, err := release.Verify(bytes.NewReader(agentBinary()), signature, agent)
		if err != nil || manifest.Version != "v1.2.0" {
			t.Errorf("got %+v, %v", manifest, err)
		}
	})

	_, otherKey, _ := ed25519.GenerateKey(nil)
	cases := []struct {
		name      string
		data      []byte
		signature []byte
		want      release.Expect
		err       error
	}{
		{"an unsigned file", agentBinary(), nil, agent, release.ErrBadSignature},
		{"a signature by another key", agentBinary(), signedAgent(t, otherKey), agent, release.ErrBadSignature},
		{"a changed binary", append(bytes.Clone(agentBinary()), 0), signature, agent, release.ErrWrongArtifact},
		{"a signature with its version changed", agentBinary(), bytes.Replace(signature, []byte("v1.2.0"), []byte("v9.9.9"), 1), agent, release.ErrBadSignature},
		{"a signature with its platform changed", agentBinary(), bytes.Replace(signature, []byte("os: linux"), []byte("os: darwin"), 1), agent, release.ErrBadSignature},
		{"a build for another platform", agentBinary(), signature, release.Expect{Name: "sentinel-agent", OS: "windows", Arch: "amd64"}, release.ErrWrongArtifact},
		{"another product", agentBinary(), signature, release.Expect{Name: "sentinel-server"}, release.ErrWrongArtifact},
		{"an older version than wanted", agentBinary(), signature, release.Expect{Version: "v1.3.0"}, release.ErrWrongArtifact},
		{"a signature with a line added", agentBinary(), append(bytes.Clone(signature), []byte("extra: 1\n")...), agent, release.ErrBadSignature},
		{"a signature with a field missing", agentBinary(), bytes.Replace(signature, []byte("arch: amd64\n"), nil, 1), agent, release.ErrBadSignature},
		{"something that isn't a signature", agentBinary(), []byte("-----BEGIN PGP SIGNATURE-----"), agent, release.ErrBadSignature},
		{"a checksum that isn't hex", agentBinary(), replaceLine(signature, "sha256: ", "sha256: zz"), agent, release.ErrBadSignature},
		{"a signature that isn't base64", agentBinary(), replaceLine(signature, "signature: ", "signature: !!"), agent, release.ErrBadSignature},
		{"a huge signature file", agentBinary(), bytes.Repeat([]byte("a"), 5000), agent, release.ErrBadSignature},
	}
	for _, tc := range cases {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			if _, err := release.Verify(bytes.NewReader(tc.data), tc.signature, tc.want); !errors.Is(err, tc.err) {
				t.Errorf("got %v, want %v", err, tc.err)
			}
		})
	}
}

// replaceLine replaces the line starting with prefix.
func replaceLine(file []byte, prefix, line string) []byte {
	lines := strings.Split(string(file), "\n")
	for i := range lines {
		if strings.HasPrefix(lines[i], prefix) {
			lines[i] = line
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

func TestTrustedKeys(t *testing.T) {
	production := release.FormatPublicKey(developmentKey(t).Public().(ed25519.PublicKey))
	_, other, _ := ed25519.GenerateKey(nil)
	development := release.FormatPublicKey(other.Public().(ed25519.PublicKey))
	cases := []struct {
		name, production, development string
		want                          int
	}{
		{"trusts the production keys when there are any", "# keys\n" + production + "\n" + development, development, 2},
		{"falls back to the development key", "# none yet\n", development, 1},
		{"trusts nothing when the production file is broken", "garbage", development, 0},
		{"trusts nothing when the development file is broken", "", "garbage", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := release.TrustedKeysFrom(tc.production, tc.development); len(got) != tc.want {
				t.Errorf("got %d keys, want %d", len(got), tc.want)
			}
		})
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("disk error") }

func TestReadFailures(t *testing.T) {
	if _, err := release.ManifestFor(failingReader{}, "sentinel-agent", "v1", "linux", "amd64"); err == nil {
		t.Error("ManifestFor: expected an error")
	}
	signature := signedAgent(t, developmentKey(t))
	if _, err := release.Verify(failingReader{}, signature, release.Expect{}); err == nil {
		t.Error("Verify: expected an error")
	}
}

func FuzzParseSignature(f *testing.F) {
	key := developmentKey(f)
	f.Add(signedAgent(f, key))
	f.Add([]byte("sentinel-signature v1\nname: x\n"))
	keys := []ed25519.PublicKey{key.Public().(ed25519.PublicKey)}
	f.Fuzz(func(t *testing.T, file []byte) {
		manifest, err := release.ParseSignature(file, keys)
		if err == nil {
			// Anything accepted signs back to the same manifest.
			again, err := release.Sign(manifest, key)
			if err != nil || !bytes.Equal(again, file) {
				t.Errorf("accepted a signature that doesn't round trip: %q", file)
			}
		}
	})
}

func TestBuildNote(t *testing.T) {
	if note := release.BuildNote(); release.IsDevelopmentBuild() != strings.Contains(note, "development") {
		t.Errorf("note %q", note)
	}
}

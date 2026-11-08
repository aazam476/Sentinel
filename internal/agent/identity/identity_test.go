// SPDX-License-Identifier: Apache-2.0

package identity_test

import (
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/identity"
)

func validState(t *testing.T) identity.State {
	t.Helper()
	public, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return identity.State{ServerURL: "https://sentinel.example.com", AgentID: "01JBX3K5Q7Z8M4N6P8R0T2V4W6", ServerPublicKey: public}
}

func openStore(t *testing.T, dir string) *identity.Store {
	t.Helper()
	store, err := identity.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestKey(t *testing.T) {
	t.Run("creates a key once and keeps it", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "state")
		first, err := openStore(t, dir).Key()
		if err != nil {
			t.Fatal(err)
		}
		second, err := openStore(t, dir).Key()
		if err != nil {
			t.Fatal(err)
		}
		if !first.Equal(second) {
			t.Error("the key changed; the server would no longer recognise the agent")
		}
	})

	t.Run("refuses a key file of the wrong size", func(t *testing.T) {
		dir := t.TempDir()
		store := openStore(t, dir)
		if err := os.WriteFile(filepath.Join(dir, "agent-key"), []byte("short"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Key(); err == nil {
			t.Error("Key accepted a 5-byte key")
		}
	})

	t.Run("reports a key that can't be read or created", func(t *testing.T) {
		dir := t.TempDir()
		store := openStore(t, dir)
		if err := os.Mkdir(filepath.Join(dir, "agent-key"), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Key(); err == nil {
			t.Error("Key returned nil")
		}
	})
}

func TestState(t *testing.T) {
	t.Run("is missing until enrollment saves it", func(t *testing.T) {
		store := openStore(t, t.TempDir())
		if _, err := store.Load(); !errors.Is(err, identity.ErrNotEnrolled) {
			t.Errorf("err = %v, want ErrNotEnrolled", err)
		}
	})

	t.Run("reads back what was saved, and a second save replaces it", func(t *testing.T) {
		dir := t.TempDir()
		store := openStore(t, dir)
		first, second := validState(t), validState(t)
		second.ServerURL = "https://other.example.com/sentinel"
		for _, state := range []identity.State{first, second} {
			if err := store.Save(state); err != nil {
				t.Fatal(err)
			}
		}
		got, err := openStore(t, dir).Load()
		if err != nil {
			t.Fatal(err)
		}
		if got.ServerURL != second.ServerURL || !got.ServerPublicKey.Equal(second.ServerPublicKey) {
			t.Errorf("got %+v, want the second state", got)
		}
	})

	invalid := []struct {
		name   string
		change func(*identity.State)
	}{
		{"a server URL that isn't http(s)", func(s *identity.State) { s.ServerURL = "ftp://x" }},
		{"a server URL without a host", func(s *identity.State) { s.ServerURL = "https://" }},
		{"an invalid agent ID", func(s *identity.State) { s.AgentID = "nope" }},
		{"a server key of the wrong size", func(s *identity.State) { s.ServerPublicKey = []byte{1} }},
		{"a TLS fingerprint in the wrong format", func(s *identity.State) { s.TLSFingerprint = "md5:abc" }},
	}
	for _, test := range invalid {
		t.Run("refuses to save "+test.name, func(t *testing.T) {
			state := validState(t)
			test.change(&state)
			if err := openStore(t, t.TempDir()).Save(state); err == nil {
				t.Error("Save returned nil")
			}
		})
	}

	t.Run("reports a damaged state file", func(t *testing.T) {
		for _, content := range []string{"{", `{"serverUrl":"https://x","agentId":"bad"}`} {
			dir := t.TempDir()
			store := openStore(t, dir)
			if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Load(); err == nil || errors.Is(err, identity.ErrNotEnrolled) {
				t.Errorf("%q: err = %v, want a damage report", content, err)
			}
		}
	})

	t.Run("reports a state directory it can't write to", func(t *testing.T) {
		dir := t.TempDir()
		store := openStore(t, dir)
		if err := os.Mkdir(filepath.Join(dir, "state.json"), 0o700); err != nil { // can't be replaced by a file
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "state.json", "x"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := store.Save(validState(t)); err == nil {
			t.Error("Save returned nil")
		}
	})
}

func TestStateFileErrors(t *testing.T) {
	t.Run("reports a state file that can't be read", func(t *testing.T) {
		dir := t.TempDir()
		store := openStore(t, dir)
		if err := os.Mkdir(filepath.Join(dir, "state.json"), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(); err == nil || errors.Is(err, identity.ErrNotEnrolled) {
			t.Errorf("err = %v, want a read error", err)
		}
	})

	t.Run("reports a temporary file that can't be created", func(t *testing.T) {
		dir := t.TempDir()
		store := openStore(t, dir)
		blocker := filepath.Join(dir, "state.json.new")
		if err := os.MkdirAll(filepath.Join(blocker, "inside"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := store.Save(validState(t)); err == nil {
			t.Error("Save returned nil")
		}
	})
}

func TestOpen(t *testing.T) {
	t.Run("reports a state directory that can't be created", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := identity.Open(filepath.Join(file, "state")); err == nil {
			t.Error("Open returned nil")
		}
	})
}

func TestDefaultPaths(t *testing.T) {
	tests := []struct {
		goos, programData, state, policy string
	}{
		{"linux", "", "/var/lib/sentinel-agent", "/etc/sentinel/policy.toml"},
		{"freebsd", "", "/var/lib/sentinel-agent", "/etc/sentinel/policy.toml"},
		{"darwin", "", "/Library/Application Support/Sentinel", "/Library/Sentinel/etc/policy.toml"},
		{"windows", `D:\Data`, `D:\Data\Sentinel\state`, `D:\Data\Sentinel\etc`},
		{"windows", "", `C:\ProgramData\Sentinel\state`, `C:\ProgramData\Sentinel\etc`},
	}
	for _, test := range tests {
		t.Run("puts "+test.goos+"'s files in the standard places", func(t *testing.T) {
			paths := identity.DefaultPaths(test.goos, test.programData)
			if paths.StateDir != test.state || !strings.HasPrefix(filepath.ToSlash(paths.PolicyFile()), filepath.ToSlash(test.policy)) {
				t.Errorf("paths = %+v, policy %s", paths, paths.PolicyFile())
			}
		})
	}

	t.Run("knows this machine's paths", func(t *testing.T) {
		if identity.PathsForThisMachine().StateDir == "" {
			t.Error("no state directory")
		}
	})
}

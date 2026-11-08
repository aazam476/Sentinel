// SPDX-License-Identifier: Apache-2.0

package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/shared/keyfile"
)

// File names inside the state directory.
const (
	keyFileName   = "agent-key"
	stateFileName = "state.json"
)

// ErrNotEnrolled means the agent has no saved state yet: run
// `sentinel-agent enroll` (or install) first. It is never reassigned.
var ErrNotEnrolled = errors.New("this agent isn't enrolled yet; run `sentinel-agent enroll`")

// State is what enrollment saves.
type State struct {
	// ServerURL is the server's base URL, e.g. https://sentinel.example.com.
	ServerURL string `json:"serverUrl"`
	// AgentID is the ID the server gave this agent.
	AgentID string `json:"agentId"`
	// ServerPublicKey is the key the server must sign every handshake
	// with; it was checked against the install command's fingerprint.
	ServerPublicKey ed25519.PublicKey `json:"serverPublicKey"`
	// TLSFingerprint pins the server's self-signed TLS certificate key
	// (protocol.CertificateKeyFingerprint). Empty means the certificate is
	// checked against the system's trusted authorities, as browsers do.
	TLSFingerprint string `json:"tlsFingerprint,omitempty"`
}

// Store reads and writes the state directory.
type Store struct {
	dir string
}

// Open makes sure the state directory exists and only root or SYSTEM can
// use it, then returns a Store for it.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	if err := restrictToAdministrators(dir); err != nil {
		return nil, fmt.Errorf("protect state directory %s: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// Key returns the agent's private key, creating one the first time.
func (s *Store) Key() (ed25519.PrivateKey, error) {
	path := filepath.Join(s.dir, keyFileName)
	seed, err := keyfile.LoadOrCreate(path, "agent key", newSeed)
	if err != nil {
		return nil, err
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("agent key %s is %d bytes, want %d", path, len(seed), ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// Load returns the saved state, or ErrNotEnrolled.
func (s *Store) Load() (State, error) {
	data, err := keyfile.Read(filepath.Join(s.dir, stateFileName), "agent state")
	if errors.Is(err, fs.ErrNotExist) {
		return State{}, ErrNotEnrolled
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("read agent state: %w", err)
	}
	if err := state.validate(); err != nil {
		return State{}, fmt.Errorf("agent state is damaged: %w", err)
	}
	return state, nil
}

// Save stores the state, replacing any earlier one in a single step so a
// crash can't leave half a file behind.
func (s *Store) Save(state State) error {
	if err := state.validate(); err != nil {
		return err
	}
	// Encoding can't fail: State holds only strings and bytes.
	data, _ := json.MarshalIndent(state, "", "  ")
	temporary := filepath.Join(s.dir, stateFileName+".new")
	_ = os.Remove(temporary) // left over from a crash, if anything
	if err := keyfile.Write(temporary, "agent state", data); err != nil {
		return err
	}
	if err := os.Rename(temporary, filepath.Join(s.dir, stateFileName)); err != nil {
		return fmt.Errorf("save agent state: %w", err)
	}
	return nil
}

func (state State) validate() error {
	server, err := url.Parse(state.ServerURL)
	if err != nil || (server.Scheme != "https" && server.Scheme != "http") || server.Host == "" {
		return fmt.Errorf("server URL %q isn't an http(s) URL", state.ServerURL)
	}
	if !protocol.IsID(state.AgentID) {
		return fmt.Errorf("agent ID %q isn't valid", state.AgentID)
	}
	if len(state.ServerPublicKey) != ed25519.PublicKeySize {
		return errors.New("the server key isn't an Ed25519 public key")
	}
	if state.TLSFingerprint != "" && !strings.HasPrefix(state.TLSFingerprint, "SHA256:") {
		return fmt.Errorf("the TLS fingerprint %q isn't in SHA256:... form", state.TLSFingerprint)
	}
	return nil
}

func newSeed() []byte {
	seed := make([]byte, ed25519.SeedSize)
	// crypto/rand.Read never returns an error: Go stops the program if the
	// system's random source fails.
	_, _ = rand.Read(seed)
	return seed
}

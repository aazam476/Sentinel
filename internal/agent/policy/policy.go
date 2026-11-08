// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/aazam476/sentinel/internal/protocol"
)

// The capabilities a policy can switch off (SEC-22).
const (
	Terminal = "terminal"
	Desktop  = "desktop"
	Files    = "files"
	Scripts  = "scripts"
	Docker   = "docker"
	Storage  = "storage"
	Packages = "packages"
	Power    = "power"
)

// DisabledReason is what the UI shows for a capability the policy switches
// off.
const DisabledReason = "Disabled by local policy"

// maxFileSize caps the policy file; a real one is a few hundred bytes.
const maxFileSize = 64 << 10

// Capabilities lists every capability, in the order the UI shows them.
func Capabilities() []string {
	return []string{Terminal, Desktop, Files, Scripts, Docker, Storage, Packages, Power}
}

// Policy is the parsed policy file.
type Policy struct {
	disabled []string
	// Docker holds the machine's Docker settings; empty fields use the
	// defaults.
	Docker DockerSettings
}

// DockerSettings are where the agent finds Docker and keeps stacks (§9.4).
// They are local settings, like the capabilities: the server can't move
// the agent to another socket or folder.
type DockerSettings struct {
	// Socket is the Docker or Podman API socket, such as
	// unix:///run/podman/podman.sock.
	Socket string `toml:"socket"`
	// StacksDir holds one folder per compose stack.
	StacksDir string `toml:"stacks_dir"`
}

// file is the policy file's TOML layout.
type file struct {
	Capabilities map[string]bool `toml:"capabilities"`
	Docker       DockerSettings  `toml:"docker"`
}

// Allows reports whether the policy allows a capability.
func (p Policy) Allows(capability string) bool {
	return !slices.Contains(p.disabled, capability)
}

// Report lists every capability and whether it is allowed, for the server.
func (p Policy) Report() []protocol.Capability {
	report := make([]protocol.Capability, 0, len(Capabilities()))
	for _, name := range Capabilities() {
		capability := protocol.Capability{Name: name, Enabled: p.Allows(name)}
		if !capability.Enabled {
			capability.Reason = DisabledReason
		}
		report = append(report, capability)
	}
	return report
}

// Load reads the policy file at path. A missing file allows everything.
func Load(path string) (Policy, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Policy{}, nil
	}
	if err != nil {
		return Policy{}, fmt.Errorf("read policy file: %w", err)
	}
	if err := checkTrusted(path, info); err != nil {
		return Policy{}, err
	}
	if info.Size() > maxFileSize {
		return Policy{}, fmt.Errorf("policy file %s is larger than %d bytes", path, maxFileSize)
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: the path is the fixed policy location
	if err != nil {
		return Policy{}, fmt.Errorf("read policy file: %w", err)
	}
	policy, err := Parse(string(data))
	if err != nil {
		return Policy{}, fmt.Errorf("policy file %s: %w", path, err)
	}
	return policy, nil
}

// Parse reads the text of a policy file. Unknown sections, keys and
// capability names are errors, so a typo can't leave a capability on.
func Parse(text string) (Policy, error) {
	var parsed file
	metadata, err := toml.Decode(text, &parsed)
	if err != nil {
		return Policy{}, fmt.Errorf("not valid TOML: %w", err)
	}
	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, 0, len(undecoded))
		for _, key := range undecoded {
			keys = append(keys, key.String())
		}
		return Policy{}, fmt.Errorf("unknown setting %s", strings.Join(keys, ", "))
	}
	policy := Policy{Docker: parsed.Docker}
	for name, allowed := range parsed.Capabilities {
		if !slices.Contains(Capabilities(), name) {
			return Policy{}, fmt.Errorf("unknown capability %q; the capabilities are %s", name, strings.Join(Capabilities(), ", "))
		}
		if !allowed {
			policy.disabled = append(policy.disabled, name)
		}
	}
	slices.Sort(policy.disabled)
	return policy, nil
}

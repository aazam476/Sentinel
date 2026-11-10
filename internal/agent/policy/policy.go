// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
	// Files limits the file manager (SEC-25).
	Files FilesSettings
	// Desktop configures remote desktop (§9.3).
	Desktop DesktopSettings
	// DesktopRequiresConsent asks the person at the machine to allow each
	// remote desktop session first (SEC-23).
	DesktopRequiresConsent bool
	// ShowSessionIndicator shows the person at the machine a notice while
	// any remote terminal or desktop session is open (SEC-24).
	ShowSessionIndicator bool
}

// DesktopSettings are the machine owner's remote desktop settings.
type DesktopSettings struct {
	// TestPattern shares a moving test pattern instead of the screen, to
	// check that remote desktop connects (directly or through the server)
	// without showing anything on the machine.
	TestPattern bool `toml:"test_pattern"`
	// Display is the X display to share, such as ":0", instead of the
	// active session's; for machines without loginctl, or kiosks.
	Display string `toml:"display"`
	// XAuthority is the Xauthority file with Display's cookie; empty
	// connects without one.
	XAuthority string `toml:"xauthority"`
}

// FilesSettings limit the file manager to some folders (SEC-25). Like the
// capabilities, only the machine's owner can change them.
type FilesSettings struct {
	// AllowedPaths are the folders the file manager may use, with
	// everything inside them; empty allows the whole file system.
	AllowedPaths []string `toml:"allowed_paths"`
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
	Files        FilesSettings   `toml:"files"`
	Desktop      DesktopSettings `toml:"desktop"`
	// The two SEC-23 and SEC-24 settings sit at the top level, under the
	// names §6 gives them.
	DesktopRequiresConsent bool `toml:"desktop_requires_consent"`
	ShowSessionIndicator   bool `toml:"show_session_indicator"`
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
	allowed, err := allowedPaths(parsed.Files.AllowedPaths)
	if err != nil {
		return Policy{}, err
	}
	if err := checkDesktop(parsed.Desktop); err != nil {
		return Policy{}, err
	}
	policy := Policy{
		Docker: parsed.Docker, Files: FilesSettings{AllowedPaths: allowed}, Desktop: parsed.Desktop,
		DesktopRequiresConsent: parsed.DesktopRequiresConsent, ShowSessionIndicator: parsed.ShowSessionIndicator,
	}
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

// allowedPaths checks the file manager's allowed folders: each must be an
// absolute path. They are cleaned, so "/srv/" and "/srv" mean the same.
func allowedPaths(paths []string) ([]string, error) {
	cleaned := make([]string, 0, len(paths))
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("files.allowed_paths: %q is not an absolute path", path)
		}
		cleaned = append(cleaned, filepath.Clean(path))
	}
	return cleaned, nil
}

// checkDesktop checks the desktop settings name a local display, and an
// Xauthority file only along with it.
func checkDesktop(settings DesktopSettings) error {
	if settings.Display != "" && !strings.HasPrefix(settings.Display, ":") {
		return fmt.Errorf("desktop.display: %q is not a local display such as \":0\"", settings.Display)
	}
	if settings.XAuthority != "" && (settings.Display == "" || !filepath.IsAbs(settings.XAuthority)) {
		return fmt.Errorf("desktop.xauthority: %q needs desktop.display, and must be an absolute path", settings.XAuthority)
	}
	return nil
}

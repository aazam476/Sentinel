// SPDX-License-Identifier: Apache-2.0

package packages

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/protocol"
)

// brewPaths are where Homebrew installs itself: Apple silicon, then Intel.
func brewPaths() []string { return []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} }

// darwinManagers is softwareupdate, and Homebrew when it is installed.
func darwinManagers(host Host) []Manager {
	managers := []Manager{softwareUpdate{host.Runner}}
	for _, path := range brewPaths() {
		if host.Exists(path) {
			if owner, err := host.Owner(path); err == nil && owner != "root" {
				managers = append(managers, homebrew{runner: host.Runner, path: path, owner: owner})
			}
			break
		}
	}
	return managers
}

// softwareUpdate is Apple's updates for macOS and its apps.
type softwareUpdate struct{ runner command.Runner }

func (softwareUpdate) Name() string     { return "softwareupdate" }
func (softwareUpdate) Selectable() bool { return true }

// validID allows an update's label, which has spaces: "macOS Sonoma
// 14.5-23F79".
func (softwareUpdate) validID(id string) bool { return updateLabel.MatchString(id) }

var updateLabel = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.,()+-]*$`)

func (s softwareUpdate) List(ctx context.Context) ([]protocol.PackageUpdate, error) {
	output, err := s.runner.Run(ctx, "/usr/sbin/softwareupdate", "--list")
	if err != nil {
		return nil, err
	}
	return ParseSoftwareUpdate(output), nil
}

func (s softwareUpdate) Apply(ctx context.Context, ids []string) ([]byte, error) {
	if len(ids) == 0 {
		return s.runner.Run(ctx, "/usr/sbin/softwareupdate", "--install", "--all")
	}
	return s.runner.Run(ctx, "/usr/sbin/softwareupdate", append([]string{"--install"}, ids...)...)
}

// softwareUpdateTitle is the line after a label: "Title: X, Version: Y, ...".
var softwareUpdateTitle = regexp.MustCompile(`^\s*Title: (.*?), Version: ([^,]*),`)

// ParseSoftwareUpdate reads `softwareupdate --list`: "* Label: X" lines,
// each followed by its title and version.
func ParseSoftwareUpdate(output []byte) []protocol.PackageUpdate {
	updates := []protocol.PackageUpdate{}
	for line := range strings.Lines(string(output)) {
		if label, ok := strings.CutPrefix(strings.TrimSpace(line), "* Label: "); ok {
			if !updateLabel.MatchString(label) || len(label) > maxID {
				continue
			}
			updates = append(updates, protocol.PackageUpdate{ID: label, Name: label})
			continue
		}
		match := softwareUpdateTitle.FindStringSubmatch(line)
		if match != nil && len(updates) > 0 {
			last := &updates[len(updates)-1]
			last.Name, last.Available = match[1], match[2]
			last.Security = strings.Contains(match[1], "Security") || strings.Contains(match[1], "Background Security")
		}
	}
	return updates
}

// homebrew runs as the user who installed it: it refuses to run as root.
type homebrew struct {
	runner      command.Runner
	path, owner string
}

func (homebrew) Name() string           { return "homebrew" }
func (homebrew) Selectable() bool       { return true }
func (homebrew) validID(id string) bool { return packageName.MatchString(id) }

// run runs brew as its owner, with the owner's home and environment.
func (h homebrew) run(ctx context.Context, args ...string) ([]byte, error) {
	return h.runner.Run(ctx, "/usr/bin/sudo", append([]string{"-u", h.owner, "-H", "--", h.path}, args...)...)
}

func (h homebrew) List(ctx context.Context) ([]protocol.PackageUpdate, error) {
	refresh(ctx, h.runner, "/usr/bin/sudo", "-u", h.owner, "-H", "--", h.path, "update", "--quiet")
	output, err := h.run(ctx, "outdated", "--json=v2")
	if err != nil {
		return nil, err
	}
	return ParseHomebrew(output)
}

func (h homebrew) Apply(ctx context.Context, ids []string) ([]byte, error) {
	return h.run(ctx, append([]string{"upgrade", "--"}, ids...)...)
}

type brewOutdated struct {
	Formulae []brewPackage `json:"formulae"`
	Casks    []brewPackage `json:"casks"`
}

type brewPackage struct {
	Name      string   `json:"name"`
	Installed []string `json:"installed_versions"`
	Current   string   `json:"current_version"`
	Pinned    bool     `json:"pinned"`
}

// ParseHomebrew reads `brew outdated --json=v2`. Pinned formulae are left
// out: brew won't upgrade them.
func ParseHomebrew(output []byte) ([]protocol.PackageUpdate, error) {
	var outdated brewOutdated
	if err := json.Unmarshal(output, &outdated); err != nil {
		return nil, fmt.Errorf("read brew's list of outdated packages: %w", err)
	}
	updates := []protocol.PackageUpdate{}
	for _, item := range append(outdated.Formulae, outdated.Casks...) {
		if item.Pinned || item.Name == "" {
			continue
		}
		update := protocol.PackageUpdate{ID: item.Name, Name: item.Name, Available: item.Current}
		if len(item.Installed) > 0 {
			update.Current = item.Installed[len(item.Installed)-1]
		}
		updates = withValidName(updates, update)
	}
	return updates, nil
}

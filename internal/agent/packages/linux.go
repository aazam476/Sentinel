// SPDX-License-Identifier: Apache-2.0

package packages

import (
	"context"
	"regexp"
	"strings"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/protocol"
)

// linuxManagers finds the distribution's package manager. A machine has
// one; the first found wins.
func linuxManagers(host Host) []Manager {
	candidates := []struct {
		program string
		manager Manager
	}{
		{"apt-get", apt{host.Runner}},
		{"dnf", dnf{host.Runner}},
		{"zypper", zypper{host.Runner}},
		{"pacman", pacman{host.Runner}},
		{"apk", apk{host.Runner}},
	}
	for _, candidate := range candidates {
		if host.Find(candidate.program) != "" {
			return []Manager{candidate.manager}
		}
	}
	return nil
}

// refresh updates a manager's catalog. Failing (a mirror is down) isn't
// fatal: the list is then as of the last refresh.
func refresh(ctx context.Context, runner command.Runner, name string, args ...string) {
	_, _ = runner.Run(ctx, name, args...)
}

// apt is Debian and Ubuntu.
type apt struct{ runner command.Runner }

func (apt) Name() string           { return "apt" }
func (apt) Selectable() bool       { return true }
func (apt) validID(id string) bool { return packageName.MatchString(id) }

func (a apt) List(ctx context.Context) ([]protocol.PackageUpdate, error) {
	refresh(ctx, a.runner, "apt-get", "update", "-qq")
	output, err := a.runner.Run(ctx, "apt", "list", "--upgradable")
	if err != nil {
		return nil, err
	}
	return ParseApt(output), nil
}

// aptOptions keep a package's changed configuration files and never ask.
func aptOptions() []string {
	return []string{"DEBIAN_FRONTEND=noninteractive", "apt-get", "-y", "-q",
		"-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold"}
}

func (a apt) Apply(ctx context.Context, ids []string) ([]byte, error) {
	args := aptOptions()
	if len(ids) == 0 {
		args = append(args, "dist-upgrade")
	} else {
		args = append(append(args, "install", "--only-upgrade", "--"), ids...)
	}
	return a.runner.Run(ctx, "env", args...)
}

// aptLine is "name/suite version arch [upgradable from: old]".
var aptLine = regexp.MustCompile(`^([^/\s]+)/(\S+) (\S+) \S+ \[upgradable from: ([^\]]+)\]`)

// ParseApt reads `apt list --upgradable`.
func ParseApt(output []byte) []protocol.PackageUpdate {
	updates := []protocol.PackageUpdate{}
	for line := range strings.Lines(string(output)) {
		match := aptLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		security := false
		for suite := range strings.SplitSeq(match[2], ",") {
			security = security || strings.HasSuffix(suite, "-security")
		}
		updates = withValidName(updates, protocol.PackageUpdate{ID: match[1], Name: match[1], Current: match[4], Available: match[3], Security: security})
	}
	return updates
}

// dnf is Fedora, RHEL and their relatives.
type dnf struct{ runner command.Runner }

func (dnf) Name() string           { return "dnf" }
func (dnf) Selectable() bool       { return true }
func (dnf) validID(id string) bool { return packageName.MatchString(id) }

func (d dnf) List(ctx context.Context) ([]protocol.PackageUpdate, error) {
	output, err := d.runner.Run(ctx, "dnf", "-q", "--refresh", "list", "--upgrades")
	if err != nil {
		return nil, err
	}
	return ParseDnf(output), nil
}

func (d dnf) Apply(ctx context.Context, ids []string) ([]byte, error) {
	args := []string{"-y", "upgrade"}
	if len(ids) > 0 {
		args = append(append(args, "--"), ids...)
	}
	return d.runner.Run(ctx, "dnf", args...)
}

// ParseDnf reads `dnf list --upgrades`: "name.arch version repo" lines,
// after a heading.
func ParseDnf(output []byte) []protocol.PackageUpdate {
	updates := []protocol.PackageUpdate{}
	for line := range strings.Lines(string(output)) {
		parts := fields(line)
		dot := strings.LastIndexByte(firstOr(parts), '.')
		if len(parts) != 3 || dot <= 0 {
			continue
		}
		name := parts[0][:dot]
		updates = withValidName(updates, protocol.PackageUpdate{ID: name, Name: name, Available: parts[1]})
	}
	return updates
}

// zypper is openSUSE and SLES.
type zypper struct{ runner command.Runner }

func (zypper) Name() string           { return "zypper" }
func (zypper) Selectable() bool       { return true }
func (zypper) validID(id string) bool { return packageName.MatchString(id) }

func (z zypper) List(ctx context.Context) ([]protocol.PackageUpdate, error) {
	refresh(ctx, z.runner, "zypper", "--non-interactive", "--quiet", "refresh")
	output, err := z.runner.Run(ctx, "zypper", "--non-interactive", "--quiet", "list-updates")
	if err != nil {
		return nil, err
	}
	return ParseZypper(output), nil
}

func (z zypper) Apply(ctx context.Context, ids []string) ([]byte, error) {
	args := []string{"--non-interactive", "update"}
	if len(ids) > 0 {
		args = append(append(args, "--"), ids...)
	}
	return z.runner.Run(ctx, "zypper", args...)
}

// ParseZypper reads `zypper list-updates`: a table whose rows are
// "v | repository | name | current | available | arch".
func ParseZypper(output []byte) []protocol.PackageUpdate {
	updates := []protocol.PackageUpdate{}
	for line := range strings.Lines(string(output)) {
		cells := strings.Split(line, "|")
		if len(cells) != 6 || strings.TrimSpace(cells[0]) != "v" {
			continue
		}
		name := strings.TrimSpace(cells[2])
		updates = withValidName(updates, protocol.PackageUpdate{
			ID: name, Name: name, Current: strings.TrimSpace(cells[3]), Available: strings.TrimSpace(cells[4]),
		})
	}
	return updates
}

// pacman is Arch. It only upgrades everything at once: Arch doesn't
// support partial upgrades.
type pacman struct{ runner command.Runner }

func (pacman) Name() string        { return "pacman" }
func (pacman) Selectable() bool    { return false }
func (pacman) validID(string) bool { return false }

// List uses checkupdates (pacman-contrib), which refreshes a copy of the
// catalog: `pacman -Sy` alone would leave the machine half upgraded.
func (p pacman) List(ctx context.Context) ([]protocol.PackageUpdate, error) {
	output, err := p.runner.Run(ctx, "checkupdates")
	if err != nil && len(output) == 0 && strings.Contains(err.Error(), "exit status 2") {
		return []protocol.PackageUpdate{}, nil // checkupdates exits 2 when there is nothing to update
	}
	if err != nil {
		return nil, err
	}
	return ParsePacman(output), nil
}

func (p pacman) Apply(ctx context.Context, _ []string) ([]byte, error) {
	return p.runner.Run(ctx, "pacman", "-Syu", "--noconfirm")
}

// ParsePacman reads checkupdates: "name old -> new".
func ParsePacman(output []byte) []protocol.PackageUpdate {
	updates := []protocol.PackageUpdate{}
	for line := range strings.Lines(string(output)) {
		parts := fields(line)
		if len(parts) != 4 || parts[2] != "->" {
			continue
		}
		updates = withValidName(updates, protocol.PackageUpdate{ID: parts[0], Name: parts[0], Current: parts[1], Available: parts[3]})
	}
	return updates
}

// apk is Alpine.
type apk struct{ runner command.Runner }

func (apk) Name() string           { return "apk" }
func (apk) Selectable() bool       { return true }
func (apk) validID(id string) bool { return packageName.MatchString(id) }

func (a apk) List(ctx context.Context) ([]protocol.PackageUpdate, error) {
	refresh(ctx, a.runner, "apk", "update", "--quiet")
	output, err := a.runner.Run(ctx, "apk", "list", "--upgradable")
	if err != nil {
		return nil, err
	}
	return ParseApk(output), nil
}

func (a apk) Apply(ctx context.Context, ids []string) ([]byte, error) {
	if len(ids) == 0 {
		return a.runner.Run(ctx, "apk", "upgrade", "--no-interactive")
	}
	return a.runner.Run(ctx, "apk", append([]string{"add", "--upgrade", "--no-interactive", "--"}, ids...)...)
}

// apkLine is "name-version arch {origin} (license) [upgradable from: name-old]".
var apkLine = regexp.MustCompile(`^(\S+) \S+ \{[^}]*\} \([^)]*\) \[upgradable from: (\S+)\]`)

// apkVersion is the end of a package's full name: "-1.36.1-r5".
var apkVersion = regexp.MustCompile(`-([0-9][^-]*-r[0-9]+)$`)

// ParseApk reads `apk list --upgradable`.
func ParseApk(output []byte) []protocol.PackageUpdate {
	updates := []protocol.PackageUpdate{}
	for line := range strings.Lines(string(output)) {
		match := apkLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		version := apkVersion.FindStringSubmatchIndex(match[1])
		if version == nil {
			continue
		}
		name := match[1][:version[0]]
		current := strings.TrimPrefix(match[2], name+"-")
		updates = withValidName(updates, protocol.PackageUpdate{ID: name, Name: name, Current: current, Available: match[1][version[2]:]})
	}
	return updates
}

func firstOr(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// withValidName adds an update only if its name could be installed: a
// parser never offers an update that Apply would refuse.
func withValidName(updates []protocol.PackageUpdate, update protocol.PackageUpdate) []protocol.PackageUpdate {
	if len(update.ID) > maxID || !packageName.MatchString(update.ID) {
		return updates
	}
	return append(updates, update)
}

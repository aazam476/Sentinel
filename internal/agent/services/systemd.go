// SPDX-License-Identifier: Apache-2.0

package services

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/protocol"
)

// Systemd manages systemd units through systemctl.
type Systemd struct {
	runner command.Runner
}

// NewSystemd returns a Systemd that runs systemctl with runner.
func NewSystemd(runner command.Runner) Systemd { return Systemd{runner: runner} }

// List combines the installed unit files (which says whether each starts
// at boot) with the loaded units (which says whether each is running).
// Neither list alone has every service: a unit that isn't running and
// isn't enabled usually isn't loaded, and a unit can be loaded with no
// unit file of its own.
func (s Systemd) List(ctx context.Context) (protocol.ServiceListPayload, error) {
	files, err := s.runner.Run(ctx, "systemctl", "list-unit-files", "--type=service", "--no-legend", "--no-pager")
	if err != nil {
		return protocol.ServiceListPayload{}, fmt.Errorf("list unit files: %w", unsupportedIfMissing(err))
	}
	units, err := s.runner.Run(ctx, "systemctl", "list-units", "--type=service", "--all", "--no-legend", "--no-pager", "--plain")
	if err != nil {
		return protocol.ServiceListPayload{}, fmt.Errorf("list units: %w", unsupportedIfMissing(err))
	}
	return protocol.ServiceListPayload{Manager: "systemd", Services: mergeSystemd(ParseUnitFiles(files), ParseUnits(units))}, nil
}

// Do runs systemctl for the action. The name goes after "--", so even a
// name that got past ValidateName couldn't be read as an option.
func (s Systemd) Do(ctx context.Context, action Action, name string) error {
	if err := ValidateName("linux", name); err != nil {
		return err
	}
	if !slices.Contains(Actions(), action) {
		return fmt.Errorf("unknown service action %q", action)
	}
	if slices.Contains(systemdPowerServices(), name) {
		return fmt.Errorf("%s: %w", name, ErrPowerService)
	}
	if _, err := s.runner.Run(ctx, "systemctl", string(action), "--no-ask-password", "--", name); err != nil {
		if errors.Is(err, command.ErrNotFound) {
			return fmt.Errorf("systemctl: %w", unsupportedIfMissing(err))
		}
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "does not exist") {
			return fmt.Errorf("%s: %w", name, ErrNotFound)
		}
		return fmt.Errorf("systemctl %s: %w", action, unsupportedIfMissing(err))
	}
	return nil
}

// systemdPowerServices reboot, power off or suspend the machine when
// started. They are refused, so the power capability (SEC-22) can't be
// got around through services.
func systemdPowerServices() []string {
	return []string{
		"systemd-reboot.service", "systemd-poweroff.service", "systemd-halt.service",
		"systemd-kexec.service", "systemd-soft-reboot.service", "systemd-suspend.service",
		"systemd-hibernate.service", "systemd-hybrid-sleep.service", "systemd-suspend-then-hibernate.service",
	}
}

// UnitFile is one line of `systemctl list-unit-files`.
type UnitFile struct {
	Name  string
	State string
}

// ParseUnitFiles reads `systemctl list-unit-files --no-legend` output:
// "UNIT STATE [PRESET]" per line. Template units (name@.service) are
// left out: they can't run until instantiated.
func ParseUnitFiles(output []byte) []UnitFile {
	var files []UnitFile
	for fields := range lines(output) {
		if len(fields) < 2 || strings.HasSuffix(fields[0], "@.service") {
			continue
		}
		files = append(files, UnitFile{Name: fields[0], State: fields[1]})
	}
	return files
}

// Unit is one line of `systemctl list-units`.
type Unit struct {
	Name, Load, Active, Sub, Description string
}

// ParseUnits reads `systemctl list-units --no-legend --plain` output:
// "UNIT LOAD ACTIVE SUB DESCRIPTION...", where the description may have
// spaces.
func ParseUnits(output []byte) []Unit {
	var units []Unit
	for fields := range lines(output) {
		if len(fields) < 4 {
			continue
		}
		units = append(units, Unit{Name: fields[0], Load: fields[1], Active: fields[2], Sub: fields[3],
			Description: strings.Join(fields[4:], " ")})
	}
	return units
}

// lines yields each non-empty line of output split into fields. A leading
// status bullet ("●", for units that failed or weren't found) is dropped.
func lines(output []byte) func(yield func([]string) bool) {
	return func(yield func([]string) bool) {
		scanner := bufio.NewScanner(bytes.NewReader(output))
		scanner.Buffer(make([]byte, 64<<10), 1<<20)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) > 0 && (fields[0] == "●" || fields[0] == "*") {
				fields = fields[1:]
			}
			if len(fields) > 0 && !yield(fields) {
				return
			}
		}
	}
}

// mergeSystemd joins unit files and loaded units by name, sorted by name.
func mergeSystemd(files []UnitFile, units []Unit) []protocol.Service {
	byName := map[string]*protocol.Service{}
	for _, file := range files {
		if file.State == "alias" {
			continue // another name for a unit that is listed itself
		}
		byName[file.Name] = &protocol.Service{Name: file.Name, State: protocol.ServiceStopped, Startup: systemdStartup(file.State)}
	}
	for _, unit := range units {
		service, known := byName[unit.Name]
		if !known {
			if unit.Load == "not-found" {
				continue // referenced by another unit, but not installed
			}
			service = &protocol.Service{Name: unit.Name, Startup: protocol.StartupFixed}
			byName[unit.Name] = service
		}
		service.Description = unit.Description
		service.State = systemdState(unit.Active, unit.Sub)
	}
	services := make([]protocol.Service, 0, len(byName))
	for _, service := range byName {
		services = append(services, *service)
	}
	slices.SortFunc(services, func(a, b protocol.Service) int { return strings.Compare(a.Name, b.Name) })
	return services
}

func systemdStartup(state string) protocol.ServiceStartup {
	switch state {
	case "enabled", "enabled-runtime":
		return protocol.StartupAutomatic
	case "disabled":
		return protocol.StartupManual
	case "masked", "masked-runtime":
		return protocol.StartupDisabled
	default: // static, indirect, generated, transient, linked, ...
		return protocol.StartupFixed
	}
}

// systemdState maps ACTIVE and SUB. A oneshot service that ran and
// finished is "active (exited)": it counts as stopped, since nothing runs.
func systemdState(active, sub string) protocol.ServiceState {
	switch {
	case active == "active" && sub == "exited":
		return protocol.ServiceStopped
	case active == "active", active == "reloading":
		return protocol.ServiceRunning
	case active == "inactive":
		return protocol.ServiceStopped
	case active == "failed":
		return protocol.ServiceFailed
	case active == "activating":
		return protocol.ServiceStarting
	case active == "deactivating":
		return protocol.ServiceStopping
	default:
		return protocol.ServiceUnknown
	}
}

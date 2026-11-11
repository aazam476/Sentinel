// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// runKey holds the programs Windows starts for everyone who signs in.
const runKey = `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`

// runValue names the tray's entry.
const runValue = "Sentinel"

// MachineAutostart starts the tray from the Run key for everyone who
// signs in.
func MachineAutostart(executable string) Autostart {
	return windowsAutostart{executable: executable}
}

type windowsAutostart struct{ executable string }

func (a windowsAutostart) Install() error {
	key, _, err := registry.CreateKey(registry.LOCAL_MACHINE, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open the Run key: %w", err)
	}
	defer func() { _ = key.Close() }() // only read-write handles need checking
	if err := key.SetStringValue(runValue, runCommand(a.executable)); err != nil {
		return fmt.Errorf("add the tray to the Run key: %w", err)
	}
	return nil
}

func (windowsAutostart) Uninstall() error {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open the Run key: %w", err)
	}
	defer func() { _ = key.Close() }() // as above
	if err := key.DeleteValue(runValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("remove the tray from the Run key: %w", err)
	}
	return nil
}

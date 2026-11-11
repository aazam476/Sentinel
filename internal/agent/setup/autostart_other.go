// SPDX-License-Identifier: Apache-2.0

//go:build !windows && !darwin

package setup

// MachineAutostart is nil: there is no tray on this system.
func MachineAutostart(string) Autostart {
	return nil
}

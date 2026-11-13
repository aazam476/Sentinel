// SPDX-License-Identifier: Apache-2.0

package packages

// The Windows Update scripts, for tests to expect.
const (
	SearchScript  = searchScript
	InstallScript = installScript
)

// ValidID says whether a manager's Apply would take an ID.
func ValidID(manager, id string) bool {
	all := []Manager{apt{}, dnf{}, zypper{}, pacman{}, apk{}, softwareUpdate{}, homebrew{}, windowsUpdate{}, winget{}}
	for _, m := range all {
		if m.Name() == manager {
			return m.validID(id) || !m.Selectable()
		}
	}
	return false
}

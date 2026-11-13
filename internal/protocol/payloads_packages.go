// SPDX-License-Identifier: Apache-2.0

package protocol

// Package updates (§9.6): the server lists what each package manager on a
// machine can update, and installs some or all of them.
const (
	ActionPackageListUpdates  = "package.list.updates"
	ActionPackageApplyUpdates = "package.apply.updates"
)

// PackageUpdate is one update a package manager offers.
type PackageUpdate struct {
	// ID is what package.apply.updates takes: the package name, or an
	// update's ID on Windows Update.
	ID string `json:"id"`
	// Name is what people read: the package name, or an update's title.
	Name string `json:"name"`
	// Current is the installed version, or "" when the manager doesn't say.
	Current string `json:"current"`
	// Available is the version the update installs, or "".
	Available string `json:"available"`
	// Security is true for updates the manager marks as security fixes.
	Security bool `json:"security"`
}

// PackageSource is one package manager's updates.
type PackageSource struct {
	// Manager is apt, dnf, zypper, pacman, apk, softwareupdate, homebrew,
	// windows-update or winget.
	Manager string          `json:"manager"`
	Updates []PackageUpdate `json:"updates"`
	// Selectable is false for managers that only update everything at
	// once (pacman: Arch doesn't support partial upgrades).
	Selectable bool `json:"selectable"`
	// Error says why the manager couldn't be asked; Updates is then empty.
	Error string `json:"error,omitempty"`
}

// PackageUpdatesPayload answers package.list.updates.
type PackageUpdatesPayload struct {
	Sources []PackageSource `json:"sources"`
	// RestartRequired is true when installed updates wait for a restart.
	RestartRequired bool `json:"restartRequired"`
}

// PackageApplyPayload asks package.apply.updates to install updates from
// one manager: those with these IDs, or every one when IDs is empty.
type PackageApplyPayload struct {
	Manager string   `json:"manager"`
	IDs     []string `json:"ids"`
}

// PackageApplyResult answers package.apply.updates.
type PackageApplyResult struct {
	// Output is the end of what the package manager printed.
	Output string `json:"output"`
	// RestartRequired is true when the machine must restart to finish.
	RestartRequired bool `json:"restartRequired"`
}

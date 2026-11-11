// SPDX-License-Identifier: Apache-2.0

package setup

// StarterPolicy is the policy file written on first enrollment on goos.
func StarterPolicy(goos string) string { return starterPolicy(goos) }

// LaunchAgentPlist is the macOS launch agent that starts the tray.
func LaunchAgentPlist(executable string) string { return launchAgentPlist(executable) }

// RunCommand is the Windows Run entry that starts the tray.
func RunCommand(executable string) string { return runCommand(executable) }

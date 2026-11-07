// SPDX-License-Identifier: Apache-2.0

package buildinfo

import "runtime/debug"

// develVersion is what the Go toolchain reports as the module version when a
// binary is built from a checkout that isn't exactly at a release tag (and by
// older toolchains, for every local build).
const develVersion = "(devel)"

// shortRevisionLength is how many characters of the commit hash to show.
// Twelve is enough to be unique in this repository for a very long time.
const shortRevisionLength = 12

// Version returns a human-readable version string for the running binary.
//
// Release builds return their tag, for example "v0.1.0-beta.1". Development
// builds return "dev-" followed by the short commit hash, plus "-dirty" when
// the working tree had uncommitted changes. If the binary carries no build
// information at all it returns "dev".
func Version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return describe(nil)
	}
	return describe(info)
}

// describe turns build information into a version string. It is separate from
// Version so tests can feed it every kind of build information.
func describe(info *debug.BuildInfo) string {
	if info == nil {
		return "dev"
	}

	if info.Main.Version != "" && info.Main.Version != develVersion {
		return info.Main.Version
	}

	revision := buildSetting(info, "vcs.revision")
	if revision == "" {
		return "dev"
	}
	if len(revision) > shortRevisionLength {
		revision = revision[:shortRevisionLength]
	}

	version := "dev-" + revision
	if buildSetting(info, "vcs.modified") == "true" {
		version += "-dirty"
	}
	return version
}

// buildSetting returns the value of one build setting, or "" if it's missing.
func buildSetting(info *debug.BuildInfo, key string) string {
	for _, setting := range info.Settings {
		if setting.Key == key {
			return setting.Value
		}
	}
	return ""
}

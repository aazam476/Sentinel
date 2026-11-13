// SPDX-License-Identifier: Apache-2.0

package packages_test

import (
	"os"
	"testing"
	"unicode/utf8"

	"github.com/aazam476/sentinel/internal/agent/packages"
	"github.com/aazam476/sentinel/internal/protocol"
)

// FuzzParsers checks the parsers never panic, only return valid text, and
// never offer an update their manager would refuse to install.
func FuzzParsers(f *testing.F) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		f.Fatal(err)
	}
	for _, entry := range entries {
		if data, err := os.ReadFile("testdata/" + entry.Name()); err == nil {
			f.Add(data)
		}
	}
	f.Fuzz(func(t *testing.T, output []byte) {
		lists := map[string][]protocol.PackageUpdate{
			"apt": packages.ParseApt(output), "dnf": packages.ParseDnf(output), "zypper": packages.ParseZypper(output),
			"pacman": packages.ParsePacman(output), "apk": packages.ParseApk(output),
			"softwareupdate": packages.ParseSoftwareUpdate(output), "winget": packages.ParseWinget(output),
		}
		if updates, err := packages.ParseHomebrew(output); err == nil {
			lists["homebrew"] = updates
		}
		if updates, err := packages.ParseWindowsUpdate(output); err == nil {
			lists["windows-update"] = updates
		}
		for manager, list := range lists {
			for _, update := range list {
				if !packages.ValidID(manager, update.ID) {
					t.Errorf("%s offers an update it would refuse: %+v", manager, update)
				}
				if utf8.ValidString(string(output)) && !utf8.ValidString(update.Name) {
					t.Errorf("invalid text from valid input: %+v", update)
				}
			}
		}
	})
}

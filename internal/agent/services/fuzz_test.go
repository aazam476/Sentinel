// SPDX-License-Identifier: Apache-2.0

package services_test

import (
	"os"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/services"
)

func addGoldenFiles(f *testing.F) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		f.Fatal(err)
	}
	for _, entry := range entries {
		if data, err := os.ReadFile("testdata/" + entry.Name()); err == nil && strings.HasSuffix(entry.Name(), ".txt") {
			f.Add(data)
		}
	}
}

// FuzzParsers checks the parsers never panic and never return a name a
// later action would refuse.
func FuzzParsers(f *testing.F) {
	addGoldenFiles(f)
	f.Fuzz(func(t *testing.T, output []byte) {
		for _, file := range services.ParseUnitFiles(output) {
			if file.Name == "" {
				t.Error("empty unit file name")
			}
		}
		for _, unit := range services.ParseUnits(output) {
			if unit.Name == "" {
				t.Error("empty unit name")
			}
		}
		for _, job := range services.ParseLaunchctlList(output) {
			if services.ValidateName("darwin", job.Name) != nil {
				t.Errorf("listed an invalid label %q", job.Name)
			}
		}
		services.ParseDisabled(output)
	})
}

// FuzzValidateName checks that no accepted name could be read as an
// option or break out of a quoted query.
func FuzzValidateName(f *testing.F) {
	for _, seed := range []string{"nginx.service", "-x.service", "com.a.b", "MSSQL$X", "a'b", `a"b`, "a\x00b"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		for _, goos := range []string{"linux", "darwin", "windows"} {
			if services.ValidateName(goos, name) != nil {
				continue
			}
			if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "'\"\x00\n/;&|`") {
				t.Errorf("%s accepted %q", goos, name)
			}
		}
	})
}

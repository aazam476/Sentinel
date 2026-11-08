// SPDX-License-Identifier: Apache-2.0

package stacks_test

import (
	"os"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/stacks"
)

// FuzzParsers checks the compose output parsers never panic, and that a
// project name they return could never leave the stacks folder.
func FuzzParsers(f *testing.F) {
	for _, name := range []string{"compose-5.6-ls.json", "compose-5.6-ps.ndjson", "compose-2.20-ps.json"} {
		data, err := os.ReadFile("testdata/" + name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, output []byte) {
		projects, err := stacks.ParseProjects(output)
		if err == nil {
			for _, project := range projects {
				for _, bad := range []string{"/", "\\", ".."} {
					if len(project.Name) == 0 || strings.Contains(project.Name, bad) {
						t.Errorf("project name %q", project.Name)
					}
				}
			}
		}
		_, _ = stacks.ParseServices(output)
	})
}

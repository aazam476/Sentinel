// SPDX-License-Identifier: Apache-2.0

package release_test

import (
	"encoding/json"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/shared/release"
)

const lock = `{"packages": {
  "": {"name": "sentinel-web"},
  "node_modules/react": {"version": "19.3.1"},
  "node_modules/@base-ui/react": {"version": "1.9.0"},
  "node_modules/vitest": {"version": "4.0.0", "dev": true}
}}`

func TestSEC33_SBOM(t *testing.T) {
	build := &debug.BuildInfo{GoVersion: "go1.27.3", Deps: []*debug.Module{
		{Path: "golang.org/x/crypto", Version: "v0.58.0", Sum: "h1:abc="},
		{Path: "example.com/old", Version: "v1.0.0", Replace: &debug.Module{Path: "example.com/fork", Version: "v1.0.1"}},
	}}
	created := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	input := release.SBOMInput{Name: "sentinel-server", Version: "v1.0.0", Binary: []byte("binary"), Build: build, NPMLock: []byte(lock), Created: created}

	t.Run("lists the Go modules and the web UI's runtime packages, in SPDX 2.3", func(t *testing.T) {
		sbom, err := release.SBOM(input)
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			SPDXVersion string `json:"spdxVersion"`
			Packages    []struct {
				Name        string `json:"name"`
				VersionInfo string `json:"versionInfo"`
			} `json:"packages"`
		}
		if err := json.Unmarshal(sbom, &document); err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, p := range document.Packages {
			names = append(names, p.Name+"@"+p.VersionInfo)
		}
		want := "sentinel-server@v1.0.0 go@go1.27.3 golang.org/x/crypto@v0.58.0 example.com/fork@v1.0.1 @base-ui/react@1.9.0 react@19.3.1"
		if document.SPDXVersion != "SPDX-2.3" || strings.Join(names, " ") != want {
			t.Errorf("got %s %v", document.SPDXVersion, names)
		}
		for _, text := range []string{"pkg:npm/%40base-ui/react@1.9.0", "pkg:golang/golang.org/x/crypto@v0.58.0", "h1:abc=", "2026-11-09T00:00:00Z"} {
			if !strings.Contains(string(sbom), text) {
				t.Errorf("the SBOM has no %q", text)
			}
		}
	})

	t.Run("is the same every time for the same input", func(t *testing.T) {
		first, _ := release.SBOM(input)
		second, _ := release.SBOM(input)
		if string(first) != string(second) {
			t.Error("the SBOM isn't reproducible")
		}
	})

	t.Run("leaves npm out for the agent, and refuses what it can't read", func(t *testing.T) {
		agent := input
		agent.NPMLock = nil
		if sbom, err := release.SBOM(agent); err != nil || strings.Contains(string(sbom), "pkg:npm") {
			t.Errorf("got %v", err)
		}
		agent.NPMLock = []byte("{")
		if _, err := release.SBOM(agent); err == nil {
			t.Error("a broken lockfile was accepted")
		}
		agent.Build = nil
		if _, err := release.SBOM(agent); err == nil {
			t.Error("a binary without build information was accepted")
		}
	})
}

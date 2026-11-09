// SPDX-License-Identifier: Apache-2.0

package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"time"
)

// SBOMInput is what a software bill of materials describes (SEC-33).
type SBOMInput struct {
	// Name and Version name the artifact; Binary is its content.
	Name, Version string
	Binary        []byte
	// Build is the binary's Go build information (debug/buildinfo).
	Build *debug.BuildInfo
	// NPMLock is web/package-lock.json for the server, whose web UI is
	// embedded in it; nil for the agent.
	NPMLock []byte
	// Created is when the SBOM is made; a release uses the commit's time,
	// so the SBOM is reproducible too.
	Created time.Time
}

type spdxDocument struct {
	SPDXVersion       string             `json:"spdxVersion"`
	DataLicense       string             `json:"dataLicense"`
	SPDXID            string             `json:"SPDXID"`
	Name              string             `json:"name"`
	DocumentNamespace string             `json:"documentNamespace"`
	CreationInfo      spdxCreation       `json:"creationInfo"`
	Packages          []spdxPackage      `json:"packages"`
	Relationships     []spdxRelationship `json:"relationships"`
}

type spdxCreation struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

type spdxPackage struct {
	Name             string         `json:"name"`
	SPDXID           string         `json:"SPDXID"`
	VersionInfo      string         `json:"versionInfo"`
	DownloadLocation string         `json:"downloadLocation"`
	FilesAnalyzed    bool           `json:"filesAnalyzed"`
	Checksums        []spdxChecksum `json:"checksums,omitempty"`
	ExternalRefs     []spdxRef      `json:"externalRefs,omitempty"`
}

type spdxChecksum struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"checksumValue"`
}

type spdxRef struct {
	Category string `json:"referenceCategory"`
	Type     string `json:"referenceType"`
	Locator  string `json:"referenceLocator"`
}

type spdxRelationship struct {
	Element string `json:"spdxElementId"`
	Type    string `json:"relationshipType"`
	Related string `json:"relatedSpdxElement"`
}

var spdxIDUnsafe = regexp.MustCompile(`[^A-Za-z0-9.-]`)

func spdxID(kind, name string) string {
	return "SPDXRef-" + kind + "-" + spdxIDUnsafe.ReplaceAllString(name, "-")
}

// SBOM returns an SPDX 2.3 JSON document listing the artifact, the Go
// modules built into it, and, for the server, the npm packages of its web
// UI.
func SBOM(input SBOMInput) ([]byte, error) {
	if input.Build == nil {
		return nil, fmt.Errorf("%s has no Go build information", input.Name)
	}
	digest := sha256.Sum256(input.Binary)
	root := spdxPackage{
		Name: input.Name, SPDXID: "SPDXRef-Package-" + spdxIDUnsafe.ReplaceAllString(input.Name, "-"), VersionInfo: input.Version,
		DownloadLocation: "NOASSERTION", Checksums: []spdxChecksum{{"SHA256", hex.EncodeToString(digest[:])}},
	}
	document := spdxDocument{
		SPDXVersion: "SPDX-2.3", DataLicense: "CC0-1.0", SPDXID: "SPDXRef-DOCUMENT",
		Name: input.Name + "-" + input.Version, DocumentNamespace: "https://github.com/aazam476/sentinel/sbom/" + input.Name + "-" + input.Version,
		CreationInfo:  spdxCreation{Created: input.Created.UTC().Format(time.RFC3339), Creators: []string{"Tool: sentinel-release"}},
		Packages:      []spdxPackage{root},
		Relationships: []spdxRelationship{{"SPDXRef-DOCUMENT", "DESCRIBES", root.SPDXID}},
	}
	add := func(dependency spdxPackage) {
		document.Packages = append(document.Packages, dependency)
		document.Relationships = append(document.Relationships, spdxRelationship{root.SPDXID, "CONTAINS", dependency.SPDXID})
	}
	add(spdxPackage{Name: "go", SPDXID: spdxID("Go", "stdlib"), VersionInfo: input.Build.GoVersion, DownloadLocation: "https://go.dev/dl/"})
	for _, module := range goModules(input.Build) {
		add(module)
	}
	npm, err := npmPackages(input.NPMLock)
	if err != nil {
		return nil, err
	}
	for _, dependency := range npm {
		add(dependency)
	}
	return json.MarshalIndent(document, "", "  ")
}

func goModules(build *debug.BuildInfo) []spdxPackage {
	var modules []spdxPackage
	for _, module := range build.Deps {
		if module.Replace != nil {
			module = module.Replace
		}
		dependency := spdxPackage{
			Name: module.Path, SPDXID: spdxID("Go", module.Path), VersionInfo: module.Version, DownloadLocation: "NOASSERTION",
			ExternalRefs: []spdxRef{{"PACKAGE-MANAGER", "purl", "pkg:golang/" + module.Path + "@" + module.Version}},
		}
		if hash, found := strings.CutPrefix(module.Sum, "h1:"); found {
			dependency.ExternalRefs = append(dependency.ExternalRefs, spdxRef{"OTHER", "go-module-sum", "h1:" + hash})
		}
		modules = append(modules, dependency)
	}
	return modules
}

// npmPackages lists the packages a lockfile installs for production: the
// ones built into the web UI.
func npmPackages(lock []byte) ([]spdxPackage, error) {
	if lock == nil {
		return nil, nil
	}
	var parsed struct {
		Packages map[string]struct {
			Version   string `json:"version"`
			Dev       bool   `json:"dev"`
			Integrity string `json:"integrity"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(lock, &parsed); err != nil {
		return nil, fmt.Errorf("read package-lock.json: %w", err)
	}
	var packages []spdxPackage
	for path, entry := range parsed.Packages {
		at := strings.LastIndex(path, "node_modules/")
		if at < 0 || entry.Dev {
			continue // the project itself, a workspace, or a tool for building it
		}
		name := path[at+len("node_modules/"):]
		purlName := strings.Replace(name, "@", "%40", 1)
		packages = append(packages, spdxPackage{
			Name: name, SPDXID: spdxID("npm", path), VersionInfo: entry.Version, DownloadLocation: "NOASSERTION",
			ExternalRefs: []spdxRef{{"PACKAGE-MANAGER", "purl", "pkg:npm/" + purlName + "@" + entry.Version}},
		})
	}
	slices.SortFunc(packages, func(a, b spdxPackage) int { return strings.Compare(a.SPDXID, b.SPDXID) })
	return packages, nil
}

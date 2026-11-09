// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// DefaultPath is where the server looks for its configuration file when no
// --config flag is given.
const DefaultPath = "/etc/sentinel/server.toml"

// File mirrors server.toml exactly as written by the operator. Every field is
// optional; Resolve fills in defaults and validates the result.
type File struct {
	// Listen is the host:port to accept connections on, for example ":7443".
	Listen string `toml:"listen"`
	// DataDir holds the database, keys and certificates.
	DataDir string `toml:"data_dir"`
	// PublicURL is the address people type into their browser, for example
	// "https://sentinel.example.com". Needed when a proxy changes the host.
	PublicURL string `toml:"public_url"`
	// BasePath serves Sentinel under a sub-path, for example "/sentinel".
	BasePath string `toml:"base_path"`
	// TrustedProxies lists the proxies (IP addresses or CIDR ranges) whose
	// X-Forwarded-* headers are believed.
	TrustedProxies []string `toml:"trusted_proxies"`
	// TLS configures HTTPS.
	TLS TLSFile `toml:"tls"`
	// Files configures the file manager.
	Files FilesFile `toml:"files"`
	// Updates configures where agent builds come from.
	Updates UpdatesFile `toml:"updates"`
}

// UpdatesFile is the [updates] table of server.toml.
type UpdatesFile struct {
	// ReleaseURL is where the latest release's files are downloaded from;
	// "off" turns fetching off (for air-gapped servers, which upload builds
	// instead). The default is the project's GitHub releases.
	ReleaseURL string `toml:"release_url"`
}

// FilesFile is the [files] table of server.toml.
type FilesFile struct {
	// MaxSizeGB is the largest file that can be uploaded or downloaded, in
	// gigabytes (2^30 bytes). 0 means the default, 10.
	MaxSizeGB int64 `toml:"max_size_gb"`
}

// TLSFile is the [tls] table of server.toml.
type TLSFile struct {
	// Mode is "self-signed" (the default), "acme" or "off".
	Mode string `toml:"mode"`
	// ACMEEmail is the contact address given to the ACME certificate
	// authority (Let's Encrypt). Optional.
	ACMEEmail string `toml:"acme_email"`
	// BehindTLSProxy must be true to serve plain HTTP on a non-loopback
	// address. It tells Sentinel that a reverse proxy in front of it handles
	// HTTPS.
	BehindTLSProxy bool `toml:"behind_tls_proxy"`
}

// Load reads the configuration file at path. A missing file isn't an error:
// every setting has a default, so Load returns an empty File. Unknown keys
// are an error, because they are almost always typos that would otherwise be
// silently ignored.
func Load(path string) (File, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the operator chooses which config file to read
	if errors.Is(err, fs.ErrNotExist) {
		return File{}, nil
	}
	if err != nil {
		return File{}, fmt.Errorf("read config file: %w", err)
	}

	file, err := Parse(data)
	if err != nil {
		return File{}, fmt.Errorf("config file %s: %w", path, err)
	}
	return file, nil
}

// Parse decodes the contents of a server.toml file.
func Parse(data []byte) (File, error) {
	var file File
	metadata, err := toml.Decode(string(data), &file)
	if err != nil {
		return File{}, fmt.Errorf("parse TOML: %w", err)
	}

	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, 0, len(undecoded))
		for _, key := range undecoded {
			keys = append(keys, key.String())
		}
		return File{}, fmt.Errorf("unknown setting(s): %s", strings.Join(keys, ", "))
	}

	return file, nil
}

// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/server/config"
)

func TestLoad(t *testing.T) {
	t.Run("returns an empty file when the config file does not exist", func(t *testing.T) {
		file, err := config.Load(filepath.Join(t.TempDir(), "missing.toml"))
		if err != nil {
			t.Fatalf("Load returned %v", err)
		}
		if file.Listen != "" || file.DataDir != "" {
			t.Errorf("Load returned %+v, want an empty File", file)
		}
	})

	t.Run("reads every setting from the file", func(t *testing.T) {
		path := writeConfig(t, `
listen = "0.0.0.0:8443"
data_dir = "/srv/sentinel"
public_url = "https://sentinel.example.com"
base_path = "/sentinel"
trusted_proxies = ["10.0.0.0/8"]

[tls]
mode = "acme"
acme_email = "admin@example.com"
`)
		file, err := config.Load(path)
		if err != nil {
			t.Fatalf("Load returned %v", err)
		}
		want := config.File{
			Listen:         "0.0.0.0:8443",
			DataDir:        "/srv/sentinel",
			PublicURL:      "https://sentinel.example.com",
			BasePath:       "/sentinel",
			TrustedProxies: []string{"10.0.0.0/8"},
			TLS:            config.TLSFile{Mode: "acme", ACMEEmail: "admin@example.com"},
		}
		if file.Listen != want.Listen || file.DataDir != want.DataDir || file.PublicURL != want.PublicURL ||
			file.BasePath != want.BasePath || len(file.TrustedProxies) != 1 || file.TLS != want.TLS {
			t.Errorf("Load returned %+v, want %+v", file, want)
		}
	})

	t.Run("rejects unknown settings so typos are not silently ignored", func(t *testing.T) {
		path := writeConfig(t, "lisen = \":7443\"\n")
		_, err := config.Load(path)
		if err == nil || !strings.Contains(err.Error(), "lisen") {
			t.Errorf("Load error = %v, want one naming the unknown key", err)
		}
	})

	t.Run("rejects a file that is not valid TOML", func(t *testing.T) {
		path := writeConfig(t, "listen = \n")
		if _, err := config.Load(path); err == nil {
			t.Error("Load returned nil for invalid TOML")
		}
	})

	t.Run("returns an error when the path is a directory", func(t *testing.T) {
		if _, err := config.Load(t.TempDir()); err == nil {
			t.Error("Load returned nil for a directory")
		}
	})
}

func TestResolveDefaults(t *testing.T) {
	t.Run("uses safe defaults when nothing is configured", func(t *testing.T) {
		resolved, err := config.File{}.Resolve()
		if err != nil {
			t.Fatalf("Resolve returned %v", err)
		}
		if resolved.ListenAddress != ":7443" {
			t.Errorf("ListenAddress = %q, want :7443", resolved.ListenAddress)
		}
		if resolved.DataDir != filepath.Clean(config.DefaultDataDir) && !filepath.IsAbs(resolved.DataDir) {
			t.Errorf("DataDir = %q, want the absolute default", resolved.DataDir)
		}
		if resolved.TLS.Mode != config.TLSSelfSigned {
			t.Errorf("TLS mode = %q, want self-signed", resolved.TLS.Mode)
		}
		if resolved.BasePath != "/" || resolved.PublicURL != nil || len(resolved.TrustedProxies) != 0 {
			t.Errorf("Resolve returned %+v, want root base path and no public URL or proxies", resolved)
		}
	})

	t.Run("makes a relative data directory absolute", func(t *testing.T) {
		resolved, err := config.File{DataDir: "dev-data"}.Resolve()
		if err != nil {
			t.Fatalf("Resolve returned %v", err)
		}
		if !filepath.IsAbs(resolved.DataDir) || filepath.Base(resolved.DataDir) != "dev-data" {
			t.Errorf("DataDir = %q, want an absolute path ending in dev-data", resolved.DataDir)
		}
	})
}

func TestSEC10_TLSIsOnByDefaultAndPlainHTTPOnlyOnLoopbackOrBehindAProxy(t *testing.T) {
	tests := []struct {
		name    string
		file    config.File
		wantErr string
	}{
		{name: "self-signed TLS on every interface is allowed", file: config.File{Listen: ":7443"}},
		{name: "plain HTTP on 127.0.0.1 is allowed", file: config.File{Listen: "127.0.0.1:7443", TLS: config.TLSFile{Mode: "off"}}},
		{name: "plain HTTP on ::1 is allowed", file: config.File{Listen: "[::1]:7443", TLS: config.TLSFile{Mode: "off"}}},
		{name: "plain HTTP on localhost is allowed", file: config.File{Listen: "localhost:7443", TLS: config.TLSFile{Mode: "off"}}},
		{
			name: "plain HTTP on every interface is allowed behind a TLS proxy",
			file: config.File{Listen: ":7443", TLS: config.TLSFile{Mode: "off", BehindTLSProxy: true}},
		},
		{
			name:    "plain HTTP on every interface is refused",
			file:    config.File{Listen: ":7443", TLS: config.TLSFile{Mode: "off"}},
			wantErr: "plain HTTP is only allowed",
		},
		{
			name:    "plain HTTP on a LAN address is refused",
			file:    config.File{Listen: "192.168.1.10:7443", TLS: config.TLSFile{Mode: "off"}},
			wantErr: "plain HTTP is only allowed",
		},
		{
			name:    "plain HTTP on a host name other than localhost is refused",
			file:    config.File{Listen: "sentinel.lan:7443", TLS: config.TLSFile{Mode: "off"}},
			wantErr: "plain HTTP is only allowed",
		},
		{
			name:    "behind_tls_proxy is refused while Sentinel serves TLS itself",
			file:    config.File{TLS: config.TLSFile{BehindTLSProxy: true}},
			wantErr: "requires tls.mode",
		},
		{
			name:    "an unknown TLS mode is refused",
			file:    config.File{TLS: config.TLSFile{Mode: "insecure"}},
			wantErr: "is not one of",
		},
		{
			name: "ACME with an https public host name is allowed",
			file: config.File{PublicURL: "https://sentinel.example.com", TLS: config.TLSFile{Mode: "acme"}},
		},
		{
			name:    "ACME without a public URL is refused",
			file:    config.File{TLS: config.TLSFile{Mode: "acme"}},
			wantErr: "needs public_url",
		},
		{
			name:    "ACME with an http public URL is refused",
			file:    config.File{PublicURL: "http://sentinel.example.com", TLS: config.TLSFile{Mode: "acme"}},
			wantErr: "https://",
		},
		{
			name:    "ACME with an IP address public URL is refused",
			file:    config.File{PublicURL: "https://203.0.113.5", TLS: config.TLSFile{Mode: "acme"}},
			wantErr: "host name",
		},
		{
			name:    "a listen address without a port is refused",
			file:    config.File{Listen: "127.0.0.1"},
			wantErr: "listen address",
		},
		{
			name:    "a listen address with an empty port is refused",
			file:    config.File{Listen: "127.0.0.1:"},
			wantErr: "has no port",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.file.Resolve()
			assertError(t, err, test.wantErr)
		})
	}
}

func TestResolvePublicURL(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr string
	}{
		{name: "keeps an https origin", value: "https://sentinel.example.com", want: "https://sentinel.example.com"},
		{name: "keeps an explicit port", value: "https://sentinel.example.com:8443", want: "https://sentinel.example.com:8443"},
		{name: "drops a trailing slash", value: "https://sentinel.example.com/", want: "https://sentinel.example.com"},
		{name: "lowercases the host", value: "https://Sentinel.Example.COM", want: "https://sentinel.example.com"},
		{name: "allows http for setups behind a proxy", value: "http://localhost:5173", want: "http://localhost:5173"},
		{name: "rejects other schemes", value: "ftp://sentinel.example.com", wantErr: "must start with"},
		{name: "rejects a URL without a scheme", value: "sentinel.example.com", wantErr: "must start with"},
		{name: "rejects a URL without a host", value: "https://", wantErr: "no host"},
		{name: "rejects a path", value: "https://example.com/sentinel", wantErr: "set base_path instead"},
		{name: "rejects a query", value: "https://example.com/?a=b", wantErr: "query"},
		{name: "rejects credentials", value: "https://someone@example.com", wantErr: "user name"},
		{name: "rejects a URL that does not parse", value: "https://exa mple.com:x", wantErr: "public_url"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolved, err := config.File{PublicURL: test.value}.Resolve()
			assertError(t, err, test.wantErr)
			if test.wantErr == "" && resolved.PublicURL.String() != test.want {
				t.Errorf("PublicURL = %q, want %q", resolved.PublicURL, test.want)
			}
		})
	}
}

func TestResolveBasePath(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr string
	}{
		{name: "defaults to the root", value: "", want: "/"},
		{name: "keeps the root", value: "/", want: "/"},
		{name: "adds a leading slash", value: "sentinel", want: "/sentinel"},
		{name: "removes a trailing slash", value: "/sentinel/", want: "/sentinel"},
		{name: "keeps nested segments", value: "/tools/sentinel", want: "/tools/sentinel"},
		{name: "rejects a parent segment", value: "/tools/../sentinel", wantErr: ". or .."},
		{name: "rejects a current-directory segment", value: "/./sentinel", wantErr: ". or .."},
		{name: "rejects empty segments", value: "/tools//sentinel", wantErr: "may only contain"},
		{name: "rejects spaces", value: "/my sentinel", wantErr: "may only contain"},
		{name: "rejects percent-encoding", value: "/sen%74inel", wantErr: "may only contain"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolved, err := config.File{BasePath: test.value}.Resolve()
			assertError(t, err, test.wantErr)
			if test.wantErr == "" && resolved.BasePath != test.want {
				t.Errorf("BasePath = %q, want %q", resolved.BasePath, test.want)
			}
		})
	}
}

func TestResolveTrustedProxies(t *testing.T) {
	tests := []struct {
		name    string
		values  []string
		want    []netip.Prefix
		wantErr string
	}{
		{name: "accepts a CIDR range", values: []string{"10.0.0.0/8"}, want: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}},
		{name: "masks host bits in a CIDR range", values: []string{"10.1.2.3/8"}, want: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}},
		{name: "turns a single IPv4 address into a /32", values: []string{"192.168.1.2"}, want: []netip.Prefix{netip.MustParsePrefix("192.168.1.2/32")}},
		{name: "turns a single IPv6 address into a /128", values: []string{"fd00::1"}, want: []netip.Prefix{netip.MustParsePrefix("fd00::1/128")}},
		{name: "rejects a malformed CIDR range", values: []string{"10.0.0.0/33"}, wantErr: "trusted_proxies"},
		{name: "rejects a host name", values: []string{"proxy.lan"}, wantErr: "neither an IP address nor a CIDR range"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolved, err := config.File{TrustedProxies: test.values}.Resolve()
			assertError(t, err, test.wantErr)
			if test.wantErr != "" {
				return
			}
			if len(resolved.TrustedProxies) != len(test.want) {
				t.Fatalf("TrustedProxies = %v, want %v", resolved.TrustedProxies, test.want)
			}
			for i := range test.want {
				if resolved.TrustedProxies[i] != test.want[i] {
					t.Errorf("TrustedProxies[%d] = %v, want %v", i, resolved.TrustedProxies[i], test.want[i])
				}
			}
		})
	}
}

func FuzzParseAndResolve(f *testing.F) {
	f.Add("listen = \":7443\"\n[tls]\nmode = \"off\"\n")
	f.Add("public_url = \"https://a.example\"\nbase_path = \"/x/\"\ntrusted_proxies = [\"10.0.0.0/8\"]\n")
	f.Add("[tls]\nmode = \"acme\"\nbehind_tls_proxy = true\n")
	f.Add("listen = \"[::1]:0\"\n")

	f.Fuzz(func(t *testing.T, contents string) {
		file, err := config.Parse([]byte(contents))
		if err != nil {
			return
		}
		resolved, err := file.Resolve()
		if err != nil {
			return
		}
		if resolved.TLS.Mode == config.TLSOff && !resolved.TLS.BehindTLSProxy {
			host, _, splitErr := net.SplitHostPort(resolved.ListenAddress)
			if splitErr != nil {
				t.Fatalf("accepted unparsable listen address %q", resolved.ListenAddress)
			}
			if !isLoopback(host) {
				t.Fatalf("accepted plain HTTP on non-loopback address %q", resolved.ListenAddress)
			}
		}
		if !strings.HasPrefix(resolved.BasePath, "/") || strings.Contains(resolved.BasePath, "..") {
			t.Fatalf("accepted unsafe base path %q", resolved.BasePath)
		}
	})
}

// isLoopback is an independent re-implementation of the loopback rule, so
// the fuzz test doesn't trust the code it is checking.
func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "server.toml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// assertError checks that err is nil when wantErr is empty, and otherwise
// that it mentions wantErr.
func assertError(t *testing.T, err error, wantErr string) {
	t.Helper()
	if wantErr == "" {
		if err != nil {
			t.Fatalf("Resolve returned %v, want nil", err)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("Resolve error = %v, want one containing %q", err, wantErr)
	}
}

func TestResolveMaxFileSize(t *testing.T) {
	tests := []struct {
		name      string
		gigabytes int64
		want      int64
		wantErr   bool
	}{
		{name: "defaults to 10 GB", gigabytes: 0, want: 10 << 30},
		{name: "takes a size in gigabytes", gigabytes: 100, want: 100 << 30},
		{name: "rejects a negative size", gigabytes: -1, wantErr: true},
		{name: "rejects more than 1 TB", gigabytes: 1025, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolved, err := config.File{Files: config.FilesFile{MaxSizeGB: test.gigabytes}}.Resolve()
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, want error %v", err, test.wantErr)
			}
			if err == nil && resolved.MaxFileBytes != test.want {
				t.Errorf("MaxFileBytes = %d, want %d", resolved.MaxFileBytes, test.want)
			}
		})
	}
}

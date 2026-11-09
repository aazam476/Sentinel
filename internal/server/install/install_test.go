// SPDX-License-Identifier: Apache-2.0

package install_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/server/install"
	"github.com/aazam476/sentinel/internal/server/updates"
	"github.com/aazam476/sentinel/internal/shared/release"
)

func releaseKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	text, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "release", "development.key"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := release.ParsePrivateKey(string(text))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// fakeBuilds hosts one build, for one system, from files on disk.
type fakeBuilds struct {
	os, arch, path string
}

func (f fakeBuilds) Newest(osName, arch string) (updates.Build, string, error) {
	if osName != f.os || arch != f.arch {
		return updates.Build{}, "", errors.New("no build")
	}
	return updates.Build{Version: "v9.9.9", OS: osName, Arch: arch}, f.path, nil
}

// echoAgent builds the stand-in agent that prints its arguments.
func echoAgent(t *testing.T, folder, goos string) []byte {
	t.Helper()
	output := filepath.Join(folder, "echoagent")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", output, "../../testutil/echoagent")
	build.Env = append(os.Environ(), "GOOS="+goos, "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the stand-in agent: %v\n%s", err, out)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

// host writes a build and its signature (by key, describing content as
// signedOS), serving tampered bytes in its place when told.
func host(t *testing.T, key ed25519.PrivateKey, content []byte, signedOS string, served []byte) fakeBuilds {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sentinel-agent_v9.9.9")
	manifest, err := release.ManifestFor(bytes.NewReader(content), "sentinel-agent", "v9.9.9", signedOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := release.Sign(manifest, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, served, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".sig", signature, 0o600); err != nil {
		t.Fatal(err)
	}
	return fakeBuilds{os: signedOS, arch: runtime.GOARCH, path: path}
}

func serve(builds fakeBuilds, keys []ed25519.PublicKey, tls bool) *httptest.Server {
	mux := http.NewServeMux()
	install.New(builds, keys).Routes(mux)
	if tls {
		return httptest.NewTLSServer(mux)
	}
	return httptest.NewServer(mux)
}

// pin is the TLS fingerprint the Add machine page gives for a server.
func pin(server *httptest.Server) string {
	certificate, _ := x509.ParseCertificate(server.TLS.Certificates[0].Certificate[0])
	sum := sha256.Sum256(certificate.RawSubjectPublicKeyInfo)
	return "SHA256:" + base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestSEC34_ShellInstallerVerifiesBeforeInstalling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("agent.sh is for Linux and macOS")
	}
	for _, tool := range []string{"sh", "curl", "openssl"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s isn't installed", tool)
		}
	}
	key := releaseKey(t)
	keys := []ed25519.PublicKey{key.Public().(ed25519.PublicKey)}
	agent := echoAgent(t, t.TempDir(), runtime.GOOS)
	_, otherKey, _ := ed25519.GenerateKey(nil)

	run := func(t *testing.T, builds fakeBuilds, extra ...string) (string, error) {
		t.Helper()
		server := serve(builds, keys, true)
		defer server.Close()
		script := filepath.Join(t.TempDir(), "agent.sh")
		response, err := server.Client().Get(server.URL + "/install/agent.sh") //nolint:noctx // a test against a local server
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }()
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(response.Body)
		if err := os.WriteFile(script, body.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		args := append([]string{script, "--server", server.URL, "--token", "tok", "--fingerprint", "SHA256:server",
			"--tls-fingerprint", pin(server), "--dry-run"}, extra...)
		output, err := exec.CommandContext(t.Context(), "sh", args...).CombinedOutput()
		return string(output), err
	}

	t.Run("checks the signature, then runs the build's install with the command's flags", func(t *testing.T) {
		output, err := run(t, host(t, key, agent, runtime.GOOS, agent))
		if err != nil {
			t.Fatalf("%v\n%s", err, output)
		}
		for _, want := range []string{"its signature checks out", "echoagent ran: install --server https://", "--token tok --fingerprint SHA256:server --tls-fingerprint SHA256:", "--dry-run"} {
			if !strings.Contains(output, want) {
				t.Errorf("no %q in:\n%s", want, output)
			}
		}
	})

	refusals := []struct {
		name   string
		builds func() fakeBuilds
	}{
		{"a build changed after signing", func() fakeBuilds { return host(t, key, agent, runtime.GOOS, append(bytes.Clone(agent), 0)) }},
		{"a build signed by another key", func() fakeBuilds { return host(t, otherKey, agent, runtime.GOOS, agent) }},
	}
	for _, tc := range refusals {
		t.Run("refuses "+tc.name+" and runs nothing", func(t *testing.T) {
			output, err := run(t, tc.builds())
			if err == nil || !strings.Contains(output, "doesn't check out") || strings.Contains(output, "echoagent ran") {
				t.Errorf("%v\n%s", err, output)
			}
		})
	}

	t.Run("refuses a server whose certificate isn't the one pinned", func(t *testing.T) {
		builds := host(t, key, agent, runtime.GOOS, agent)
		output, err := run(t, builds, "--tls-fingerprint", "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
		if err == nil || strings.Contains(output, "echoagent ran") {
			t.Errorf("%v\n%s", err, output)
		}
	})

	t.Run("says when the server has no build for the machine", func(t *testing.T) {
		output, err := run(t, fakeBuilds{os: "plan9", arch: "386"})
		if err == nil || !strings.Contains(output, "Updates page") {
			t.Errorf("%v\n%s", err, output)
		}
	})
}

func TestSEC34_PowerShellInstallerVerifiesBeforeInstalling(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("agent.ps1 is for Windows")
	}
	key := releaseKey(t)
	keys := []ed25519.PublicKey{key.Public().(ed25519.PublicKey)}
	agent := echoAgent(t, t.TempDir(), "windows")
	run := func(t *testing.T, builds fakeBuilds) (string, error) {
		t.Helper()
		server := serve(builds, keys, false)
		defer server.Close()
		response, err := http.Get(server.URL + "/install/agent.ps1") //nolint:noctx // a test against a local server
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }()
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(response.Body)
		script := filepath.Join(t.TempDir(), "agent.ps1")
		if err := os.WriteFile(script, body.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		output, err := exec.CommandContext(t.Context(), "powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script,
			"-Server", server.URL, "-Token", "tok", "-Fingerprint", "SHA256:server", "-DryRun").CombinedOutput()
		return string(output), err
	}

	t.Run("checks the signature, then runs the build's install with the command's flags", func(t *testing.T) {
		output, err := run(t, host(t, key, agent, "windows", agent))
		if err != nil || !strings.Contains(output, "its signature checks out") ||
			!strings.Contains(output, "echoagent ran: install --server http://") || !strings.Contains(output, "--dry-run") {
			t.Errorf("%v\n%s", err, output)
		}
	})

	t.Run("refuses a build changed after signing and runs nothing", func(t *testing.T) {
		output, err := run(t, host(t, key, agent, "windows", append(bytes.Clone(agent), 0)))
		if err == nil || !strings.Contains(output, "doesn't check out") || strings.Contains(output, "echoagent ran") {
			t.Errorf("%v\n%s", err, output)
		}
	})
}

func TestScripts(t *testing.T) {
	key := releaseKey(t)
	builds := fakeBuilds{os: "linux", arch: "amd64", path: filepath.Join(t.TempDir(), "missing")}
	server := serve(builds, []ed25519.PublicKey{key.Public().(ed25519.PublicKey)}, false)
	defer server.Close()
	for _, path := range []string{"/install/agent.sh", "/install/agent.ps1"} {
		response, err := http.Get(server.URL + path) //nolint:noctx // a test against a local server
		if err != nil {
			t.Fatal(err)
		}
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(response.Body)
		_ = response.Body.Close()
		if !strings.Contains(body.String(), release.FormatPublicKey(key.Public().(ed25519.PublicKey))) || strings.Contains(body.String(), "@RELEASE_KEYS@") {
			t.Errorf("%s doesn't carry the trusted key", path)
		}
	}
	for _, path := range []string{"/install/agent/linux/amd64/other", "/install/agent/windows/arm64/sentinel-agent"} {
		response, err := http.Get(server.URL + path) //nolint:noctx // a test against a local server
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d", path, response.StatusCode)
		}
	}
	if len(install.New(builds, nil).ScriptKeysForTest()) == 0 {
		t.Error("no compiled-in keys")
	}
}

func TestSEC34_ServerInstallerVerifiesBeforeInstalling(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("install-server.sh is for Linux")
	}
	for _, tool := range []string{"sh", "curl", "openssl"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s isn't installed", tool)
		}
	}
	key := releaseKey(t)
	template, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "install-server.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "install-server.sh")
	keyText := release.FormatPublicKey(key.Public().(ed25519.PublicKey))
	if err := os.WriteFile(script, bytes.Replace(template, []byte("@RELEASE_KEYS@"), []byte(keyText), 1), 0o600); err != nil { //nolint:gosec // G703: a temporary file of the test's own
		t.Fatal(err)
	}
	run := func(t *testing.T, served []byte, signedBy ed25519.PrivateKey) (string, error) {
		t.Helper()
		binary := []byte("\x7fELF pretend server")
		name := "sentinel-server_v9.9.9_linux_" + runtime.GOARCH
		manifest, _ := release.ManifestFor(bytes.NewReader(binary), "sentinel-server", "v9.9.9", "linux", runtime.GOARCH)
		signature, _ := release.Sign(manifest, signedBy)
		digest := sha256.Sum256(served)
		files := map[string][]byte{
			"/SHA256SUMS":       []byte(hexString(digest[:]) + "  " + name + "\n"),
			"/" + name:          served,
			"/" + name + ".sig": signature,
		}
		releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			content, found := files[r.URL.Path]
			if !found {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(content)
		}))
		defer releases.Close()
		output, err := exec.CommandContext(t.Context(), "sh", script, "--release-url", releases.URL, "--dry-run").CombinedOutput()
		return string(output), err
	}

	t.Run("checks the server's signature before describing the install", func(t *testing.T) {
		output, err := run(t, []byte("\x7fELF pretend server"), key)
		if err != nil || !strings.Contains(output, "sentinel-server v9.9.9 for linux/") || !strings.Contains(output, "Would create the sentinel user") {
			t.Errorf("%v\n%s", err, output)
		}
	})

	_, other, _ := ed25519.GenerateKey(nil)
	for name, tc := range map[string]struct {
		served []byte
		key    ed25519.PrivateKey
	}{
		"a changed binary":               {[]byte("\x7fELF tampered"), key},
		"a binary signed by another key": {[]byte("\x7fELF pretend server"), other},
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			output, err := run(t, tc.served, tc.key)
			if err == nil || !strings.Contains(output, "doesn't check out") {
				t.Errorf("%v\n%s", err, output)
			}
		})
	}
}

func hexString(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&15])
	}
	return string(out)
}

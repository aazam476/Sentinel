// SPDX-License-Identifier: Apache-2.0

package policy_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/policy"
)

func writePolicy(t *testing.T, text string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.toml")
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil { // WriteFile's mode is reduced by the umask
		t.Fatal(err)
	}
	return path
}

func TestSEC22_ThePolicyFileSwitchesCapabilitiesOff(t *testing.T) {
	path := writePolicy(t, "[capabilities]\nterminal = false\ndesktop = false\ndocker = true\n", 0o600)
	loaded, err := policy.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range policy.Capabilities() {
		want := name != policy.Terminal && name != policy.Desktop
		if loaded.Allows(name) != want {
			t.Errorf("Allows(%s) = %v, want %v", name, !want, want)
		}
	}
	for _, capability := range loaded.Report() {
		disabled := capability.Name == policy.Terminal || capability.Name == policy.Desktop
		if capability.Enabled == disabled || (disabled && capability.Reason != "Disabled by local policy") {
			t.Errorf("reported %+v", capability)
		}
	}
}

func TestSEC22_EverythingIsAllowedWithoutAPolicyFile(t *testing.T) {
	loaded, err := policy.Load(filepath.Join(t.TempDir(), "policy.toml"))
	if err != nil {
		t.Fatal(err)
	}
	report := loaded.Report()
	if len(report) != len(policy.Capabilities()) {
		t.Fatalf("report = %+v", report)
	}
	for _, capability := range report {
		if !capability.Enabled || capability.Reason != "" {
			t.Errorf("%+v, want enabled", capability)
		}
	}
}

func TestSEC22_APolicyFileThatCantBeTrustedStopsTheAgent(t *testing.T) {
	tests := []struct {
		name string
		text string
		mode os.FileMode
		want string
	}{
		{"a misspelt capability", "[capabilities]\nteminal = false\n", 0o600, "unknown capability"},
		{"an unknown section", "[capabilites]\nterminal = false\n", 0o600, "unknown setting"}, //nolint:misspell // the misspelling is the test
		{"an unknown top-level setting", "allow_everything = true\n", 0o600, "unknown setting"},
		{"a value that isn't true or false", "[capabilities]\nterminal = \"no\"\n", 0o600, "not valid TOML"},
		{"text that isn't TOML", "[capabilities\n", 0o600, "not valid TOML"},
	}
	if runtime.GOOS != "windows" {
		tests = append(tests, struct {
			name string
			text string
			mode os.FileMode
			want string
		}{"a file other users can change", "[capabilities]\nterminal = false\n", 0o666, "chmod go-w"})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := policy.Load(writePolicy(t, test.text, test.mode))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("err = %v, want %q", err, test.want)
			}
		})
	}

	t.Run("a file far larger than any real policy", func(t *testing.T) {
		big := "# " + strings.Repeat("x", 70<<10) + "\n"
		if _, err := policy.Load(writePolicy(t, big, 0o600)); err == nil {
			t.Error("Load accepted a 70 KiB file")
		}
	})

	t.Run("a path that can't be read", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "policy.toml")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := policy.Load(dir); err == nil {
			t.Error("Load accepted a directory")
		}
	})

	t.Run("a path whose parent isn't a directory", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows reports such a path as missing, which allows everything as intended")
		}
		file := writePolicy(t, "", 0o600)
		if _, err := policy.Load(filepath.Join(file, "policy.toml")); err == nil {
			t.Error("Load returned nil")
		}
	})
}

func TestParse(t *testing.T) {
	t.Run("an empty file allows everything", func(t *testing.T) {
		loaded, err := policy.Parse("# nothing switched off\n")
		if err != nil || !loaded.Allows(policy.Power) {
			t.Errorf("err = %v, power allowed = %v", err, loaded.Allows(policy.Power))
		}
	})
}

func FuzzParse(f *testing.F) {
	for _, seed := range []string{"", "[capabilities]\nterminal = false\n", "[capabilities]\nx = 1", "a = [", "[[capabilities]]"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		loaded, err := policy.Parse(text)
		if err != nil {
			return
		}
		// Anything accepted must report exactly the known capabilities.
		if len(loaded.Report()) != len(policy.Capabilities()) {
			t.Errorf("%q gave report %+v", text, loaded.Report())
		}
	})
}

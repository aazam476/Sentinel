// SPDX-License-Identifier: Apache-2.0

package scripts

import (
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
)

func TestFind(t *testing.T) {
	installed := func(programs ...string) func(string) (string, error) {
		return func(program string) (string, error) {
			for _, have := range programs {
				if have == program {
					return "/usr/bin/" + program, nil
				}
			}
			return "", exec.ErrNotFound
		}
	}
	cases := []struct {
		name, goos, language string
		have                 []string
		want                 string
		wantErr              error
	}{
		{"runs Python 3 where python3 is installed", "linux", protocol.LanguagePython, []string{"python3", "python"}, "python3", nil},
		{"falls back to python", "darwin", protocol.LanguagePython, []string{"python"}, "python", nil},
		{"prefers PowerShell 7 on Windows", "windows", protocol.LanguagePowerShell, []string{"pwsh", "powershell"}, "pwsh", nil},
		{"uses Windows PowerShell without PowerShell 7", "windows", protocol.LanguagePowerShell, []string{"powershell"}, "powershell", nil},
		{"says when nothing can run the language", "linux", protocol.LanguagePowerShell, nil, "", ErrNoInterpreter},
		{"has no sh on Windows", "windows", protocol.LanguageSh, []string{"sh"}, "", ErrLanguage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &Runner{goos: tc.goos, lookPath: installed(tc.have...)}
			found, _, err := runner.find(tc.language)
			if !errors.Is(err, tc.wantErr) || (err == nil && found.program != tc.want) {
				t.Errorf("got %q, %v", found.program, err)
			}
		})
	}
}

func TestTimeoutOf(t *testing.T) {
	cases := map[int]time.Duration{0: 10 * time.Minute, -5: 10 * time.Minute, 30: 30 * time.Second, 1 << 30: 24 * time.Hour}
	for seconds, want := range cases {
		if got := timeoutOf(seconds); got != want {
			t.Errorf("%d: got %v, want %v", seconds, got, want)
		}
	}
}

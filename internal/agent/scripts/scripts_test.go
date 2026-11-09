// SPDX-License-Identifier: Apache-2.0

package scripts_test

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/scripts"
	"github.com/aazam476/sentinel/internal/protocol"
)

// output collects a script's output; Run writes to it from another goroutine.
type output struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (o *output) Write(data []byte) (int, error) {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	return o.buffer.Write(data)
}

func (o *output) String() string {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	return o.buffer.String()
}

// shell is the language this machine's tests run scripts in.
func shell(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("powershell"); err != nil {
			t.Skip("PowerShell isn't installed")
		}
		return protocol.LanguagePowerShell
	}
	return protocol.LanguageSh
}

func TestRun(t *testing.T) {
	cases := []struct {
		name       string
		unix, ps   string
		wantOutput string
		wantExit   int
	}{
		{"passes on output and standard error in the order written, and the exit status",
			"echo one; echo two >&2; echo three; exit 3",
			"Write-Output one; [Console]::Error.WriteLine('two'); Write-Output three; exit 3",
			"one\ntwo\nthree\n", 3},
		{"reports success", "echo ok", "Write-Output ok", "ok\n", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			language := shell(t)
			body := tc.unix
			if language == protocol.LanguagePowerShell {
				body = tc.ps
			}
			out := &output{}
			result, err := scripts.New(runtime.GOOS).Run(t.Context(), protocol.ScriptRunPayload{Language: language, Body: body, TimeoutSeconds: 30}, out)
			if err != nil {
				t.Fatal(err)
			}
			got := strings.ReplaceAll(out.String(), "\r\n", "\n")
			if got != tc.wantOutput || result.ExitCode != tc.wantExit || result.TimedOut || result.Error != "" {
				t.Errorf("got %q and %+v, want %q and exit %d", got, result, tc.wantOutput, tc.wantExit)
			}
		})
	}

	t.Run("stops a script at its time limit, with what it started", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows ends only the script's own process (ADR 0024)")
		}
		out := &output{}
		started := time.Now()
		result, err := scripts.New(runtime.GOOS).Run(t.Context(), protocol.ScriptRunPayload{
			Language: protocol.LanguageSh, Body: "echo started; sleep 60 & wait", TimeoutSeconds: 1,
		}, out)
		if err != nil {
			t.Fatal(err)
		}
		if !result.TimedOut || result.ExitCode != -1 || out.String() != "started\n" {
			t.Errorf("got %+v with %q", result, out.String())
		}
		if elapsed := time.Since(started); elapsed > 10*time.Second {
			t.Errorf("took %v: the script's sleep outlived it", elapsed)
		}
	})

	t.Run("stops a script when asked", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("covered on Unix; Windows has no sh")
		}
		ctx, cancel := context.WithCancel(t.Context())
		out := &output{}
		done := make(chan protocol.ScriptResult)
		go func() {
			result, _ := scripts.New(runtime.GOOS).Run(ctx, protocol.ScriptRunPayload{Language: protocol.LanguageSh, Body: "echo ready; sleep 60"}, out)
			done <- result
		}()
		for !strings.Contains(out.String(), "ready") {
			runtime.Gosched()
		}
		cancel()
		if result := <-done; result.TimedOut || result.Error != "the script was stopped" {
			t.Errorf("got %+v", result)
		}
	})

	t.Run("refuses a language it doesn't know", func(t *testing.T) {
		_, err := scripts.New(runtime.GOOS).Run(t.Context(), protocol.ScriptRunPayload{Language: "cobol", Body: "x"}, &output{})
		if !errors.Is(err, scripts.ErrLanguage) {
			t.Errorf("got %v", err)
		}
	})
}

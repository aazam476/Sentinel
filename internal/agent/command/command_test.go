// SPDX-License-Identifier: Apache-2.0

package command_test

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/command"
)

func TestExec(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	t.Run("returns a program's output, passing arguments without a shell", func(t *testing.T) {
		output, err := command.Exec{}.Run(t.Context(), "sh", "-c", `printf '%s' "$0"`, "$(not run); and spaces")
		if err != nil || string(output) != "$(not run); and spaces" {
			t.Errorf("output = %q (%v)", output, err)
		}
	})

	t.Run("reports a failing program with its error output", func(t *testing.T) {
		_, err := command.Exec{}.Run(t.Context(), "sh", "-c", "echo 'Unit nope.service not found.' >&2; exit 5")
		if err == nil || !strings.Contains(err.Error(), "Unit nope.service not found.") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("reports a failing program without error output", func(t *testing.T) {
		if _, err := (command.Exec{}).Run(t.Context(), "sh", "-c", "exit 3"); err == nil || !strings.Contains(err.Error(), "exit status 3") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("reports a program that isn't installed", func(t *testing.T) {
		if _, err := (command.Exec{}).Run(t.Context(), "sentinel-no-such-program"); !errors.Is(err, command.ErrNotFound) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("cuts off very long output", func(t *testing.T) {
		output, err := command.Exec{}.Run(t.Context(), "sh", "-c", "head -c 20000000 /dev/zero")
		if err != nil || len(output) != 16<<20 {
			t.Errorf("read %d bytes (%v)", len(output), err)
		}
	})
}

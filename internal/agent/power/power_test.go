// SPDX-License-Identifier: Apache-2.0

package power_test

import (
	"errors"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/power"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
	"github.com/aazam476/sentinel/internal/testutil/fakerunner"
)

func TestSchedule(t *testing.T) {
	cases := []struct {
		goos    string
		action  power.Action
		command string
	}{
		{"linux", power.Reboot, "shutdown -r now"},
		{"linux", power.Shutdown, "shutdown -P now"},
		{"darwin", power.Reboot, "shutdown -r now"},
		{"darwin", power.Shutdown, "shutdown -h now"},
		{"windows", power.Reboot, "shutdown /r /t 0"},
		{"windows", power.Shutdown, "shutdown /s /t 0"},
	}
	for _, c := range cases {
		t.Run(c.goos+" "+string(c.action)+" runs "+c.command+" after the delay", func(t *testing.T) {
			runner := fakerunner.New(t).Expect(c.command, "", nil)
			clock := fakeclock.New(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC))
			done, err := power.New(c.goos, runner, clock).Schedule(c.action)
			if err != nil {
				t.Fatal(err)
			}
			if err := clock.WaitForTimers(t.Context(), 1); err != nil {
				t.Fatal(err)
			}
			if calls := runner.Calls(); len(calls) != 0 {
				t.Errorf("ran %v before the delay", calls)
			}
			clock.Advance(power.Delay)
			if err := <-done; err != nil {
				t.Error(err)
			}
		})
	}

	t.Run("passes on a failed command", func(t *testing.T) {
		runner := fakerunner.New(t).Expect("shutdown -r now", "", errors.New("exit status 1: must be root"))
		clock := fakeclock.New(time.Now())
		done, err := power.New("linux", runner, clock).Schedule(power.Reboot)
		if err != nil {
			t.Fatal(err)
		}
		if err := clock.WaitForTimers(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		clock.Advance(power.Delay)
		if err := <-done; err == nil {
			t.Error("no error")
		}
	})

	t.Run("refuses unknown actions and systems", func(t *testing.T) {
		if _, err := power.New("linux", fakerunner.New(t), fakeclock.New(time.Now())).Schedule("hibernate"); err == nil {
			t.Error("unknown action accepted")
		}
		if _, err := power.New("plan9", fakerunner.New(t), fakeclock.New(time.Now())).Schedule(power.Reboot); !errors.Is(err, power.ErrUnsupported) {
			t.Errorf("err = %v", err)
		}
	})
}

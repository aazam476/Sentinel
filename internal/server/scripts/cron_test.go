// SPDX-License-Identifier: Apache-2.0

package scripts_test

import (
	"errors"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/scripts"
)

func TestCronNext(t *testing.T) {
	// Saturday 8 November 2026, 12:34 UTC.
	from := time.Date(2026, 11, 8, 12, 34, 56, 0, time.UTC)
	cases := []struct {
		name, cron string
		want       time.Time
	}{
		{"every minute runs at the next whole minute", "* * * * *", time.Date(2026, 11, 8, 12, 35, 0, 0, time.UTC)},
		{"every 15 minutes", "*/15 * * * *", time.Date(2026, 11, 8, 12, 45, 0, 0, time.UTC)},
		{"every weekday at 03:00 skips the weekend", "0 3 * * 1-5", time.Date(2026, 11, 9, 3, 0, 0, 0, time.UTC)},
		{"named weekdays and months", "30 2 * jan,nov mon", time.Date(2026, 11, 9, 2, 30, 0, 0, time.UTC)},
		{"7 is Sunday", "0 6 * * 7", time.Date(2026, 11, 15, 6, 0, 0, 0, time.UTC)},
		{"the first of the month", "@monthly", time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)},
		{"either the 13th or a Friday, as cron means it", "0 0 13 * fri", time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)},
		{"from 5, every 20 minutes", "5/20 * * * *", time.Date(2026, 11, 8, 12, 45, 0, 0, time.UTC)},
		{"a list and a range with a step", "0,30 9-17/4 * * *", time.Date(2026, 11, 8, 13, 0, 0, 0, time.UTC)},
		{"leap day, years ahead", "0 0 29 2 *", time.Date(2028, 2, 29, 0, 0, 0, 0, time.UTC)},
		{"never: February 30th", "0 0 30 2 *", time.Time{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := scripts.ParseCron(tc.cron)
			if err != nil {
				t.Fatal(err)
			}
			if got := parsed.Next(from, time.UTC); !got.Equal(tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("runs in the schedule's own time zone", func(t *testing.T) {
		newYork, err := time.LoadLocation("America/New_York")
		if err != nil {
			t.Skip("no time zone data")
		}
		parsed, _ := scripts.ParseCron("0 3 * * *")
		// 03:00 in New York on 9 November is 08:00 UTC.
		if got := parsed.Next(from, newYork); !got.Equal(time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)) {
			t.Errorf("got %v", got.UTC())
		}
	})
}

func TestCronRefusals(t *testing.T) {
	for _, text := range []string{"", "* * * *", "* * * * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *",
		"* * * * 8", "5-1 * * * *", "*/0 * * * *", "a * * * *", "1-2-3 * * * *", "@sometimes", "*/100 * * * *"} {
		t.Run("refuses "+text, func(t *testing.T) {
			if _, err := scripts.ParseCron(text); !errors.Is(err, scripts.ErrCron) {
				t.Errorf("got %v", err)
			}
		})
	}
}

// FuzzCron checks that no schedule text makes the parser panic, and that
// whenever a schedule's next time is found, it is after the start, on a
// whole minute, and the time after it comes later still.
func FuzzCron(f *testing.F) {
	for _, seed := range []string{"* * * * *", "*/7 1-5 */2 jan-mar sun,sat", "@yearly", "0 0 31 2 *", "59 23 31 12 7"} {
		f.Add(seed)
	}
	from := time.Date(2026, 11, 8, 12, 34, 56, 0, time.UTC)
	f.Fuzz(func(t *testing.T, text string) {
		parsed, err := scripts.ParseCron(text)
		if err != nil {
			return
		}
		next := parsed.Next(from, time.UTC)
		if next.IsZero() {
			return
		}
		if !next.After(from) || next.Second() != 0 {
			t.Errorf("%q: next %v", text, next)
		}
		if again := parsed.Next(next, time.UTC); !again.IsZero() && !again.After(next) {
			t.Errorf("%q: %v then %v", text, next, again)
		}
	})
}

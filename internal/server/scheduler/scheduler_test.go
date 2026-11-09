// SPDX-License-Identifier: Apache-2.0

package scheduler_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/scheduler"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

func TestRun(t *testing.T) {
	t.Run("runs each job on its own interval", func(t *testing.T) {
		clock := fakeclock.New(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC))
		minutely, hourly := make(chan struct{}, 100), make(chan struct{}, 100)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			scheduler.Run(ctx, clock, slog.New(slog.DiscardHandler),
				scheduler.Job{Name: "minutely", Interval: time.Minute, Run: signal(minutely)},
				scheduler.Job{Name: "hourly", Interval: time.Hour, Run: signal(hourly)})
			close(done)
		}()

		for range 60 {
			waitForTimers(t, clock, 2)
			clock.Advance(time.Minute)
			<-minutely
		}
		<-hourly
		waitForTimers(t, clock, 2)
		if len(minutely) != 0 || len(hourly) != 0 {
			t.Errorf("extra runs: %d minutely, %d hourly", len(minutely), len(hourly))
		}
		cancel()
		<-done
	})

	t.Run("logs a failed run and runs the job again", func(t *testing.T) {
		clock := fakeclock.New(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC))
		var log syncBuffer
		runs := make(chan struct{}, 10)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			scheduler.Run(ctx, clock, slog.New(slog.NewTextHandler(&log, nil)), scheduler.Job{
				Name: "rollup", Interval: time.Minute,
				Run: func(context.Context) error { runs <- struct{}{}; return errors.New("database is locked") },
			})
			close(done)
		}()
		for range 2 {
			waitForTimers(t, clock, 1)
			clock.Advance(time.Minute)
			<-runs
		}
		// The timer is set again only after the failure is logged.
		waitForTimers(t, clock, 1)
		cancel()
		<-done
		if got := log.String(); strings.Count(got, "job=rollup") != 2 || !strings.Contains(got, "database is locked") {
			t.Errorf("log = %q, want both failures", got)
		}
	})

	t.Run("returns at once with no jobs", func(t *testing.T) {
		scheduler.Run(t.Context(), fakeclock.New(time.Time{}), slog.New(slog.DiscardHandler))
	})
}

func signal(channel chan<- struct{}) func(context.Context) error {
	return func(context.Context) error {
		channel <- struct{}{}
		return nil
	}
}

func waitForTimers(t *testing.T, clock *fakeclock.Clock, count int) {
	t.Helper()
	if err := clock.WaitForTimers(t.Context(), count); err != nil {
		t.Fatal(err)
	}
}

// syncBuffer is a bytes.Buffer safe for the logger's goroutines.
type syncBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (b *syncBuffer) Write(data []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.Write(data)
}

func (b *syncBuffer) String() string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.String()
}

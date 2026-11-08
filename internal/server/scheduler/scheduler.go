// SPDX-License-Identifier: Apache-2.0

package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/aazam476/sentinel/internal/shared/clock"
)

// Clock makes the timers jobs wait on.
type Clock interface {
	NewTimer(duration time.Duration) clock.Timer
}

// Job is a task that runs every Interval.
type Job struct {
	// Name identifies the job in the log.
	Name     string
	Interval time.Duration
	Run      func(ctx context.Context) error
}

// Run runs every job on its interval until ctx ends, then waits for any
// run in progress to finish.
func Run(ctx context.Context, clock Clock, logger *slog.Logger, jobs ...Job) {
	var running sync.WaitGroup
	for _, job := range jobs {
		running.Go(func() { runJob(ctx, clock, logger, job) })
	}
	running.Wait()
}

func runJob(ctx context.Context, clock Clock, logger *slog.Logger, job Job) {
	timer := clock.NewTimer(job.Interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C():
		}
		if err := job.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Error("scheduled job failed; it will run again", "job", job.Name, "error", err)
		}
		timer.Reset(job.Interval)
	}
}

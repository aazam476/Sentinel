// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/aazam476/sentinel/internal/agent/command"
)

// ErrNotReadOnly means a zfs or zpool command that could change something
// was refused.
var ErrNotReadOnly = errors.New("only read-only zfs and zpool commands are allowed")

// readOnlySubcommands are the zfs and zpool subcommands that never change
// anything.
func readOnlySubcommands() map[string]bool {
	return map[string]bool{"list": true, "status": true, "get": true}
}

// readOnly runs commands through runner, refusing zfs and zpool
// subcommands that aren't on the read-only list.
type readOnly struct {
	runner command.Runner
}

// Run implements command.Runner.
func (r readOnly) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == "zfs" || name == "zpool" {
		if len(args) == 0 || !readOnlySubcommands()[args[0]] {
			return nil, fmt.Errorf("%s %v: %w", name, args, ErrNotReadOnly)
		}
	}
	output, err := r.runner.Run(ctx, name, args...)
	if err != nil {
		return output, fmt.Errorf("run %s: %w", name, err)
	}
	return output, nil
}

// ReadOnlyRunner wraps runner so that it refuses any zfs or zpool command
// that could change something.
func ReadOnlyRunner(runner command.Runner) command.Runner {
	return readOnly{runner: runner}
}

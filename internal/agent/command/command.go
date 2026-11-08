// SPDX-License-Identifier: Apache-2.0

package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// maxOutput is the most output the agent reads from one program. Anything
// longer is cut off, so a runaway program can't exhaust the agent's memory.
const maxOutput = 16 << 20

// maxErrorText is how much of a program's error output an error carries.
const maxErrorText = 4 << 10

// ErrNotFound means the program isn't installed.
var ErrNotFound = errors.New("program not found")

// Runner runs an external program and returns its standard output.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Exec runs programs for real. The program is run directly, never through
// a shell, so arguments are passed exactly as given.
type Exec struct{}

// Run runs name with args. A program that exits with an error returns an
// error carrying the start of what it wrote to standard error.
func (Exec) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, ErrNotFound)
	}
	program := exec.CommandContext(ctx, path, args...) //nolint:gosec // G204: callers pass fixed programs and validated arguments, and no shell is involved
	var stdout, stderr limitedBuffer
	stdout.limit, stderr.limit = maxOutput, maxErrorText
	program.Stdout, program.Stderr = &stdout, &stderr
	if err := program.Run(); err != nil {
		if text := strings.TrimSpace(stderr.String()); text != "" {
			return stdout.Bytes(), fmt.Errorf("%s: %w: %s", name, err, text)
		}
		return stdout.Bytes(), fmt.Errorf("%s: %w", name, err)
	}
	return stdout.Bytes(), nil
}

// limitedBuffer keeps the first limit bytes written to it and drops the
// rest. The buffer isn't embedded: its ReadFrom would bypass the limit.
type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	if room := b.limit - b.buffer.Len(); room > 0 {
		b.buffer.Write(data[:min(len(data), room)])
	}
	return len(data), nil // keep the program running; the rest is dropped
}

func (b *limitedBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *limitedBuffer) String() string { return b.buffer.String() }

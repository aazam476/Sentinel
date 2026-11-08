// SPDX-License-Identifier: Apache-2.0

package fakerunner

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

// Runner answers expected commands with canned output.
type Runner struct {
	t       testing.TB
	mutex   sync.Mutex
	answers map[string]answer
	calls   []string
}

type answer struct {
	output []byte
	err    error
}

// New returns a Runner that fails t on any command it wasn't told about.
func New(t testing.TB) *Runner {
	return &Runner{t: t, answers: map[string]answer{}}
}

// Expect makes the command (program and arguments, joined by spaces)
// return output and err.
func (r *Runner) Expect(commandLine string, output string, err error) *Runner {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.answers[commandLine] = answer{output: []byte(output), err: err}
	return r
}

// ExpectFile makes the command return the contents of a golden file.
func (r *Runner) ExpectFile(commandLine, path string) *Runner {
	r.t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		r.t.Fatal(err)
	}
	return r.Expect(commandLine, string(data), nil)
}

// Run implements command.Runner.
func (r *Runner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	commandLine := strings.Join(append([]string{name}, args...), " ")
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.calls = append(r.calls, commandLine)
	found, ok := r.answers[commandLine]
	if !ok {
		r.t.Errorf("unexpected command: %s", commandLine)
		return nil, fmt.Errorf("unexpected command: %s", commandLine)
	}
	return found.output, found.err
}

// Calls returns every command run so far, in order.
func (r *Runner) Calls() []string {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return append([]string(nil), r.calls...)
}

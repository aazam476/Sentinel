// SPDX-License-Identifier: Apache-2.0

package scripts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
)

// Errors from Runner. They are never reassigned.
var (
	// ErrLanguage: the language isn't one the agent runs.
	ErrLanguage = errors.New("unknown script language; use bash, sh, powershell or python")
	// ErrNoInterpreter: the language's interpreter isn't installed.
	ErrNoInterpreter = errors.New("the script's interpreter isn't installed on this machine")
)

// maxTimeout bounds a script's time limit; the default is 10 minutes (§9.8).
const (
	defaultTimeout = 10 * time.Minute
	maxTimeout     = 24 * time.Hour
)

// interpreter is how a language is run: a program and the arguments that
// come before the script's file, and the file's extension.
type interpreter struct {
	program   string
	arguments []string
	extension string
}

// interpreters lists, per operating system, how each language runs.
func interpreters(goos string) map[string][]interpreter {
	powershell := []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File"}
	if goos == "windows" {
		return map[string][]interpreter{
			protocol.LanguagePowerShell: {{"pwsh", powershell, ".ps1"}, {"powershell", powershell, ".ps1"}},
			protocol.LanguagePython:     {{"python", nil, ".py"}, {"py", []string{"-3"}, ".py"}},
			protocol.LanguageBash:       {{"bash", nil, ".sh"}},
		}
	}
	return map[string][]interpreter{
		protocol.LanguageBash:       {{"bash", nil, ".sh"}},
		protocol.LanguageSh:         {{"sh", nil, ".sh"}},
		protocol.LanguagePython:     {{"python3", nil, ".py"}, {"python", nil, ".py"}},
		protocol.LanguagePowerShell: {{"pwsh", powershell[:2], ".ps1"}},
	}
}

// Runner runs scripts on this machine.
type Runner struct {
	goos     string
	lookPath func(string) (string, error)
}

// New returns a Runner for an operating system (runtime.GOOS).
func New(goos string) *Runner {
	return &Runner{goos: goos, lookPath: exec.LookPath}
}

// Run runs a script, writing its output to out as it comes, and returns how
// it ended. It returns an error only when the script couldn't start.
func (r *Runner) Run(ctx context.Context, request protocol.ScriptRunPayload, out io.Writer) (protocol.ScriptResult, error) {
	found, program, err := r.find(request.Language)
	if err != nil {
		return protocol.ScriptResult{}, err
	}
	folder, err := os.MkdirTemp("", "sentinel-script-")
	if err != nil {
		return protocol.ScriptResult{}, fmt.Errorf("make a folder for the script: %w", err)
	}
	defer func() { _ = os.RemoveAll(folder) }() // the script's own temporary folder
	file := filepath.Join(folder, "script"+found.extension)
	if err := os.WriteFile(file, []byte(request.Body), 0o600); err != nil {
		return protocol.ScriptResult{}, fmt.Errorf("write the script: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, timeoutOf(request.TimeoutSeconds))
	defer cancel()
	command := exec.CommandContext(ctx, program, append(found.arguments, file)...) //nolint:gosec // G204: a fixed interpreter runs the script; no shell builds the command
	command.Dir = folder
	command.Stdout, command.Stderr = out, out
	command.WaitDelay = 5 * time.Second
	ownGroup(command)
	err = command.Run()
	return result(ctx, command, err), nil
}

func timeoutOf(seconds int) time.Duration {
	if seconds <= 0 {
		return defaultTimeout
	}
	return min(time.Duration(seconds)*time.Second, maxTimeout)
}

// find picks the first of a language's interpreters that is installed.
func (r *Runner) find(language string) (interpreter, string, error) {
	choices, known := interpreters(r.goos)[language]
	if !known {
		return interpreter{}, "", ErrLanguage
	}
	for _, choice := range choices {
		if path, err := r.lookPath(choice.program); err == nil {
			return choice, path, nil
		}
	}
	return interpreter{}, "", fmt.Errorf("%s: %w", language, ErrNoInterpreter)
}

// result describes how the command ended.
func result(ctx context.Context, command *exec.Cmd, err error) protocol.ScriptResult {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return protocol.ScriptResult{ExitCode: -1, TimedOut: true, Error: "the script ran past its time limit and was stopped"}
	}
	if ctx.Err() != nil {
		return protocol.ScriptResult{ExitCode: -1, Error: "the script was stopped"}
	}
	var exitErr *exec.ExitError
	if err == nil || errors.As(err, &exitErr) {
		return protocol.ScriptResult{ExitCode: command.ProcessState.ExitCode()}
	}
	return protocol.ScriptResult{ExitCode: -1, Error: err.Error()}
}

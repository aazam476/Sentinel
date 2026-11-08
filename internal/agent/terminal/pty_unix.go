// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package terminal

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/creack/pty"
)

// NewStarter returns the Starter for this machine: the login shell of the
// account the agent runs as (root), in a pseudo-terminal.
func NewStarter() Starter {
	return func(cols, rows uint16) (Shell, error) {
		return startPTY(loginShell("/etc/passwd", os.Getuid(), os.Getenv("SHELL")), cols, rows)
	}
}

// startPTY runs shell as a login shell in a new pseudo-terminal.
func startPTY(shell string, cols, rows uint16) (Shell, error) {
	// The session ends the shell through Close, not a context.
	command := exec.CommandContext(context.Background(), shell, "-l") //nolint:gosec // G204: the shell comes from the system's account database
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}
	command.Dir = home
	command.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", shell, err)
	}
	return &ptyShell{terminal: terminal, command: command}, nil
}

// ptyShell is a shell in a pseudo-terminal.
type ptyShell struct {
	terminal *os.File
	command  *exec.Cmd
}

func (s *ptyShell) Read(buffer []byte) (int, error) {
	n, err := s.terminal.Read(buffer)
	// Linux reports the end of a pseudo-terminal whose shell exited as an
	// I/O error rather than EOF; both mean the same here.
	if err != nil && !errors.Is(err, io.EOF) {
		err = io.EOF
	}
	return n, err
}

func (s *ptyShell) Write(data []byte) (int, error) {
	return s.terminal.Write(data)
}

func (s *ptyShell) Resize(cols, rows uint16) error {
	return pty.Setsize(s.terminal, &pty.Winsize{Cols: cols, Rows: rows})
}

// Close ends the shell: closing the terminal hangs it up, as closing a
// terminal window does, and the shell is reaped so it doesn't linger.
func (s *ptyShell) Close() error {
	err := s.terminal.Close()
	if s.command.Process != nil {
		_ = s.command.Process.Kill() // already gone after the hang-up, usually
		_ = s.command.Wait()         // reaps it; its exit status doesn't matter here
	}
	return err
}

// loginShell finds the login shell of the account with uid in the passwd
// file, then falls back to $SHELL, then to /bin/sh.
func loginShell(passwdPath string, uid int, shellVariable string) string {
	if shell := shellFromPasswd(passwdPath, uid); shell != "" {
		return shell
	}
	if filepath.IsAbs(shellVariable) {
		return shellVariable
	}
	return "/bin/sh"
}

// shellFromPasswd reads the shell field (the seventh) of uid's line.
func shellFromPasswd(path string, uid int) string {
	file, err := os.Open(path) //nolint:gosec // G304: the system account database
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }() // read-only; nothing to report
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		fields := strings.Split(lines.Text(), ":")
		if len(fields) == 7 && fields[2] == strconv.Itoa(uid) && filepath.IsAbs(fields[6]) {
			return fields[6]
		}
	}
	return ""
}

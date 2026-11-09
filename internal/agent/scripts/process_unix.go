// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package scripts

import (
	"os/exec"
	"syscall"
)

// ownGroup runs the script in a process group of its own and, when it must
// stop, ends the whole group: a script's child processes (a sleep, a
// background job) would otherwise outlive it.
func ownGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
}

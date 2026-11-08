// SPDX-License-Identifier: Apache-2.0

package terminal

import "io"

// Shell is a running shell attached to a terminal.
type Shell interface {
	// Read returns the shell's output; it fails once the shell has exited.
	io.Reader
	// Write sends keystrokes to the shell.
	io.Writer
	// Resize changes the terminal's size.
	Resize(cols, rows uint16) error
	// Close ends the shell and releases the terminal.
	Close() error
}

// Starter starts a new shell in a terminal of the given size. Each
// operating system has its own (see pty_unix.go and conpty_windows.go).
type Starter func(cols, rows uint16) (Shell, error)

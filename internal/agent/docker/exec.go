// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"context"
	"fmt"

	"github.com/moby/moby/client"

	"github.com/aazam476/sentinel/internal/agent/terminal"
)

// defaultShell runs bash where the image has it, and sh otherwise.
func defaultShell() []string {
	return []string{"/bin/sh", "-c", "if command -v bash >/dev/null 2>&1; then exec bash -l; else exec sh -l; fi"}
}

// ExecStarter returns a terminal.Starter that runs command (or a shell) in
// a container with a terminal, for the terminal manager to run like the
// machine's own shells.
func (e *Engine) ExecStarter(id string, command []string) (terminal.Starter, error) {
	if err := checkContainer(id); err != nil {
		return nil, err
	}
	if len(command) == 0 {
		command = defaultShell()
	}
	return func(cols, rows uint16) (terminal.Shell, error) {
		// The session outlives the request that opened it.
		ctx := context.Background()
		var shell *execShell
		err := e.call(ctx, func(c *client.Client) error {
			size := client.ConsoleSize{Height: uint(rows), Width: uint(cols)}
			created, err := c.ExecCreate(ctx, id, client.ExecCreateOptions{TTY: true, ConsoleSize: size,
				AttachStdin: true, AttachStdout: true, AttachStderr: true, Cmd: command,
				Env: []string{"TERM=xterm-256color"}})
			if err != nil {
				return err
			}
			attached, err := c.ExecAttach(ctx, created.ID, client.ExecAttachOptions{TTY: true, ConsoleSize: size})
			if err != nil {
				return err
			}
			shell = &execShell{client: c, id: created.ID, attached: attached.HijackedResponse}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("start a shell in container %s: %w", id, err)
		}
		return shell, nil
	}, nil
}

// execShell is a shell in a container, attached through Docker's hijacked
// connection: with a terminal, input and output are one raw stream.
type execShell struct {
	client   *client.Client
	id       string
	attached client.HijackedResponse
}

func (s *execShell) Read(buffer []byte) (int, error) { return s.attached.Reader.Read(buffer) }

func (s *execShell) Write(data []byte) (int, error) { return s.attached.Conn.Write(data) }

func (s *execShell) Resize(cols, rows uint16) error {
	_, err := s.client.ExecResize(context.Background(), s.id, client.ExecResizeOptions{Height: uint(rows), Width: uint(cols)})
	if err != nil {
		return fmt.Errorf("resize: %w", err)
	}
	return nil
}

// Close ends the shell: with its input closed, it exits.
func (s *execShell) Close() error {
	s.attached.Close()
	return nil
}

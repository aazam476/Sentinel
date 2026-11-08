// SPDX-License-Identifier: Apache-2.0

package stacks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aazam476/sentinel/internal/protocol"
)

// stagedFiles are a save's new files, written next to the old ones under
// temporary names until they have been checked.
type stagedFiles struct {
	compose string
	env     string
}

// stage writes the request's files under fixed temporary names in folder
// (saves are one at a time; see Manager.saving). The .env is written only
// if it has content.
func (m *Manager) stage(folder string, request protocol.StackSavePayload) (stagedFiles, error) {
	var staged stagedFiles
	var err error
	if staged.compose, err = writeStaged(filepath.Join(folder, composeFile+".new"), request.Compose); err != nil {
		return staged, err
	}
	if request.Env != "" {
		if staged.env, err = writeStaged(filepath.Join(folder, envFile+".new"), request.Env); err != nil {
			return staged, err
		}
	}
	return staged, nil
}

// writeStaged writes text to a new file only root can read: a .env holds
// secrets, and a compose file often does too. A file left by a save that
// crashed is replaced.
func writeStaged(path, text string) (string, error) {
	_ = os.Remove(path)                                                      // usually there is none
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: inside the stack's folder
	if err != nil {
		return "", fmt.Errorf("write the stack's files: %w", err)
	}
	_, writeErr := file.WriteString(text)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return path, fmt.Errorf("write the stack's files: %w", err)
	}
	return path, nil
}

// commit moves the staged files into place. Each rename replaces the old
// file in one step, so compose never sees half a file.
func (s stagedFiles) commit(folder string, stack protocol.Stack) error {
	target := filepath.Join(folder, composeFile)
	if !stack.Managed {
		target = stack.ConfigFiles[0]
	}
	if err := os.Rename(s.compose, target); err != nil {
		return fmt.Errorf("save the compose file: %w", err)
	}
	if !stack.Managed {
		return nil
	}
	envPath := filepath.Join(folder, envFile)
	if s.env == "" {
		if err := os.Remove(envPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove the old .env: %w", err)
		}
		return nil
	}
	if err := os.Rename(s.env, envPath); err != nil {
		return fmt.Errorf("save .env: %w", err)
	}
	return nil
}

// remove deletes whatever staged files are left (all of them, unless they
// were committed).
func (s stagedFiles) remove() {
	for _, path := range []string{s.compose, s.env} {
		if path != "" {
			_ = os.Remove(path) // already moved into place, or never written
		}
	}
}

// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package files

import (
	"errors"
	"syscall"
)

func isNotEmpty(err error) bool  { return errors.Is(err, syscall.ENOTEMPTY) }
func isNotFolder(err error) bool { return errors.Is(err, syscall.ENOTDIR) }

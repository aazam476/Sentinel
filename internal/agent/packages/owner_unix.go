// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package packages

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// owner returns the name of the user who owns a file.
func owner(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("check who owns %s: %w", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("this system doesn't say who owns a file")
	}
	found, err := user.LookupId(strconv.FormatUint(uint64(stat.Uid), 10))
	if err != nil {
		return "", fmt.Errorf("look up who owns %s: %w", path, err)
	}
	return found.Username, nil
}

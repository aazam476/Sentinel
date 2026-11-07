// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package localaccess

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSEC08_OnlyRootOrTheDataOwnerMayResetTheAdmin(t *testing.T) {
	dataDir := t.TempDir()
	owner := os.Geteuid()

	tests := []struct {
		name    string
		userID  int
		wantErr error
	}{
		{name: "allows root", userID: 0},
		{name: "allows the data directory's owner", userID: owner},
		{name: "refuses anyone else", userID: owner + 4242, wantErr: ErrNotAllowed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := check(dataDir, test.userID); !errors.Is(err, test.wantErr) {
				t.Errorf("check = %v, want %v", err, test.wantErr)
			}
		})
	}

	t.Run("reports a missing data directory", func(t *testing.T) {
		if err := check(filepath.Join(dataDir, "missing"), 0); err == nil || errors.Is(err, ErrNotAllowed) {
			t.Errorf("check = %v, want a missing-directory error", err)
		}
	})

	t.Run("Check uses the current user", func(t *testing.T) {
		if err := Check(dataDir); err != nil {
			t.Errorf("Check on a directory this test owns = %v", err)
		}
	})
}

// SPDX-License-Identifier: Apache-2.0

package buildinfo_test

import (
	"testing"

	"github.com/aazam476/sentinel/internal/shared/buildinfo"
)

func TestVersion(t *testing.T) {
	t.Run("returns a non-empty version for the running binary", func(t *testing.T) {
		if got := buildinfo.Version(); got == "" {
			t.Fatal("Version() returned an empty string")
		}
	})
}

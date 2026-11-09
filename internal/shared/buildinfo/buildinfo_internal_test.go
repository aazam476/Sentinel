// SPDX-License-Identifier: Apache-2.0

package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestDescribe(t *testing.T) {
	tests := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{
			name: "returns dev when there is no build information",
			info: nil,
			want: "dev",
		},
		{
			name: "returns the release tag for a tagged build",
			info: &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0-beta.1"}},
			want: "v0.1.0-beta.1",
		},
		{
			name: "returns dev for a devel build without version control information",
			info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			want: "dev",
		},
		{
			name: "returns dev for an empty module version without version control information",
			info: &debug.BuildInfo{},
			want: "dev",
		},
		{
			name: "returns the short commit hash for a devel build",
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
					{Key: "vcs.modified", Value: "false"},
				},
			},
			want: "dev-0123456789ab",
		},
		{
			name: "marks a devel build from a modified working tree as dirty",
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
					{Key: "vcs.modified", Value: "true"},
				},
			},
			want: "dev-0123456789ab-dirty",
		},
		{
			name: "keeps a commit hash that is already short",
			info: &debug.BuildInfo{
				Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc123"}},
			},
			want: "dev-abc123",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := describe(test.info); got != test.want {
				t.Errorf("describe() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestStampedVersion(t *testing.T) {
	stamped = "v9.9.9"
	t.Cleanup(func() { stamped = "" })
	if got := Version(); got != "v9.9.9" {
		t.Errorf("Version() = %q, want the stamped version", got)
	}
}

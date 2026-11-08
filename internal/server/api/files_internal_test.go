// SPDX-License-Identifier: Apache-2.0

package api

import "testing"

func TestByteRange(t *testing.T) {
	cases := []struct {
		name, header string
		start, end   uint64
		partial      bool
	}{
		{"sends everything without a range", "", 0, 100, false},
		{"resumes from an offset", "bytes=40-", 40, 100, true},
		{"sends a closed range, end included", "bytes=10-19", 10, 20, true},
		{"caps a range past the end", "bytes=10-500", 10, 100, true},
		{"ignores a range starting past the end", "bytes=100-", 0, 100, false},
		{"ignores several ranges", "bytes=0-1,5-6", 0, 100, false},
		{"ignores a backwards range's end", "bytes=50-10", 50, 100, true},
		{"ignores a suffix range", "bytes=-10", 0, 100, false},
		{"ignores another unit", "items=1-2", 0, 100, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end, partial := byteRange(tc.header, 100)
			if start != tc.start || end != tc.end || partial != tc.partial {
				t.Errorf("got %d-%d %v, want %d-%d %v", start, end, partial, tc.start, tc.end, tc.partial)
			}
		})
	}
}

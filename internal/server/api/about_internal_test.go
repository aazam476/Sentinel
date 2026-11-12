// SPDX-License-Identifier: Apache-2.0

package api

import (
	"reflect"
	"testing"
)

func TestCountVersionsPutsTheMostCommonFirst(t *testing.T) {
	got := countVersions([]string{"v1.2.0", "v1.1.0", "v1.2.0", "", "v1.0.0", "v1.1.0", "v1.2.0"})
	want := []AgentVersionCount{
		{Version: "v1.2.0", Machines: 3}, {Version: "v1.1.0", Machines: 2},
		{Version: "unknown", Machines: 1}, {Version: "v1.0.0", Machines: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("countVersions = %+v\nwant %+v", got, want)
	}
	if got := countVersions(nil); got == nil || len(got) != 0 {
		t.Errorf("countVersions(nil) = %#v, want an empty list", got)
	}
}

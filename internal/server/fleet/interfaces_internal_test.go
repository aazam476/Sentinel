// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/protocol"
)

func TestCleanInterfacesNormalisesAndRefuses(t *testing.T) {
	cleaned, err := cleanInterfaces([]protocol.NetworkInterface{
		{Name: " eth0 ", MAC: "AA-BB-CC-DD-EE-01", Addresses: []string{"192.168.1.20/24", "fe80::1/64"}},
	})
	if err != nil || cleaned[0].Name != "eth0" || cleaned[0].MAC != "aa:bb:cc:dd:ee:01" {
		t.Errorf("cleaned = %+v, %v", cleaned, err)
	}
	refused := []struct {
		name       string
		interfaces []protocol.NetworkInterface
	}{
		{name: "too many interfaces", interfaces: make([]protocol.NetworkInterface, 33)},
		{name: "no name", interfaces: []protocol.NetworkInterface{{Name: ""}}},
		{name: "a bad MAC", interfaces: []protocol.NetworkInterface{{Name: "eth0", MAC: "zz"}}},
		{name: "a bad network", interfaces: []protocol.NetworkInterface{{Name: "eth0", Addresses: []string{"192.168.1.20"}}}},
		{name: "too many addresses", interfaces: []protocol.NetworkInterface{{Name: "eth0", Addresses: strings.Split(strings.Repeat("10.0.0.1/8,", 17), ",")[:17]}}},
	}
	for _, test := range refused {
		if _, err := cleanInterfaces(test.interfaces); err == nil {
			t.Errorf("accepted %s", test.name)
		}
	}
}

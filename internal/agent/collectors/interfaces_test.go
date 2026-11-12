// SPDX-License-Identifier: Apache-2.0

package collectors_test

import (
	"errors"
	"net"
	"reflect"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/collectors"
	"github.com/aazam476/sentinel/internal/protocol"
)

func TestInterfacesListTheUpOnesWithTheirNetworks(t *testing.T) {
	mac, _ := net.ParseMAC("aa:bb:cc:dd:ee:01")
	interfaces := []net.Interface{
		{Name: "lo", Flags: net.FlagUp | net.FlagLoopback},
		{Name: "eth0", HardwareAddr: mac, Flags: net.FlagUp | net.FlagBroadcast},
		{Name: "eth1", Flags: 0}, // down
		{Name: "wg0", Flags: net.FlagUp},
	}
	addresses := func(iface net.Interface) ([]net.Addr, error) {
		switch iface.Name {
		case "eth0":
			_, v4, _ := net.ParseCIDR("192.168.1.0/24")
			v4.IP = net.ParseIP("192.168.1.20")
			return []net.Addr{v4, &net.IPAddr{IP: net.ParseIP("fe80::1")}}, nil
		default:
			return nil, errors.New("no addresses")
		}
	}
	want := []protocol.NetworkInterface{
		{Name: "eth0", MAC: "aa:bb:cc:dd:ee:01", Addresses: []string{"192.168.1.20/24"}},
		{Name: "wg0", Addresses: []string{}},
	}
	if got := collectors.Interfaces(interfaces, addresses); !reflect.DeepEqual(got, want) {
		t.Errorf("Interfaces = %+v\nwant %+v", got, want)
	}
}

func TestInterfacesStopAtAReasonableNumber(t *testing.T) {
	many := make([]net.Interface, 100)
	for i := range many {
		many[i] = net.Interface{Name: "veth", Flags: net.FlagUp}
	}
	none := func(net.Interface) ([]net.Addr, error) { return nil, nil }
	if got := collectors.Interfaces(many, none); len(got) != 32 {
		t.Errorf("got %d interfaces, want 32", len(got))
	}
}

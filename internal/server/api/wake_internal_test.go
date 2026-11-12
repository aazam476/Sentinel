// SPDX-License-Identifier: Apache-2.0

package api

import (
	"testing"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/fleet"
)

func machineOn(id int64, name string, interfaces ...protocol.NetworkInterface) fleet.Agent {
	return fleet.Agent{ID: id, PublicID: name, Hostname: name, SystemInfo: protocol.SystemInfo{Interfaces: interfaces}}
}

func TestPlanWakeUsesAnOnlineNeighbourOnTheSameNetwork(t *testing.T) {
	target := machineOn(1, "nas",
		protocol.NetworkInterface{Name: "wg0", Addresses: []string{"10.8.0.2/24"}}, // no MAC: can't be woken through it
		protocol.NetworkInterface{Name: "eth0", MAC: "aa:bb:cc:dd:ee:01", Addresses: []string{"fe80::1/64", "192.168.1.20/24"}},
	)
	offline := machineOn(2, "laptop", protocol.NetworkInterface{Name: "en0", Addresses: []string{"192.168.1.30/24"}})
	elsewhere := machineOn(3, "vps", protocol.NetworkInterface{Name: "eth0", Addresses: []string{"203.0.113.5/24", "10.8.0.1/24"}})
	noPower := machineOn(4, "kiosk", protocol.NetworkInterface{Name: "eth0", Addresses: []string{"192.168.1.40/24"}})
	noPower.SystemInfo.Capabilities = []protocol.Capability{{Name: "power", Enabled: false}}
	helper := machineOn(5, "pi", protocol.NetworkInterface{Name: "eth0", Addresses: []string{"192.168.1.50/24"}})
	online := func(id int64) bool { return id != 2 }

	plan, ok := planWake(target, []fleet.Agent{target, offline, elsewhere, noPower, helper}, online)
	if !ok || plan.helper.ID != 5 || plan.payload != (protocol.WakePayload{MAC: "aa:bb:cc:dd:ee:01", Network: "192.168.1.20/24"}) {
		t.Errorf("plan = %+v, %v; want the pi to wake eth0", plan, ok)
	}
	if _, ok := planWake(target, []fleet.Agent{target, offline, elsewhere, noPower}, online); ok {
		t.Error("planned a wake with no allowed, online machine on the network")
	}
	if _, ok := planWake(machineOn(9, "unknown"), []fleet.Agent{helper}, online); ok {
		t.Error("planned a wake for a machine with no known interfaces")
	}
}

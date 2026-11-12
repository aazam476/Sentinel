// SPDX-License-Identifier: Apache-2.0

package wol_test

import (
	"bytes"
	"errors"
	"net/netip"
	"runtime"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/wol"
)

func TestMagicPacketRepeatsTheMACSixteenTimes(t *testing.T) {
	packet, err := wol.MagicPacket("AA:bb:cc:dd:ee:01")
	if err != nil {
		t.Fatal(err)
	}
	if len(packet) != 102 || !bytes.Equal(packet[:6], bytes.Repeat([]byte{0xff}, 6)) {
		t.Fatalf("packet = %x", packet)
	}
	mac := []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x01}
	for i := range 16 {
		if !bytes.Equal(packet[6+6*i:12+6*i], mac) {
			t.Errorf("copy %d = %x", i, packet[6+6*i:12+6*i])
		}
	}
	for _, bad := range []string{"", "aa:bb:cc", "not a mac", "00:00:00:00:fe:80:00:00:00:00:00:00:02:00:5e:10:00:00:00:01"} {
		if _, err := wol.MagicPacket(bad); !errors.Is(err, wol.ErrBadMAC) {
			t.Errorf("MagicPacket(%q) = %v, want ErrBadMAC", bad, err)
		}
	}
}

func TestBroadcastOnlyOnThisMachinesNetworks(t *testing.T) {
	own := []netip.Prefix{netip.MustParsePrefix("192.168.1.20/24"), netip.MustParsePrefix("10.0.0.7/8")}
	tests := []struct {
		network string
		want    string
	}{
		{network: "192.168.1.55/24", want: "192.168.1.255"},
		{network: "10.20.30.40/8", want: "10.255.255.255"},
	}
	for _, test := range tests {
		got, err := wol.Broadcast(test.network, own)
		if err != nil || got.String() != test.want {
			t.Errorf("Broadcast(%s) = %v, %v; want %s", test.network, got, err, test.want)
		}
	}
	for _, refused := range []string{"192.168.2.1/24", "192.168.1.0/16", "8.8.8.8/32", "fe80::1/64", "nonsense"} {
		if _, err := wol.Broadcast(refused, own); !errors.Is(err, wol.ErrNotMyNetwork) {
			t.Errorf("Broadcast(%s) = %v, want ErrNotMyNetwork", refused, err)
		}
	}
}

func TestWakeRefusesBeforeSendingAnything(t *testing.T) {
	if err := wol.Wake(t.Context(), "nope", "192.168.1.0/24"); !errors.Is(err, wol.ErrBadMAC) {
		t.Errorf("bad MAC: %v", err)
	}
	if err := wol.Wake(t.Context(), "aa:bb:cc:dd:ee:01", "203.0.113.0/24"); !errors.Is(err, wol.ErrNotMyNetwork) {
		t.Errorf("foreign network: %v", err)
	}
	if _, err := wol.Networks(); err != nil {
		t.Errorf("Networks: %v", err)
	}
}

func TestWakeSendsOnTheLoopbackNetwork(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux takes a datagram to 127.255.255.255; macOS and Windows may refuse it")
	}
	// 127.0.0.0/8 is a network every machine is on; its broadcast address
	// takes a datagram without anything listening.
	if err := wol.Wake(t.Context(), "aa:bb:cc:dd:ee:01", "127.0.0.1/8"); err != nil {
		t.Errorf("Wake on loopback = %v", err)
	}
}

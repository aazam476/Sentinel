// SPDX-License-Identifier: Apache-2.0

package wol

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
)

// port is the discard port Wake-on-LAN packets conventionally go to.
const port = 9

// Errors from Wake. They are never reassigned.
var (
	// ErrBadMAC: the address isn't a 6-byte Ethernet MAC address.
	ErrBadMAC = errors.New("that isn't an Ethernet MAC address")
	// ErrNotMyNetwork: the network isn't one this machine is on.
	ErrNotMyNetwork = errors.New("this machine isn't on that network")
)

// MagicPacket is the packet that wakes the machine with this MAC address.
func MagicPacket(mac string) ([]byte, error) {
	hardware, err := net.ParseMAC(mac)
	if err != nil || len(hardware) != 6 {
		return nil, fmt.Errorf("%w: %q", ErrBadMAC, mac)
	}
	packet := bytes.Repeat([]byte{0xff}, 6)
	for range 16 {
		packet = append(packet, hardware...)
	}
	return packet, nil
}

// Broadcast is the broadcast address of network, if it is an IPv4
// network this machine is on (one of own, as from Networks).
func Broadcast(network string, own []netip.Prefix) (netip.Addr, error) {
	prefix, err := netip.ParsePrefix(network)
	if err != nil || !prefix.Addr().Is4() {
		return netip.Addr{}, fmt.Errorf("%w: %q", ErrNotMyNetwork, network)
	}
	prefix = prefix.Masked()
	for _, mine := range own {
		if mine.Masked() == prefix {
			return lastAddress(prefix), nil
		}
	}
	return netip.Addr{}, fmt.Errorf("%w: %s", ErrNotMyNetwork, prefix)
}

// lastAddress is a network's broadcast address: every host bit set.
func lastAddress(prefix netip.Prefix) netip.Addr {
	base := prefix.Addr().As4()
	value := binary.BigEndian.Uint32(base[:]) | (1<<(32-prefix.Bits()) - 1)
	var broadcast [4]byte
	binary.BigEndian.PutUint32(broadcast[:], value)
	return netip.AddrFrom4(broadcast)
}

// Networks are this machine's IPv4 networks.
func Networks() ([]netip.Prefix, error) {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("read this machine's networks: %w", err)
	}
	var networks []netip.Prefix
	for _, address := range addresses {
		if ipNet, ok := address.(*net.IPNet); ok {
			if prefix, err := netip.ParsePrefix(ipNet.String()); err == nil && prefix.Addr().Is4() {
				networks = append(networks, prefix)
			}
		}
	}
	return networks, nil
}

// Wake sends the magic packet for mac to the broadcast address of
// network, which must be one of this machine's.
func Wake(ctx context.Context, mac, network string) error {
	packet, err := MagicPacket(mac)
	if err != nil {
		return err
	}
	own, err := Networks()
	if err != nil {
		return err
	}
	broadcast, err := Broadcast(network, own)
	if err != nil {
		return err
	}
	return send(ctx, net.JoinHostPort(broadcast.String(), strconv.Itoa(port)), packet)
}

// send writes one UDP datagram. Go sets SO_BROADCAST on UDP sockets, so a
// broadcast address works.
func send(ctx context.Context, address string, packet []byte) error {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "udp4", address)
	if err != nil {
		return fmt.Errorf("send the wake-up packet: %w", err)
	}
	defer func() { _ = conn.Close() }() // one datagram, already sent or failed
	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("send the wake-up packet: %w", err)
	}
	return nil
}

// SPDX-License-Identifier: Apache-2.0

package collectors

import (
	"net"

	"github.com/aazam476/sentinel/internal/protocol"
)

// systemInterfaces describes this machine's network interfaces, or none if
// they can't be read.
func systemInterfaces() []protocol.NetworkInterface {
	interfaces, err := net.Interfaces()
	if err != nil {
		return []protocol.NetworkInterface{}
	}
	return Interfaces(interfaces, interfaceAddresses)
}

// interfaceAddresses reads an interface's IP networks.
func interfaceAddresses(iface net.Interface) ([]net.Addr, error) {
	return iface.Addrs() //nolint:wrapcheck // the caller only skips the interface
}

// maxInterfaces keeps a machine with thousands of virtual interfaces (a
// busy container host) from flooding its system info.
const maxInterfaces = 32

// Interfaces describes the network interfaces that are up and aren't
// loopback, with their MAC addresses and IP networks, for the inventory
// and Wake-on-LAN (§9.6). addresses reads each interface's networks.
func Interfaces(interfaces []net.Interface, addresses func(net.Interface) ([]net.Addr, error)) []protocol.NetworkInterface {
	described := []protocol.NetworkInterface{}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		entry := protocol.NetworkInterface{Name: iface.Name, MAC: iface.HardwareAddr.String(), Addresses: []string{}}
		if found, err := addresses(iface); err == nil {
			for _, address := range found {
				if network, ok := address.(*net.IPNet); ok {
					entry.Addresses = append(entry.Addresses, network.String())
				}
			}
		}
		described = append(described, entry)
		if len(described) == maxInterfaces {
			break
		}
	}
	return described
}

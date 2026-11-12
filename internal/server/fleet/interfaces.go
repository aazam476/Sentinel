// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"fmt"
	"net"
	"net/netip"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/auth"
)

// Bounds on the network interfaces an agent reports.
const (
	maxInterfaces         = 32
	maxInterfaceAddresses = 16
)

// cleanInterfaces checks the network interfaces an agent reports: a name,
// a MAC address that parses (or none) and IP networks in CIDR form.
func cleanInterfaces(interfaces []protocol.NetworkInterface) ([]protocol.NetworkInterface, error) {
	if len(interfaces) > maxInterfaces {
		return nil, fmt.Errorf("agent reported %d network interfaces, more than %d", len(interfaces), maxInterfaces)
	}
	for i := range interfaces {
		iface := &interfaces[i]
		var err error
		if iface.Name, err = requiredText("interface", iface.Name, maxReportedLength); err != nil {
			return nil, err
		}
		if iface.MAC != "" {
			hardware, err := net.ParseMAC(iface.MAC)
			if err != nil {
				return nil, &auth.ValidationError{Field: "mac", Message: "This isn't a MAC address."}
			}
			iface.MAC = hardware.String()
		}
		if len(iface.Addresses) > maxInterfaceAddresses {
			return nil, &auth.ValidationError{Field: "addresses", Message: "There are too many addresses."}
		}
		for j, address := range iface.Addresses {
			prefix, err := netip.ParsePrefix(address)
			if err != nil {
				return nil, &auth.ValidationError{Field: "addresses", Message: "This isn't an IP network."}
			}
			iface.Addresses[j] = prefix.String()
		}
	}
	return interfaces, nil
}

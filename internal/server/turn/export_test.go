// SPDX-License-Identifier: Apache-2.0

package turn

import "net"

// Permitted exposes the peer filter to tests.
func (r *Relay) Permitted(peer net.IP) bool { return r.permitted(nil, peer) }

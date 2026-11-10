// SPDX-License-Identifier: Apache-2.0

package x11

import (
	"errors"
	"fmt"

	"github.com/jezek/xgb/randr"
	"github.com/jezek/xgb/xproto"

	"github.com/aazam476/sentinel/internal/protocol"
)

// Monitors lists the display's monitors as RandR (1.5) knows them; a
// server without RandR has one monitor, the whole screen.
func (s *Source) Monitors() ([]protocol.MonitorInfo, error) {
	if !hasExtension(s.conn, extensionRANDR) {
		return nil, errors.New("list monitors: the display has no RandR extension")
	}
	reply, err := randr.GetMonitors(s.conn, s.root, true).Reply()
	if err != nil {
		return nil, fmt.Errorf("list monitors: %w", err)
	}
	monitors := make([]protocol.MonitorInfo, 0, len(reply.Monitors))
	for i, monitor := range reply.Monitors {
		name := fmt.Sprintf("Monitor %d", i+1)
		if atom, err := xproto.GetAtomName(s.conn, monitor.Name).Reply(); err == nil && atom.Name != "" {
			name = atom.Name
		}
		monitors = append(monitors, protocol.MonitorInfo{
			Name: name, X: int(monitor.X), Y: int(monitor.Y), Width: int(monitor.Width), Height: int(monitor.Height),
			Primary: monitor.Primary,
		})
	}
	return monitors, nil
}

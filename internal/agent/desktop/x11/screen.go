// SPDX-License-Identifier: Apache-2.0

package x11

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/aazam476/sentinel/internal/agent/desktop"
	"github.com/aazam476/sentinel/internal/protocol"
)

// Screen is the machine's X11 desktop, found anew for each session: the
// person logged in can change.
type Screen struct {
	finder Finder
	// display and authority are the local policy's fixed display and
	// Xauthority file; with no display, the active session is found.
	display, authority string
}

// NewScreen returns the X11 desktop. display and authority come from the
// local policy and may be empty.
func NewScreen(runner Runner, display, authority string) *Screen {
	return &Screen{
		finder: Finder{
			Runner: runner, ReadFile: os.ReadFile, Glob: filepath.Glob,
			HomeOf: func(name string) (string, error) {
				account, err := user.Lookup(name)
				if err != nil {
					return "", fmt.Errorf("look up user %s: %w", name, err)
				}
				return account.HomeDir, nil
			},
		},
		display: display, authority: authority,
	}
}

// Availability says whether there's an X11 desktop to share now.
func (s *Screen) Availability(ctx context.Context) protocol.DesktopAvailability {
	if _, err := s.find(ctx); err != nil {
		return protocol.DesktopAvailability{Reason: reason(err)}
	}
	return protocol.DesktopAvailability{Available: true}
}

// Open connects to the desktop.
func (s *Screen) Open(ctx context.Context) (desktop.Source, error) {
	display, err := s.find(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", desktop.ErrUnavailable, reason(err))
	}
	return Open(display)
}

func (s *Screen) find(ctx context.Context) (Display, error) {
	if s.display == "" {
		return s.finder.Find(ctx)
	}
	number, err := ParseDisplay(s.display)
	if err != nil {
		return Display{}, err
	}
	display := Display{Number: number}
	if s.authority != "" {
		file, err := s.finder.ReadFile(s.authority)
		if err != nil {
			return Display{}, fmt.Errorf("read the policy's Xauthority file: %w", err)
		}
		if display.Cookie, err = CookieFor(file, number); err != nil {
			return Display{}, err
		}
	}
	return display, nil
}

// reason is what the machine page says when there's no desktop. The
// server keeps at most 128 bytes of it.
func reason(err error) string {
	switch {
	case errors.Is(err, ErrNoSession):
		return "No one is logged in to a graphical session."
	case errors.Is(err, ErrWayland):
		return "Wayland sessions aren’t supported yet; an X11 session (such as “Ubuntu on Xorg”) works."
	case errors.Is(err, ErrNoLoginctl):
		return "No graphical session was found: loginctl couldn’t list the sessions on this machine."
	default:
		// Never the error itself: it may hold a command's output, and the
		// page shows a short sentence.
		return "No graphical session was found that remote desktop can share."
	}
}

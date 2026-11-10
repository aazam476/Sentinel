// SPDX-License-Identifier: Apache-2.0

package x11

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrNoSession means no one is using a graphical session now.
var ErrNoSession = errors.New("no one is logged in to a graphical session")

// ErrWayland means the active session is Wayland, which this package
// can't share.
var ErrWayland = errors.New("the active session uses Wayland")

// ErrNoLoginctl means loginctl couldn't list sessions: it isn't
// installed, or systemd-logind isn't running (OpenRC, containers).
var ErrNoLoginctl = errors.New("loginctl couldn't list the sessions")

// Runner runs a command and returns its output (internal/agent/command).
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Finder finds the display of the active graphical session.
type Finder struct {
	Runner Runner
	// ReadFile and Glob read the file system; tests replace them.
	ReadFile func(path string) ([]byte, error)
	Glob     func(pattern string) ([]string, error)
	// HomeOf returns a user's home folder.
	HomeOf func(username string) (string, error)
}

// session is what loginctl says about one session.
type session struct {
	id, kind, display, name, uid, class string
	active                              bool
}

// Find returns the active local X11 session's display, with its cookie
// when one can be found.
func (f Finder) Find(ctx context.Context) (Display, error) {
	sessions, err := f.sessions(ctx)
	if err != nil {
		return Display{}, err
	}
	var wayland bool
	for _, s := range sessions {
		if !s.active || (s.class != "user" && s.class != "greeter") {
			continue
		}
		switch s.kind {
		case "x11":
			return f.display(s)
		case "wayland":
			wayland = true
		}
	}
	if wayland {
		return Display{}, ErrWayland
	}
	return Display{}, ErrNoSession
}

// sessions asks loginctl about every session.
func (f Finder) sessions(ctx context.Context) ([]session, error) {
	list, err := f.Runner.Run(ctx, "loginctl", "list-sessions", "--no-legend")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoLoginctl, err)
	}
	var sessions []session
	for line := range strings.Lines(string(list)) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		output, err := f.Runner.Run(ctx, "loginctl", "show-session", fields[0],
			"-p", "Id", "-p", "Type", "-p", "Active", "-p", "Display", "-p", "Name", "-p", "User", "-p", "Class")
		if err != nil {
			continue // the session ended in between
		}
		sessions = append(sessions, parseSession(output))
	}
	return sessions, nil
}

// parseSession reads loginctl show-session's Key=value lines.
func parseSession(output []byte) session {
	var s session
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		key, value, _ := strings.Cut(scanner.Text(), "=")
		switch key {
		case "Id":
			s.id = value
		case "Type":
			s.kind = value
		case "Active":
			s.active = value == "yes"
		case "Display":
			s.display = value
		case "Name":
			s.name = value
		case "User":
			s.uid = value
		case "Class":
			s.class = value
		}
	}
	return s
}

// display turns an X11 session into the display to connect to.
func (f Finder) display(s session) (Display, error) {
	number, err := ParseDisplay(s.display)
	if err != nil {
		return Display{}, fmt.Errorf("session %s: %w", s.id, err)
	}
	display := Display{Number: number}
	for _, path := range f.authorityFiles(s) {
		file, err := f.ReadFile(path)
		if err != nil {
			continue
		}
		if cookie, err := CookieFor(file, number); err == nil {
			display.Cookie = cookie
			break
		}
	}
	return display, nil
}

// authorityFiles lists where display managers keep a session's
// Xauthority file: GDM, SDDM, LightDM, then the user's home folder.
func (f Finder) authorityFiles(s session) []string {
	runtime := filepath.Join("/run/user", s.uid)
	files := []string{filepath.Join(runtime, "gdm", "Xauthority")}
	if matches, err := f.Glob(filepath.Join(runtime, "xauth_*")); err == nil {
		files = append(files, matches...)
	}
	files = append(files, filepath.Join("/run/lightdm", s.name, "xauthority"))
	if home, err := f.HomeOf(s.name); err == nil {
		files = append(files, filepath.Join(home, ".Xauthority"))
	}
	return files
}

// ErrBadDisplay means a display name isn't ":N" or ":N.S".
var ErrBadDisplay = errors.New("not a local X display name")

// ParseDisplay reads a local display name, ":0" or ":0.0", as its number.
func ParseDisplay(name string) (int, error) {
	rest, ok := strings.CutPrefix(name, ":")
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrBadDisplay, name)
	}
	rest, _, _ = strings.Cut(rest, ".")
	number, err := strconv.Atoi(rest)
	if err != nil || number < 0 {
		return 0, fmt.Errorf("%w: %q", ErrBadDisplay, name)
	}
	return number, nil
}

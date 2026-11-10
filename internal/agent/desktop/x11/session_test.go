// SPDX-License-Identifier: Apache-2.0

package x11_test

import (
	"context"
	"encoding/binary"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/desktop"
	"github.com/aazam476/sentinel/internal/agent/desktop/x11"
)

// authEntry encodes one Xauthority entry.
func authEntry(family uint16, address, number, name string, data []byte) []byte {
	out := binary.BigEndian.AppendUint16(nil, family)
	for _, field := range [][]byte{[]byte(address), []byte(number), []byte(name), data} {
		out = binary.BigEndian.AppendUint16(out, uint16(len(field))) //nolint:gosec // G115: test fields are short
		out = append(out, field...)
	}
	return out
}

var cookie = []byte("0123456789abcdef") //nolint:gochecknoglobals // a fixed test value

const cookieHex = "30313233343536373839616263646566"

func TestCookieFor(t *testing.T) {
	other := authEntry(256, "host", "1", "MIT-MAGIC-COOKIE-1", []byte("fedcba9876543210"))
	tests := []struct {
		name    string
		file    []byte
		want    string
		wantErr error
	}{
		{"finds the display's cookie", append(other, authEntry(256, "host", "0", "MIT-MAGIC-COOKIE-1", cookie)...), cookieHex, nil},
		{"takes a cookie for any display", authEntry(65535, "", "", "MIT-MAGIC-COOKIE-1", cookie), cookieHex, nil},
		{"skips other displays", other, "", x11.ErrNoCookie},
		{"skips network addresses", authEntry(0, "\x0a\x00\x00\x01", "0", "MIT-MAGIC-COOKIE-1", cookie), "", x11.ErrNoCookie},
		{"skips other kinds of authorization", authEntry(256, "host", "0", "XDM-AUTHORIZATION-1", cookie), "", x11.ErrNoCookie},
		{"skips a cookie of the wrong size", authEntry(256, "host", "0", "MIT-MAGIC-COOKIE-1", []byte("short")), "", x11.ErrNoCookie},
		{"says when the file is empty", nil, "", x11.ErrNoCookie},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := x11.CookieFor(test.file, 0)
			if got != test.want || !errors.Is(err, test.wantErr) {
				t.Errorf("CookieFor = %q, %v; want %q, %v", got, err, test.want, test.wantErr)
			}
		})
	}

	t.Run("refuses a truncated file", func(t *testing.T) {
		entry := authEntry(256, "host", "0", "MIT-MAGIC-COOKIE-1", cookie)
		for _, cut := range []int{1, 3, len(entry) - 1} {
			if _, err := x11.CookieFor(entry[:cut], 0); err == nil || errors.Is(err, x11.ErrNoCookie) {
				t.Errorf("cut at %d: err = %v", cut, err)
			}
		}
	})
}

func FuzzCookieFor(f *testing.F) {
	f.Add(authEntry(256, "host", "0", "MIT-MAGIC-COOKIE-1", cookie), 0)
	f.Add([]byte{0xff, 0xff, 0x00}, 1)
	f.Fuzz(func(t *testing.T, file []byte, display int) {
		got, err := x11.CookieFor(file, display)
		if err == nil && len(got) != 32 {
			t.Errorf("cookie %q isn't 16 bytes of hex", got)
		}
	})
}

func TestParseDisplay(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
		ok    bool
	}{
		{"reads a display", ":0", 0, true},
		{"reads a display with a screen", ":12.0", 12, true},
		{"refuses a remote display", "host:0", 0, false},
		{"refuses an empty name", "", 0, false},
		{"refuses a name that isn't a number", ":x", 0, false},
		{"refuses a negative display", ":-1", 0, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := x11.ParseDisplay(test.input)
			if (err == nil) != test.ok || got != test.want {
				t.Errorf("ParseDisplay(%q) = %d, %v", test.input, got, err)
			}
		})
	}
}

// fakeLoginctl answers loginctl with fixed sessions.
type fakeLoginctl struct {
	list     string
	sessions map[string]string
	fail     bool
}

func (f fakeLoginctl) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if f.fail || name != "loginctl" {
		return nil, errors.New("loginctl: command not found")
	}
	if args[0] == "list-sessions" {
		return []byte(f.list), nil
	}
	if output, ok := f.sessions[args[1]]; ok {
		return []byte(output), nil
	}
	return nil, errors.New("no such session")
}

func finder(runner fakeLoginctl, files map[string][]byte) x11.Finder {
	return x11.Finder{
		Runner: runner,
		ReadFile: func(path string) ([]byte, error) {
			if data, ok := files[path]; ok {
				return data, nil
			}
			return nil, fs.ErrNotExist
		},
		Glob: func(pattern string) ([]string, error) {
			var matches []string
			for path := range files {
				if strings.HasPrefix(path, strings.TrimSuffix(pattern, "*")) {
					matches = append(matches, path)
				}
			}
			return matches, nil
		},
		HomeOf: func(name string) (string, error) {
			if name == "" {
				return "", errors.New("unknown user")
			}
			return "/home/" + name, nil
		},
	}
}

func showSession(id, kind, active, display, class string) string {
	return "Id=" + id + "\nType=" + kind + "\nActive=" + active + "\nDisplay=" + display +
		"\nName=ada\nUser=1000\nClass=" + class + "\n"
}

func TestFind(t *testing.T) {
	gdmCookie := authEntry(256, "host", "1", "MIT-MAGIC-COOKIE-1", cookie)
	tests := []struct {
		name    string
		runner  fakeLoginctl
		files   map[string][]byte
		want    x11.Display
		wantErr error
	}{
		{
			name: "finds the active X11 session and its cookie",
			runner: fakeLoginctl{list: "c1 120 gdm seat0 tty1\n4 1000 ada seat0 tty2\n9 1000 ada - pts/0\n", sessions: map[string]string{
				"c1": showSession("c1", "x11", "no", ":0", "greeter"),
				"4":  showSession("4", "x11", "yes", ":1", "user"),
				"9":  showSession("9", "tty", "yes", "", "user"),
			}},
			files: map[string][]byte{"/run/user/1000/gdm/Xauthority": gdmCookie},
			want:  x11.Display{Number: 1, Cookie: cookieHex},
		},
		{
			name:   "finds SDDM's cookie",
			runner: fakeLoginctl{list: "2 1000 ada seat0\n", sessions: map[string]string{"2": showSession("2", "x11", "yes", ":1", "user")}},
			files:  map[string][]byte{"/run/user/1000/xauth_AbCd": gdmCookie},
			want:   x11.Display{Number: 1, Cookie: cookieHex},
		},
		{
			name:   "finds the cookie in the home folder",
			runner: fakeLoginctl{list: "2 1000 ada seat0\n", sessions: map[string]string{"2": showSession("2", "x11", "yes", ":1", "user")}},
			files:  map[string][]byte{"/run/user/1000/gdm/Xauthority": {1}, "/home/ada/.Xauthority": gdmCookie},
			want:   x11.Display{Number: 1, Cookie: cookieHex},
		},
		{
			name:   "connects without a cookie when there is none",
			runner: fakeLoginctl{list: "2 1000 ada seat0\n", sessions: map[string]string{"2": showSession("2", "x11", "yes", ":0", "user")}},
			want:   x11.Display{Number: 0},
		},
		{
			name:    "says when the session is Wayland",
			runner:  fakeLoginctl{list: "2 1000 ada seat0\n", sessions: map[string]string{"2": showSession("2", "wayland", "yes", "", "user")}},
			wantErr: x11.ErrWayland,
		},
		{
			name:    "says when no one is logged in",
			runner:  fakeLoginctl{list: "\n3 1000 ada - pts/0\n", sessions: map[string]string{"3": showSession("3", "tty", "yes", "", "user")}},
			wantErr: x11.ErrNoSession,
		},
		{
			name:    "says when the display can't be read",
			runner:  fakeLoginctl{list: "2 1000 ada seat0\n", sessions: map[string]string{"2": showSession("2", "x11", "yes", "remote:0", "user")}},
			wantErr: x11.ErrBadDisplay,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := finder(test.runner, test.files).Find(t.Context())
			if got != test.want || !errors.Is(err, test.wantErr) {
				t.Errorf("Find = %+v, %v; want %+v, %v", got, err, test.want, test.wantErr)
			}
		})
	}

	t.Run("fails without loginctl", func(t *testing.T) {
		if _, err := finder(fakeLoginctl{fail: true}, nil).Find(t.Context()); err == nil {
			t.Error("found a session without loginctl")
		}
	})
}

func TestScreen(t *testing.T) {
	noSession := fakeLoginctl{list: "3 1000 ada - pts/0\n", sessions: map[string]string{"3": showSession("3", "tty", "yes", "", "user")}}
	wayland := fakeLoginctl{list: "2 1000 ada seat0\n", sessions: map[string]string{"2": showSession("2", "wayland", "yes", "", "user")}}
	tests := []struct {
		name   string
		screen *x11.Screen
		reason string
	}{
		{"says no one is logged in", x11.NewScreen(noSession, "", ""), "No one is logged in to a graphical session."},
		{"says Wayland isn't supported yet", x11.NewScreen(wayland, "", ""), "Wayland"},
		{"says when loginctl can't list sessions", x11.NewScreen(fakeLoginctl{fail: true}, "", ""), "loginctl couldn’t list"},
		{"says when the policy's Xauthority file can't be read", x11.NewScreen(noSession, ":0", "/nonexistent/Xauthority"), "No graphical session was found that"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.screen.Availability(t.Context())
			if got.Available || !strings.Contains(got.Reason, test.reason) || strings.ContainsAny(got.Reason, "\n\r") || len(got.Reason) > 128 {
				t.Errorf("Availability = %+v, want %q", got, test.reason)
			}
			if _, err := test.screen.Open(t.Context()); !errors.Is(err, desktop.ErrUnavailable) {
				t.Errorf("Open = %v, want unavailable", err)
			}
		})
	}
}

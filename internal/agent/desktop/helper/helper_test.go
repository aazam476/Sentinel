// SPDX-License-Identifier: Apache-2.0

package helper_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/desktop"
	"github.com/aazam476/sentinel/internal/agent/desktop/helper"
	"github.com/aazam476/sentinel/internal/protocol"
)

// helperVariable makes the test binary a desktop helper serving the test
// pattern, as `sentinel-agent desktop-helper` serves the screen.
const helperVariable = "SENTINEL_TEST_DESKTOP_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperVariable) != "" {
		if err := helper.Serve(context.Background(), desktop.NewTestPattern(320, 200), os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// connect runs a helper serving source in this process, over pipes, and
// returns its client and Serve's result.
func connect(t *testing.T, source desktop.Source) (*helper.Client, <-chan error) {
	t.Helper()
	toHelper, fromAgent := io.Pipe()
	toAgent, fromHelper := io.Pipe()
	served := make(chan error, 1)
	go func() {
		err := helper.Serve(t.Context(), source, toHelper, fromHelper)
		_ = fromHelper.Close() // the client's reader ends with it
		served <- err
	}()
	client, err := helper.NewClient(fromAgent, toAgent, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return client, served
}

func TestClient(t *testing.T) {
	pattern := desktop.NewTestPattern(320, 200)
	client, served := connect(t, pattern)

	if client.Name() != "test pattern" {
		t.Errorf("Name = %q", client.Name())
	}
	if width, height := client.Size(); width != 320 || height != 200 {
		t.Errorf("Size = %dx%d", width, height)
	}
	frame := image.NewRGBA(image.Rect(0, 0, 320, 200))
	if err := client.Capture(frame); err != nil {
		t.Fatal(err)
	}
	// The first colour bar is light grey.
	if got := frame.RGBAAt(5, 190); got.R != 0xc0 || got.B != 0xc0 {
		t.Errorf("pixel = %v, want the first bar", got)
	}

	if err := client.Input(protocol.DesktopInputEvent{Kind: protocol.InputMouseDown, X: 250, Y: 150}); err != nil {
		t.Fatal(err)
	}
	right := image.NewRGBA(image.Rect(0, 0, 160, 200))
	if err := client.CaptureRegion(right, image.Pt(160, 0)); err == nil {
		t.Error("the test pattern captured a region it can't")
	}
	if err := client.Capture(frame); err != nil {
		t.Fatal(err)
	}
	if got := frame.RGBAAt(258, 150); got.R != 0xff || got.G != 0 || got.B != 0xff {
		t.Errorf("pixel = %v, want the click's magenta mark", got)
	}

	monitors, err := client.Monitors()
	if err != nil || len(monitors) != 2 || monitors[1].Name != "Right half" || monitors[1].X != 160 {
		t.Errorf("monitors = %+v (%v)", monitors, err)
	}

	copied := make(chan string, 4)
	ctx, stopWatching := context.WithCancel(t.Context())
	watching := make(chan struct{})
	go func() { client.WatchClipboard(ctx, func(text string) { copied <- text }); close(watching) }()
	deadline := time.After(5 * time.Second)
	for {
		if err := client.SetClipboard("hello"); err != nil {
			t.Fatal(err)
		}
		select {
		case text := <-copied:
			if text != "Copied on the test pattern: hello" {
				t.Errorf("copied %q", text)
			}
		case <-time.After(50 * time.Millisecond):
			continue
		case <-deadline:
			t.Fatal("no copy came back")
		}
		break
	}
	stopWatching()
	<-watching

	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-served; err != nil {
		t.Errorf("Serve = %v", err)
	}
	if err := client.Input(protocol.DesktopInputEvent{Kind: protocol.InputMouseMove}); err == nil {
		t.Error("a closed helper took input")
	}
}

// plainSource is a desktop with no clipboard or monitors.
type plainSource struct{ desktop.Source }

func TestPlainSource(t *testing.T) {
	client, _ := connect(t, plainSource{desktop.NewTestPattern(64, 64)})
	defer func() { _ = client.Close() }()
	if err := client.SetClipboard("x"); err == nil || !strings.Contains(err.Error(), "no clipboard") {
		t.Errorf("SetClipboard = %v", err)
	}
	if monitors, err := client.Monitors(); err != nil || len(monitors) != 0 {
		t.Errorf("Monitors = %+v, %v", monitors, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client.WatchClipboard(ctx, func(string) { t.Error("a plain desktop copied") })
	if err := client.Input(protocol.DesktopInputEvent{Kind: "teleport"}); err != nil {
		t.Errorf("the test pattern refused unknown input: %v", err) // it ignores what it doesn't know
	}
}

func frame(kind byte, payload []byte) []byte {
	out := binary.BigEndian.AppendUint32(nil, uint32(1+len(payload))) //nolint:gosec // G115: test frames are short
	return append(append(out, kind), payload...)
}

func TestServeRefusesNonsense(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{"answers an unknown request with a failure", frame(99, nil), "malformed"},
		{"answers a bad region with a failure", frame(3, []byte{1, 2, 3}), "malformed"},
		{"answers a huge region with a failure", frame(3, append(make([]byte, 8), 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff)), "malformed"},
		{"answers bad input with a failure", frame(4, []byte("{")), "malformed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := helper.Serve(t.Context(), desktop.NewTestPattern(8, 8), bytes.NewReader(test.input), &out); err != nil {
				t.Fatal(err)
			}
			if out.Len() < 5 || out.Bytes()[4] != 0x81 || !strings.Contains(out.String(), test.want) {
				t.Errorf("reply = %q", out.Bytes())
			}
		})
	}

	t.Run("stops at a frame too big to be one", func(t *testing.T) {
		huge := binary.BigEndian.AppendUint32(nil, 1<<30)
		err := helper.Serve(t.Context(), desktop.NewTestPattern(8, 8), bytes.NewReader(huge), io.Discard)
		if !errors.Is(err, helper.ErrBadMessage) {
			t.Errorf("Serve = %v", err)
		}
	})
}

func TestNewClientWithoutAHelper(t *testing.T) {
	toAgent, fromHelper := io.Pipe()
	_ = fromHelper.Close() // the helper is gone at once
	if _, err := helper.NewClient(nopCloser{io.Discard}, toAgent, func() error { return nil }); err == nil {
		t.Error("a client started with no helper")
	}
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

func TestLaunch(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), executable)
	command.Env = append(os.Environ(), helperVariable+"=1")
	client, err := helper.Launch(command)
	if err != nil {
		t.Fatal(err)
	}
	if width, _ := client.Size(); width != 320 {
		t.Errorf("width = %d from the helper process", width)
	}
	if err := client.Close(); err != nil {
		t.Errorf("Close = %v", err)
	}

	if _, err := helper.Launch(exec.CommandContext(t.Context(), "/nonexistent/desktop-helper")); err == nil {
		t.Error("launched a helper that doesn't exist")
	}
}

func FuzzServe(f *testing.F) {
	f.Add(frame(2, nil))
	f.Add(frame(3, make([]byte, 16)))
	f.Add(frame(4, []byte(`{"kind":"mousemove","x":1,"y":2}`)))
	f.Fuzz(func(t *testing.T, input []byte) {
		_ = helper.Serve(t.Context(), desktop.NewTestPattern(8, 8), bytes.NewReader(input), io.Discard)
	})
}

var _ = bufio.NewReader // the client and helper read with bufio

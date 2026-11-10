// SPDX-License-Identifier: Apache-2.0

package x11_test

import (
	"context"
	"errors"
	"image"
	"image/color"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"

	"github.com/aazam476/sentinel/internal/agent/desktop/x11"
	"github.com/aazam476/sentinel/internal/protocol"
)

// mappedWindows lists the windows shown on the root, by their rectangle.
func mappedWindows(t *testing.T, conn *xgb.Conn, root xproto.Window) []image.Rectangle {
	t.Helper()
	tree, err := xproto.QueryTree(conn, root).Reply()
	if err != nil {
		t.Fatal(err)
	}
	var shown []image.Rectangle
	for _, child := range tree.Children {
		attributes, err := xproto.GetWindowAttributes(conn, child).Reply()
		if err != nil || attributes.MapState != xproto.MapStateViewable {
			continue
		}
		geometry, err := xproto.GetGeometry(conn, xproto.Drawable(child)).Reply()
		if err != nil {
			continue
		}
		shown = append(shown, image.Rect(int(geometry.X), int(geometry.Y), int(geometry.X)+int(geometry.Width), int(geometry.Y)+int(geometry.Height)))
	}
	return shown
}

// waitForWindow waits until a window covers frame, or until none does.
func waitForWindow(t *testing.T, conn *xgb.Conn, root xproto.Window, frame image.Rectangle, shown bool) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for slices.Contains(mappedWindows(t, conn, root), frame) != shown {
		select {
		case <-deadline:
			t.Fatalf("window at %v shown = %v, want %v", frame, !shown, shown)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// waitForPixel waits until the screen's pixel at a point passes check.
func waitForPixel(t *testing.T, source *x11.Source, at image.Point, check func(color.RGBA) bool) {
	t.Helper()
	width, height := source.Size()
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	deadline := time.After(10 * time.Second)
	for {
		if err := source.Capture(picture); err != nil {
			t.Fatal(err)
		}
		if check(picture.RGBAAt(at.X, at.Y)) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("pixel at %v is %v", at, picture.RGBAAt(at.X, at.Y))
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// click presses and releases the left button at a point of the screen.
func click(t *testing.T, source *x11.Source, at image.Point) {
	t.Helper()
	for _, kind := range []string{protocol.InputMouseDown, protocol.InputMouseUp} {
		if err := source.Input(protocol.DesktopInputEvent{Kind: kind, X: at.X, Y: at.Y}); err != nil {
			t.Fatal(err)
		}
	}
}

func center(r image.Rectangle) image.Point {
	return image.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2)
}

type answer struct {
	allowed bool
	err     error
}

func TestSEC23_TheConsentPrompt(t *testing.T) {
	display := startXvfbSized(t, false, "800x600")
	screen := x11.NewScreen(fakeLoginctl{fail: true}, display.Name(), "")
	source := open(t, display)
	conn, root := observer(t, display)
	frame, allow, deny := x11.PromptLayout(800)

	asked := func(ctx context.Context) chan answer {
		answers := make(chan answer, 1)
		go func() {
			allowed, err := screen.Ask(ctx, "admin wants to see and control this screen. Allow it?")
			answers <- answer{allowed, err}
		}()
		waitForWindow(t, conn, root, frame, true)
		return answers
	}

	t.Run("draws the question and says yes to Allow", func(t *testing.T) {
		answers := asked(t.Context())
		// The window shows first and is drawn just after.
		waitForPixel(t, source, frame.Min.Add(allow.Min).Add(image.Pt(3, 3)), func(c color.RGBA) bool { return c.B > 0xc0 && c.R < 0x40 })
		waitForPixel(t, source, frame.Min.Add(image.Pt(5, 5)), func(c color.RGBA) bool { return c.R == 0xff && c.G == 0xff })
		// A click outside the buttons is no answer.
		click(t, source, frame.Min.Add(image.Pt(10, 10)))
		click(t, source, frame.Min.Add(center(allow)))
		if got := <-answers; !got.allowed || got.err != nil {
			t.Errorf("answer = %+v, want allowed", got)
		}
		waitForWindow(t, conn, root, frame, false)
	})

	t.Run("says no to Deny", func(t *testing.T) {
		answers := asked(t.Context())
		click(t, source, frame.Min.Add(center(deny)))
		if got := <-answers; got.allowed || got.err != nil {
			t.Errorf("answer = %+v, want denied", got)
		}
	})

	t.Run("gives up when no one answers", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		answers := asked(ctx)
		cancel()
		if got := <-answers; got.allowed || !errors.Is(got.err, context.Canceled) {
			t.Errorf("answer = %+v, want no answer", got)
		}
		waitForWindow(t, conn, root, frame, false)
	})
}

func TestSEC23_NoOneToAsk(t *testing.T) {
	screen := x11.NewScreen(fakeLoginctl{list: ""}, "", "")
	if allowed, err := screen.Ask(t.Context(), "may I?"); allowed || !errors.Is(err, x11.ErrNoOneToAsk) {
		t.Errorf("Ask = %v, %v", allowed, err)
	}
	if _, err := screen.Show(t.Context(), "on"); !errors.Is(err, x11.ErrNoOneToAsk) {
		t.Errorf("Show = %v", err)
	}
}

func TestSEC24_TheSessionIndicator(t *testing.T) {
	display := startXvfbSized(t, false, "800x600")
	screen := x11.NewScreen(fakeLoginctl{fail: true}, display.Name(), "")
	source := open(t, display)
	conn, root := observer(t, display)
	frame := x11.IndicatorLayout(800)

	shown, err := screen.Show(t.Context(), "Remote session: admin is using a terminal")
	if err != nil {
		t.Fatal(err)
	}
	waitForWindow(t, conn, root, frame, true)
	waitForPixel(t, source, frame.Min.Add(image.Pt(2, 2)), func(c color.RGBA) bool { return c.R > 0xa0 && c.G < 0x40 })
	if err := shown.Close(); err != nil {
		t.Fatal(err)
	}
	_ = shown.Close() // twice is fine
	waitForWindow(t, conn, root, frame, false)
}

func TestWrap(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		limit int
		want  []string
	}{
		{"keeps a short line", "Allow it?", 20, 3, []string{"Allow it?"}},
		{"breaks between words", "one two three four", 9, 3, []string{"one two", "three", "four"}},
		{"cuts a word longer than a line", "abcdefghij", 4, 3, []string{"abcd", "efgh", "ij"}},
		{"ends with an ellipsis when it doesn't fit", "one two three four five", 9, 2, []string{"one two", "three…"}},
		{"gives nothing for nothing", "  ", 9, 2, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := x11.Wrap(test.text, test.width, test.limit); !slices.Equal(got, test.want) {
				t.Errorf("Wrap = %q, want %q", got, test.want)
			}
		})
	}
	if lines := x11.Wrap(strings.Repeat("x", 500), 58, 3); len(lines) != 3 {
		t.Errorf("a long name made %d lines", len(lines))
	}
}

// SPDX-License-Identifier: Apache-2.0

package x11

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"strings"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// The consent prompt (SEC-23) and the session indicator (SEC-24) are
// windows of their own, drawn here and put on the screen as images: X's
// core fonts are often missing, and toolkits would need cgo.
const (
	promptWidth, promptHeight = 460, 132
	promptTop                 = 48
	buttonWidth, buttonHeight = 104, 30
	indicatorWidth            = 460
	indicatorHeight           = 26
	textMargin                = 16
	glyphWidth, lineHeight    = 7, 16
	// maxLines is how much of a question the prompt shows.
	maxLines = 3
)

// ErrNoOneToAsk means there is no desktop to show a question or a notice
// on: no one is logged in to an X11 session.
var ErrNoOneToAsk = errors.New("there is no desktop to ask on")

// errDisplayGone means the X server closed the connection mid-question.
var errDisplayGone = errors.New("the X display closed")

var (
	backgroundColor = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff} //nolint:gochecknoglobals // colours; Go has no const structs
	textColor       = color.RGBA{R: 0x1d, G: 0x1f, B: 0x24, A: 0xff} //nolint:gochecknoglobals // see above
	allowColor      = color.RGBA{R: 0x24, G: 0x53, B: 0xd6, A: 0xff} //nolint:gochecknoglobals // see above
	denyColor       = color.RGBA{R: 0xe4, G: 0xe6, B: 0xeb, A: 0xff} //nolint:gochecknoglobals // see above
	indicatorColor  = color.RGBA{R: 0xb4, G: 0x23, B: 0x18, A: 0xff} //nolint:gochecknoglobals // see above
)

// promptLayout places the prompt at the top middle of a screen width wide,
// and its Allow and Deny buttons within it.
func promptLayout(screenWidth int) (frame, allow, deny image.Rectangle) {
	left := max((screenWidth-promptWidth)/2, 0)
	frame = image.Rect(left, promptTop, left+promptWidth, promptTop+promptHeight)
	top := promptHeight - textMargin - buttonHeight
	deny = image.Rect(promptWidth-textMargin-buttonWidth, top, promptWidth-textMargin, top+buttonHeight)
	allow = deny.Sub(image.Pt(buttonWidth+textMargin/2, 0))
	return frame, allow, deny
}

// indicatorLayout places the indicator at the top middle of the screen.
func indicatorLayout(screenWidth int) image.Rectangle {
	left := max((screenWidth-indicatorWidth)/2, 0)
	return image.Rect(left, 0, left+indicatorWidth, indicatorHeight)
}

// Ask shows question with Allow and Deny on the active desktop, and says
// whether the person there allowed it. It gives up when ctx ends.
func (s *Screen) Ask(ctx context.Context, question string) (bool, error) {
	display, err := s.find(ctx)
	if err != nil {
		return false, fmt.Errorf("%w: %s", ErrNoOneToAsk, reason(err))
	}
	conn, err := dial(display)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	return ask(ctx, conn, question)
}

func ask(ctx context.Context, conn *xgb.Conn, question string) (bool, error) {
	screen := xproto.Setup(conn).DefaultScreen(conn)
	frame, allow, deny := promptLayout(int(screen.WidthInPixels))
	w, err := openWindow(conn, frame, renderPrompt(question, allow, deny))
	if err != nil {
		return false, err
	}
	defer w.close()
	events, stop := watch(conn)
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("no answer: %w", ctx.Err())
		case event, open := <-events:
			if !open {
				return false, errDisplayGone
			}
			switch e := event.(type) {
			case xproto.ExposeEvent:
				_ = w.paint() // a failed repaint shows a blank prompt until the next
			case xproto.ButtonPressEvent:
				at := image.Pt(int(e.EventX), int(e.EventY))
				if at.In(allow) || at.In(deny) {
					return at.In(allow), nil
				}
			}
		}
	}
}

// Show puts text in a bar at the top of the active desktop until the
// returned Closer is closed.
func (s *Screen) Show(ctx context.Context, text string) (io.Closer, error) {
	display, err := s.find(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNoOneToAsk, reason(err))
	}
	conn, err := dial(display)
	if err != nil {
		return nil, err
	}
	screen := xproto.Setup(conn).DefaultScreen(conn)
	w, err := openWindow(conn, indicatorLayout(int(screen.WidthInPixels)), renderIndicator(text))
	if err != nil {
		conn.Close()
		return nil, err
	}
	events, stop := watch(conn)
	notice := &notice{conn: conn, window: w, stop: stop}
	notice.done.Go(func() {
		for event := range events {
			if _, ok := event.(xproto.ExposeEvent); ok {
				_ = w.paint() // as above
			}
		}
	})
	return notice, nil
}

// notice is a shown indicator.
type notice struct {
	conn   *xgb.Conn
	window *window
	stop   func()
	done   sync.WaitGroup
	once   sync.Once
}

// Close takes the indicator away.
func (n *notice) Close() error {
	n.once.Do(func() {
		n.window.close()
		n.conn.Close()
		n.stop()
		n.done.Wait()
	})
	return nil
}

// watch passes on the connection's events until it closes or stop is
// called.
func watch(conn *xgb.Conn) (<-chan xgb.Event, func()) {
	events := make(chan xgb.Event)
	done := make(chan struct{})
	go func() {
		defer close(events)
		for {
			event, err := conn.WaitForEvent()
			if event == nil && err == nil {
				return // the connection closed
			}
			if event == nil {
				continue // an X error for an earlier request; checked requests report their own
			}
			select {
			case events <- event:
			case <-done:
				return
			}
		}
	}()
	var once sync.Once
	return events, func() { once.Do(func() { close(done) }) }
}

// renderPrompt draws the question, wrapped, and the two buttons.
func renderPrompt(question string, allow, deny image.Rectangle) *image.RGBA {
	picture := image.NewRGBA(image.Rect(0, 0, promptWidth, promptHeight))
	draw.Draw(picture, picture.Bounds(), image.NewUniform(textColor), image.Point{}, draw.Src)
	draw.Draw(picture, picture.Bounds().Inset(2), image.NewUniform(backgroundColor), image.Point{}, draw.Src)
	for i, line := range wrap(question, (promptWidth-2*textMargin)/glyphWidth, maxLines) {
		drawText(picture, textMargin, textMargin+12+i*lineHeight, line, textColor)
	}
	button(picture, allow, "Allow", allowColor, backgroundColor)
	button(picture, deny, "Deny", denyColor, textColor)
	return picture
}

// renderIndicator draws the session indicator's bar.
func renderIndicator(text string) *image.RGBA {
	picture := image.NewRGBA(image.Rect(0, 0, indicatorWidth, indicatorHeight))
	draw.Draw(picture, picture.Bounds(), image.NewUniform(indicatorColor), image.Point{}, draw.Src)
	line := wrap(text, (indicatorWidth-2*textMargin)/glyphWidth, 1)
	if len(line) > 0 {
		drawText(picture, textMargin, 17, line[0], backgroundColor)
	}
	return picture
}

func button(picture *image.RGBA, area image.Rectangle, label string, fill, ink color.Color) {
	draw.Draw(picture, area, image.NewUniform(fill), image.Point{}, draw.Src)
	x := area.Min.X + (area.Dx()-len(label)*glyphWidth)/2
	drawText(picture, x, area.Min.Y+20, label, ink)
}

func drawText(picture *image.RGBA, x, y int, text string, ink color.Color) {
	drawer := font.Drawer{Dst: picture, Src: image.NewUniform(ink), Face: basicfont.Face7x13, Dot: fixed.P(x, y)}
	drawer.DrawString(text)
}

// wrap breaks text into at most limit lines of at most width characters,
// ending with "…" when it doesn't all fit.
func wrap(text string, width, limit int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		for len([]rune(word)) > width { // a word longer than a line is cut
			lines, line = appendLine(lines, line), ""
			lines = append(lines, string([]rune(word)[:width]))
			word = string([]rune(word)[width:])
		}
		switch {
		case line == "":
			line = word
		case len([]rune(line))+1+len([]rune(word)) <= width:
			line += " " + word
		default:
			lines, line = append(lines, line), word
		}
	}
	lines = appendLine(lines, line)
	if len(lines) > limit {
		last := []rune(lines[limit-1])
		lines = append(lines[:limit-1], string(last[:min(len(last), width-1)])+"…")
	}
	return lines
}

func appendLine(lines []string, line string) []string {
	if line == "" {
		return lines
	}
	return append(lines, line)
}

// SPDX-License-Identifier: Apache-2.0

package tray_test

import (
	"testing"

	"github.com/aazam476/sentinel/internal/agent/tray"
)

func TestIconsDifferByShapeNotOnlyColour(t *testing.T) {
	const size = 32
	center := func(icon tray.Icon) (r, g, b, a uint8) {
		pixel := tray.IconImage(icon, size).NRGBAAt(size/2, size/2)
		return pixel.R, pixel.G, pixel.B, pixel.A
	}
	tests := []struct {
		name      string
		icon      tray.Icon
		wantAlpha uint8
		wantWhite bool
	}{
		{name: "off is hollow", icon: tray.IconOff, wantAlpha: 0},
		{name: "idle is a solid disc", icon: tray.IconIdle, wantAlpha: 0xff},
		{name: "active has a white dot", icon: tray.IconActive, wantAlpha: 0xff, wantWhite: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r, g, b, a := center(test.icon)
			white := r == 0xff && g == 0xff && b == 0xff
			if a != test.wantAlpha || white != test.wantWhite {
				t.Errorf("center = %d,%d,%d,%d", r, g, b, a)
			}
		})
	}
	t.Run("paused has two white bars and a gap", func(t *testing.T) {
		img := tray.IconImage(tray.IconPaused, size)
		left, gap := img.NRGBAAt(size/2-size/8, size/2), img.NRGBAAt(size/2, size/2)
		if left.R != 0xff || left.G != 0xff || gap.R == 0xff && gap.G == 0xff {
			t.Errorf("bar %v, gap %v", left, gap)
		}
	})
	t.Run("corners are transparent", func(t *testing.T) {
		if corner := tray.IconImage(tray.IconIdle, size).NRGBAAt(0, 0); corner.A != 0 {
			t.Errorf("corner alpha = %d", corner.A)
		}
	})
}

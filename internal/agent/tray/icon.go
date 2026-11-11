// SPDX-License-Identifier: Apache-2.0

package tray

import (
	"image"
	"image/color"
	"math"
)

// iconColor is the icon's colour: grey when off, green when connected,
// red while someone is in, amber when paused.
func iconColor(icon Icon) color.NRGBA {
	switch icon {
	case IconIdle:
		return color.NRGBA{R: 0x22, G: 0x9a, B: 0x5a, A: 0xff}
	case IconActive:
		return color.NRGBA{R: 0xd9, G: 0x3a, B: 0x2b, A: 0xff}
	case IconPaused:
		return color.NRGBA{R: 0xd9, G: 0x8a, B: 0x14, A: 0xff}
	default:
		return color.NRGBA{R: 0x8a, G: 0x8f, B: 0x98, A: 0xff}
	}
}

// IconImage draws the tray icon for a state at size×size pixels: a disc
// in the state's colour, hollow when off, with two bars when paused and a
// dot while a session is open, so the states differ by shape as well as
// colour.
func IconImage(icon Icon, size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	fill := iconColor(icon)
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	center := float64(size) / 2
	radius := center - 0.5
	ring := math.Max(1.5, float64(size)/8)
	for y := range size {
		for x := range size {
			dx, dy := float64(x)+0.5-center, float64(y)+0.5-center
			distance := math.Hypot(dx, dy)
			coverage := clamp01(radius - distance + 0.5)
			if coverage == 0 {
				continue
			}
			pixel := fill
			switch {
			case icon == IconOff && distance < radius-ring:
				continue // hollow
			case icon == IconActive && distance < float64(size)/6:
				pixel = white
			case icon == IconPaused && math.Abs(dy) < float64(size)/4 && math.Abs(math.Abs(dx)-float64(size)/8) < float64(size)/14:
				pixel = white
			}
			pixel.A = uint8(coverage * 255)
			img.SetNRGBA(x, y, pixel)
		}
	}
	return img
}

func clamp01(value float64) float64 {
	return math.Min(1, math.Max(0, value))
}

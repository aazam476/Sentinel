// SPDX-License-Identifier: Apache-2.0

package desktop

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"

	"github.com/aazam476/sentinel/internal/protocol"
)

// tileSize is the side of a screen tile, in pixels.
const tileSize = 64

// Default and allowed JPEG qualities.
const (
	defaultQuality = 70
	minQuality     = 10
	maxQuality     = 95
)

// encoder turns frames into screen updates, sending only the tiles that
// changed since the last frame it encoded.
type encoder struct {
	quality  int
	previous *image.RGBA
	// full makes the next frame send every tile, as after a new transport.
	full bool
}

func newEncoder() *encoder {
	return &encoder{quality: defaultQuality, full: true}
}

// setQuality changes the JPEG quality, within bounds, and sends a full
// frame at it.
func (e *encoder) setQuality(quality int) {
	e.quality = min(max(quality, minQuality), maxQuality)
	e.full = true
}

// encode returns the updates for frame: the screen size when it is new or
// changed, each changed tile, then the end marker. Nothing is returned when
// nothing changed.
func (e *encoder) encode(frame *image.RGBA) ([][]byte, error) {
	var updates [][]byte
	bounds := frame.Bounds()
	if e.previous == nil || e.previous.Bounds() != bounds {
		updates = append(updates, protocol.EncodeScreen(bounds.Dx(), bounds.Dy()))
		e.previous, e.full = image.NewRGBA(bounds), true
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y += tileSize {
		for x := bounds.Min.X; x < bounds.Max.X; x += tileSize {
			tile := image.Rect(x, y, min(x+tileSize, bounds.Max.X), min(y+tileSize, bounds.Max.Y))
			if !e.full && sameTile(frame, e.previous, tile) {
				continue
			}
			data, err := e.encodeTile(frame, tile)
			if err != nil {
				return nil, err
			}
			updates = append(updates, data)
			copyTile(e.previous, frame, tile)
		}
	}
	e.full = false
	if len(updates) == 0 {
		return nil, nil
	}
	return append(updates, protocol.EncodeEnd()), nil
}

func (e *encoder) encodeTile(frame *image.RGBA, tile image.Rectangle) ([]byte, error) {
	var out bytes.Buffer
	if err := jpeg.Encode(&out, frame.SubImage(tile), &jpeg.Options{Quality: e.quality}); err != nil {
		return nil, fmt.Errorf("encode a tile: %w", err)
	}
	return protocol.EncodeTile(tile.Min.X, tile.Min.Y, tile.Dx(), tile.Dy(), out.Bytes()), nil
}

// sameTile says whether a tile is the same in both frames.
func sameTile(a, b *image.RGBA, tile image.Rectangle) bool {
	for y := tile.Min.Y; y < tile.Max.Y; y++ {
		start, end := a.PixOffset(tile.Min.X, y), a.PixOffset(tile.Max.X, y)
		if !bytes.Equal(a.Pix[start:end], b.Pix[start:end]) {
			return false
		}
	}
	return true
}

func copyTile(to, from *image.RGBA, tile image.Rectangle) {
	for y := tile.Min.Y; y < tile.Max.Y; y++ {
		start, end := from.PixOffset(tile.Min.X, y), from.PixOffset(tile.Max.X, y)
		copy(to.Pix[start:end], from.Pix[start:end])
	}
}

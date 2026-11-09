// SPDX-License-Identifier: Apache-2.0

package protocol_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/aazam476/sentinel/internal/protocol"
)

func TestScreenUpdates(t *testing.T) {
	t.Run("round-trips each kind of update", func(t *testing.T) {
		cases := []struct {
			data []byte
			want protocol.ScreenUpdate
		}{
			{protocol.EncodeScreen(2560, 1440), protocol.ScreenUpdate{Kind: protocol.FrameScreen, Width: 2560, Height: 1440}},
			{protocol.EncodeTile(64, 128, 32, 16, []byte("jpeg")), protocol.ScreenUpdate{Kind: protocol.FrameTile, X: 64, Y: 128, Width: 32, Height: 16, JPEG: []byte("jpeg")}},
			{protocol.EncodeEnd(), protocol.ScreenUpdate{Kind: protocol.FrameEnd}},
		}
		for _, tc := range cases {
			got, err := protocol.DecodeScreenUpdate(tc.data)
			if err != nil || got.Kind != tc.want.Kind || got.X != tc.want.X || got.Y != tc.want.Y || got.Width != tc.want.Width ||
				got.Height != tc.want.Height || !bytes.Equal(got.JPEG, tc.want.JPEG) {
				t.Errorf("decode %v = %+v, %v; want %+v", tc.data, got, err, tc.want)
			}
		}
	})

	for name, data := range map[string][]byte{
		"nothing":                 nil,
		"an unknown kind":         {9, 0, 0},
		"a screen size cut short": {protocol.FrameScreen, 0, 1},
		"a tile cut short":        {protocol.FrameTile, 0, 0, 0},
		"an end with more after":  {protocol.FrameEnd, 0},
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			if _, err := protocol.DecodeScreenUpdate(data); !errors.Is(err, protocol.ErrBadFrame) {
				t.Errorf("got %v", err)
			}
		})
	}
}

func FuzzDecodeScreenUpdate(f *testing.F) {
	f.Add(protocol.EncodeScreen(1920, 1080))
	f.Add(protocol.EncodeTile(1, 2, 3, 4, []byte{0xff, 0xd8}))
	f.Add(protocol.EncodeEnd())
	f.Fuzz(func(t *testing.T, data []byte) {
		update, err := protocol.DecodeScreenUpdate(data)
		if err != nil {
			return
		}
		var again []byte
		switch update.Kind {
		case protocol.FrameScreen:
			again = protocol.EncodeScreen(update.Width, update.Height)
		case protocol.FrameTile:
			again = protocol.EncodeTile(update.X, update.Y, update.Width, update.Height, update.JPEG)
		case protocol.FrameEnd:
			again = protocol.EncodeEnd()
		}
		if !bytes.Equal(again, data) {
			t.Errorf("%v decoded to %+v, which encodes to %v", data, update, again)
		}
	})
}

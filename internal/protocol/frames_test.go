// SPDX-License-Identifier: Apache-2.0

package protocol_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/aazam476/sentinel/internal/protocol"
)

func TestFrames(t *testing.T) {
	tests := []struct {
		name   string
		stream protocol.StreamID
		data   []byte
	}{
		{name: "round-trips data", stream: 7, data: []byte("hello")},
		{name: "round-trips an empty frame", stream: 1, data: nil},
		{name: "round-trips the largest stream ID", stream: 0xFFFFFFFF, data: []byte{0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stream, data, err := protocol.DecodeFrame(protocol.EncodeFrame(test.stream, test.data))
			if err != nil || stream != test.stream || !bytes.Equal(data, test.data) {
				t.Errorf("DecodeFrame = %d, %q, %v; want %d, %q", stream, data, err, test.stream, test.data)
			}
		})
	}

	t.Run("writes the stream ID big-endian", func(t *testing.T) {
		if frame := protocol.EncodeFrame(0x01020304, []byte("x")); !bytes.Equal(frame, []byte{1, 2, 3, 4, 'x'}) {
			t.Errorf("frame = %v", frame)
		}
	})

	t.Run("refuses a frame shorter than a stream ID", func(t *testing.T) {
		if _, _, err := protocol.DecodeFrame([]byte{0, 0, 1}); !errors.Is(err, protocol.ErrShortFrame) {
			t.Errorf("error = %v, want ErrShortFrame", err)
		}
	})
}

func FuzzDecodeFrame(f *testing.F) {
	f.Add([]byte{0, 0, 0, 7, 'h', 'i'})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, frame []byte) {
		stream, data, err := protocol.DecodeFrame(frame)
		if err != nil {
			return
		}
		if !bytes.Equal(protocol.EncodeFrame(stream, data), frame) {
			t.Fatalf("re-encoding %v changed it", frame)
		}
	})
}

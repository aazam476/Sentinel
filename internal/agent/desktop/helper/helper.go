// SPDX-License-Identifier: Apache-2.0

package helper

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Message kinds. Requests go from the agent to the helper; the helper
// answers each with ok or failed, and sends copied each time the
// machine's clipboard changes once watched.
const (
	kindName           byte = 1
	kindSize           byte = 2
	kindCapture        byte = 3
	kindInput          byte = 4
	kindSetClipboard   byte = 5
	kindWatchClipboard byte = 6
	kindMonitors       byte = 7

	kindOK     byte = 0x80
	kindFailed byte = 0x81
	kindCopied byte = 0x82
)

// maxFrame bounds a message: an 8K screen captured whole is 132 MiB.
const maxFrame = 256 << 20

// ErrBadMessage means a message can't be read.
var ErrBadMessage = errors.New("malformed desktop helper message")

// writeFrame sends one message.
func writeFrame(w io.Writer, kind byte, payload []byte) error {
	header := make([]byte, 5)
	binary.BigEndian.PutUint32(header, uint32(1+len(payload))) //nolint:gosec // G115: frames stay under maxFrame
	header[4] = kind
	if _, err := w.Write(append(header, payload...)); err != nil {
		return fmt.Errorf("write to the desktop helper: %w", err)
	}
	return nil
}

// readFrame reads one message.
func readFrame(r *bufio.Reader) (byte, []byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, fmt.Errorf("read from the desktop helper: %w", err)
	}
	length := binary.BigEndian.Uint32(header)
	if length == 0 || length > maxFrame {
		return 0, nil, fmt.Errorf("%w: %d bytes", ErrBadMessage, length)
	}
	frame := make([]byte, length)
	if _, err := io.ReadFull(r, frame); err != nil {
		return 0, nil, fmt.Errorf("read from the desktop helper: %w", err)
	}
	return frame[0], frame[1:], nil
}

// region is a capture request's rectangle.
type region struct {
	X, Y          int32
	Width, Height uint32
}

func encodeRegion(r region) []byte {
	out := make([]byte, 0, 16)
	out = binary.BigEndian.AppendUint32(out, uint32(r.X)) //nolint:gosec // G115: two's complement round-trips
	out = binary.BigEndian.AppendUint32(out, uint32(r.Y)) //nolint:gosec // G115: see above
	out = binary.BigEndian.AppendUint32(out, r.Width)
	return binary.BigEndian.AppendUint32(out, r.Height)
}

func decodeRegion(data []byte) (region, error) {
	if len(data) != 16 {
		return region{}, fmt.Errorf("%w: a region of %d bytes", ErrBadMessage, len(data))
	}
	r := region{
		X: int32(binary.BigEndian.Uint32(data)), Y: int32(binary.BigEndian.Uint32(data[4:])), //nolint:gosec // G115: see encodeRegion
		Width: binary.BigEndian.Uint32(data[8:]), Height: binary.BigEndian.Uint32(data[12:]),
	}
	if uint64(r.Width)*uint64(r.Height)*4 > maxFrame {
		return region{}, fmt.Errorf("%w: a region of %dx%d", ErrBadMessage, r.Width, r.Height)
	}
	return r, nil
}

func encodeSize(width, height int) []byte {
	out := binary.BigEndian.AppendUint32(nil, uint32(width))  //nolint:gosec // G115: screen sizes fit
	return binary.BigEndian.AppendUint32(out, uint32(height)) //nolint:gosec // G115: see above
}

func decodeSize(data []byte) (int, int, error) {
	if len(data) != 8 {
		return 0, 0, fmt.Errorf("%w: a size of %d bytes", ErrBadMessage, len(data))
	}
	return int(binary.BigEndian.Uint32(data)), int(binary.BigEndian.Uint32(data[4:])), nil
}

func encodeJSON(value any) []byte {
	encoded, _ := json.Marshal(value) // structs of strings and numbers always encode
	return encoded
}

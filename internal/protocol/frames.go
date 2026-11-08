// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/binary"
	"errors"
)

// StreamID identifies one stream (a terminal session, a file transfer)
// within a connection.
type StreamID uint32

// frameHeaderSize is the 4-byte big-endian stream ID before the data.
const frameHeaderSize = 4

// ErrShortFrame means a binary frame was too short to hold a stream ID. It
// is never reassigned.
var ErrShortFrame = errors.New("binary frame is shorter than its 4-byte stream ID")

// EncodeFrame builds a binary frame: the stream ID, then data.
func EncodeFrame(stream StreamID, data []byte) []byte {
	frame := make([]byte, frameHeaderSize, frameHeaderSize+len(data))
	binary.BigEndian.PutUint32(frame, uint32(stream))
	return append(frame, data...)
}

// DecodeFrame splits a binary frame into its stream ID and data. The data
// shares frame's memory.
func DecodeFrame(frame []byte) (StreamID, []byte, error) {
	if len(frame) < frameHeaderSize {
		return 0, nil, ErrShortFrame
	}
	return StreamID(binary.BigEndian.Uint32(frame)), frame[frameHeaderSize:], nil
}

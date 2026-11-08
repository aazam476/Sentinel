// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/moby/moby/client"

	"github.com/aazam476/sentinel/internal/protocol"
)

// Log limits.
const (
	defaultTail = 500
	maxTail     = 5000
	// maxLineLength cuts very long lines; a container can write anything.
	maxLineLength = 16 << 10
	// maxFrame is the largest multiplexed frame accepted.
	maxFrame = 1 << 20
)

// Logs returns a container's recent lines, oldest first.
func (e *Engine) Logs(ctx context.Context, request protocol.ContainerLogsPayload) (protocol.ContainerLogsResultPayload, error) {
	if err := checkContainer(request.ID); err != nil {
		return protocol.ContainerLogsResultPayload{}, err
	}
	tail := request.Tail
	if tail == 0 {
		tail = defaultTail
	}
	if tail < 1 || tail > maxTail {
		return protocol.ContainerLogsResultPayload{}, fmt.Errorf("%w: tail must be 1 to %d", ErrInvalid, maxTail)
	}
	lines := []protocol.ContainerLogLine{}
	err := e.readLogs(ctx, request.ID, logsOptions(tail, request.Since, false), func(line protocol.ContainerLogLine) {
		lines = append(lines, line)
	})
	if err != nil {
		return protocol.ContainerLogsResultPayload{}, fmt.Errorf("logs of container %s: %w", request.ID, err)
	}
	return protocol.ContainerLogsResultPayload{Lines: lines}, nil
}

func logsOptions(tail int, since int64, follow bool) client.ContainerLogsOptions {
	options := client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Timestamps: true,
		Follow: follow, Tail: strconv.Itoa(tail)}
	if since > 0 {
		options.Since = strconv.FormatFloat(float64(since)/1000, 'f', 3, 64)
	}
	return options
}

// readLogs reads a container's log, calling emit for each line, until the
// log ends (or, when following, until ctx ends).
func (e *Engine) readLogs(ctx context.Context, id string, options client.ContainerLogsOptions, emit func(protocol.ContainerLogLine)) error {
	return e.call(ctx, func(c *client.Client) error {
		inspected, err := c.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
		if err != nil {
			return err
		}
		logs, err := c.ContainerLogs(ctx, id, options)
		if err != nil {
			return err
		}
		defer logs.Close() //nolint:errcheck // read-only body
		// A container with a terminal writes one raw stream; any other
		// writes stdout and stderr multiplexed into frames.
		if inspected.Container.Config != nil && inspected.Container.Config.Tty {
			err = splitLines(logs, "stdout", emit)
		} else {
			err = Demultiplex(logs, emit)
		}
		if ctx.Err() != nil {
			return nil // following ended because the viewer left
		}
		return err
	})
}

// Demultiplex reads Docker's multiplexed log format: frames of an 8-byte
// header (stream type, three zero bytes, big-endian length) and that many
// bytes. Lines are emitted in the order they were written, whichever
// stream they were on.
func Demultiplex(source io.Reader, emit func(protocol.ContainerLogLine)) error {
	header := make([]byte, 8)
	pending := map[string]*bytes.Buffer{"stdout": {}, "stderr": {}}
	reader := bufio.NewReader(source)
	for {
		if _, err := io.ReadFull(reader, header); err != nil {
			flush(pending, emit)
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return fmt.Errorf("read log frame: %w", err)
		}
		stream := "stdout"
		if header[0] == 2 {
			stream = "stderr"
		}
		size := binary.BigEndian.Uint32(header[4:])
		if size > maxFrame {
			return fmt.Errorf("log frame of %d bytes is too large", size)
		}
		frame := make([]byte, size)
		if _, err := io.ReadFull(reader, frame); err != nil {
			frame = frame[:0] // a frame cut short at the end: what was read is lost
		}
		buffer := pending[stream]
		buffer.Write(frame)
		emitComplete(buffer, stream, emit)
	}
}

// emitComplete emits each complete line in buffer, keeping the rest.
func emitComplete(buffer *bytes.Buffer, stream string, emit func(protocol.ContainerLogLine)) {
	for {
		index := bytes.IndexByte(buffer.Bytes(), '\n')
		if index < 0 {
			if buffer.Len() > maxLineLength { // a line with no end in sight
				emit(parseLine(stream, buffer.Next(buffer.Len())))
			}
			return
		}
		emit(parseLine(stream, buffer.Next(index+1)))
	}
}

func flush(pending map[string]*bytes.Buffer, emit func(protocol.ContainerLogLine)) {
	for _, stream := range []string{"stdout", "stderr"} {
		if buffer := pending[stream]; buffer.Len() > 0 {
			emit(parseLine(stream, buffer.Bytes()))
			buffer.Reset()
		}
	}
}

// splitLines reads a terminal's raw log line by line.
func splitLines(source io.Reader, stream string, emit func(protocol.ContainerLogLine)) error {
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 64<<10), maxFrame)
	for scanner.Scan() {
		emit(parseLine(stream, scanner.Bytes()))
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read log: %w", err)
	}
	return nil
}

// parseLine splits off the RFC 3339 timestamp Docker puts first when asked
// for timestamps, and cleans the text for display.
func parseLine(stream string, raw []byte) protocol.ContainerLogLine {
	text := strings.TrimRight(string(raw), "\r\n")
	line := protocol.ContainerLogLine{Stream: stream, Text: text}
	if stamp, rest, found := strings.Cut(text, " "); found {
		if at, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
			line.Time, line.Text = at.UnixMilli(), rest
		}
	}
	if len(line.Text) > maxLineLength {
		line.Text = line.Text[:maxLineLength]
	}
	line.Text = strings.ToValidUTF8(line.Text, string(utf8.RuneError))
	return line
}

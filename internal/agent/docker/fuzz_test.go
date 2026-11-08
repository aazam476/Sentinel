// SPDX-License-Identifier: Apache-2.0

package docker_test

import (
	"bytes"
	"os"
	"testing"
	"unicode/utf8"

	"github.com/aazam476/sentinel/internal/agent/docker"
	"github.com/aazam476/sentinel/internal/protocol"
)

// FuzzDemultiplex feeds the log demultiplexer anything a container engine
// could send: it must not panic, and every line it emits must be valid
// text of bounded length.
func FuzzDemultiplex(f *testing.F) {
	for _, name := range []string{"logs-web.bin", "logs-tty.bin"} {
		data, err := os.ReadFile(fixtures + name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		_ = docker.Demultiplex(bytes.NewReader(input), func(line protocol.ContainerLogLine) {
			if !utf8.ValidString(line.Text) || len(line.Text) > 16<<10 {
				t.Errorf("bad line: %d bytes, valid %v", len(line.Text), utf8.ValidString(line.Text))
			}
			if line.Stream != "stdout" && line.Stream != "stderr" {
				t.Errorf("stream %q", line.Stream)
			}
		})
	})
}

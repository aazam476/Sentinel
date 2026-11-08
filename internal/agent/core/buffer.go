// SPDX-License-Identifier: Apache-2.0

package core

import (
	"sync"

	"github.com/aazam476/sentinel/internal/protocol"
)

// sampleBuffer holds metrics samples until they reach the server, keeping
// the newest ones when it is full (§7.1: up to an hour while disconnected).
type sampleBuffer struct {
	mutex    sync.Mutex
	samples  []protocol.MetricsSample
	capacity int
	// added gets a value when a sample is added, to wake the sender.
	added chan struct{}
}

func newSampleBuffer(capacity int) *sampleBuffer {
	return &sampleBuffer{capacity: capacity, added: make(chan struct{}, 1)}
}

func (b *sampleBuffer) add(sample protocol.MetricsSample) {
	b.mutex.Lock()
	b.samples = append(b.samples, sample)
	if len(b.samples) > b.capacity {
		b.samples = b.samples[len(b.samples)-b.capacity:]
	}
	b.mutex.Unlock()
	select {
	case b.added <- struct{}{}:
	default: // the sender is already due to wake
	}
}

// oldest returns the oldest sample without removing it.
func (b *sampleBuffer) oldest() (protocol.MetricsSample, bool) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if len(b.samples) == 0 {
		return protocol.MetricsSample{}, false
	}
	return b.samples[0], true
}

// remove drops sample once it was sent, unless it already fell out of a
// full buffer.
func (b *sampleBuffer) remove(sample protocol.MetricsSample) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if len(b.samples) > 0 && b.samples[0].Time == sample.Time {
		b.samples = b.samples[1:]
	}
}

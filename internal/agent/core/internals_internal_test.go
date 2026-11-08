// SPDX-License-Identifier: Apache-2.0

package core

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
)

func TestBackoff(t *testing.T) {
	t.Run("doubles from 1 second up to a minute", func(t *testing.T) {
		retry := newBackoff(func() float64 { return 0.5 }) // no jitter
		want := []time.Duration{1, 2, 4, 8, 16, 32, 60, 60}
		for i, seconds := range want {
			if got := retry.delay(); got != seconds*time.Second {
				t.Errorf("attempt %d: %v, want %v", i+1, got, seconds*time.Second)
			}
		}
		retry.reset()
		if got := retry.delay(); got != time.Second {
			t.Errorf("after reset: %v, want 1s", got)
		}
	})

	t.Run("spreads each delay by up to 20% either way", func(t *testing.T) {
		low, high := newBackoff(func() float64 { return 0 }), newBackoff(func() float64 { return 0.999999 })
		if got := low.delay(); got != 800*time.Millisecond {
			t.Errorf("lowest = %v, want 800ms", got)
		}
		if got := high.delay(); got < 1199*time.Millisecond || got > 1200*time.Millisecond {
			t.Errorf("highest = %v, want just under 1.2s", got)
		}
	})
}

func TestSampleBuffer(t *testing.T) {
	t.Run("keeps an hour of samples, dropping the oldest", func(t *testing.T) {
		buffer := newSampleBuffer(int(bufferedTime / defaultMetricsInterval))
		for i := range 400 {
			buffer.add(protocol.MetricsSample{Time: int64(i)})
		}
		oldest, ok := buffer.oldest()
		if !ok || oldest.Time != 40 {
			t.Errorf("oldest = %d, want 40 (360 kept of 400)", oldest.Time)
		}
	})

	t.Run("removes a sample only if it is still the oldest", func(t *testing.T) {
		buffer := newSampleBuffer(2)
		first := protocol.MetricsSample{Time: 1}
		buffer.add(first)
		buffer.add(protocol.MetricsSample{Time: 2})
		buffer.add(protocol.MetricsSample{Time: 3}) // pushes the first out while it is being sent
		buffer.remove(first)
		if oldest, _ := buffer.oldest(); oldest.Time != 2 {
			t.Errorf("oldest = %d, want 2", oldest.Time)
		}
		buffer.remove(protocol.MetricsSample{Time: 2})
		buffer.remove(protocol.MetricsSample{Time: 3})
		if _, ok := buffer.oldest(); ok {
			t.Error("the buffer isn't empty")
		}
		buffer.remove(first) // nothing to remove: harmless
	})
}

func TestReportSessionEnd(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{ErrServerIdentity, "identity check"},
		{websocket.CloseError{Code: protocol.CloseRevoked}, "revoked"},
		{websocket.CloseError{Code: protocol.CloseUnknownAgent}, "doesn't know this agent"},
		{websocket.CloseError{Code: protocol.CloseUnsupportedVersion}, "update the agent"},
		{websocket.CloseError{Code: protocol.CloseHandshakeFailed}, "refused this agent's identity"},
		{websocket.CloseError{Code: websocket.StatusServiceRestart}, "reconnecting"},
	}
	for _, test := range tests {
		t.Run("explains "+test.want, func(t *testing.T) {
			var log strings.Builder
			agent := New(Options{Logger: slog.New(slog.NewTextHandler(&log, nil))})
			agent.reportSessionEnd(test.err)
			if !strings.Contains(log.String(), test.want) {
				t.Errorf("log = %q, want %q", log.String(), test.want)
			}
		})
	}
}

func TestExplainRefusal(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{websocket.CloseError{Code: protocol.CloseEnrollmentRefused}, "refused the enrollment token"},
		{websocket.CloseError{Code: protocol.CloseUnsupportedVersion}, "doesn't support this agent version"},
		{errors.New("connection reset"), "enrollment failed"},
	}
	for _, test := range tests {
		t.Run("says "+test.want, func(t *testing.T) {
			if got := explainRefusal(test.err); !strings.Contains(got.Error(), test.want) {
				t.Errorf("got %q", got)
			}
		})
	}
}

func TestMalformedStreamMessages(t *testing.T) {
	// The terminal manager is nil: reaching it would panic, so these pass
	// only if the malformed messages are dropped first.
	c := &connection{agent: &Agent{options: Options{Logger: slog.New(slog.DiscardHandler)}}}

	t.Run("drops a stream frame too short to name its stream", func(t *testing.T) {
		c.handleFrame([]byte{0, 1})
	})

	t.Run("drops a stream.close whose payload can't be read", func(t *testing.T) {
		c.handleStreamClose(protocol.Message{Type: protocol.TypeStreamClose, Payload: []byte(`"not an object"`)})
	})
}

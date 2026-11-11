// SPDX-License-Identifier: Apache-2.0

package tray

import (
	"context"
	"io"
)

// Notifier asks the person at the machine and shows them sessions are
// open (the agent's core.Notifier).
type Notifier interface {
	Ask(ctx context.Context, question string) (bool, error)
	Show(ctx context.Context, text string) (io.Closer, error)
}

// WithFallback asks through the trays when one is connected, and through
// fallback (the machine's own way, such as X11) otherwise. fallback may be
// nil.
func WithFallback(trays *Server, fallback Notifier) Notifier {
	return withFallback{trays: trays, fallback: fallback}
}

type withFallback struct {
	trays    *Server
	fallback Notifier
}

func (n withFallback) Ask(ctx context.Context, question string) (bool, error) {
	if n.trays.Connected() || n.fallback == nil {
		return n.trays.Ask(ctx, question)
	}
	return n.fallback.Ask(ctx, question)
}

func (n withFallback) Show(ctx context.Context, text string) (io.Closer, error) {
	if n.trays.Connected() || n.fallback == nil {
		return n.trays.Show(ctx, text)
	}
	return n.fallback.Show(ctx, text)
}

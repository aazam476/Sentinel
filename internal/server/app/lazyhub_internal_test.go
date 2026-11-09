// SPDX-License-Identifier: Apache-2.0

package app

import (
	"log/slog"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/hub"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

func TestLazyHub(t *testing.T) {
	built := hub.New(hub.Options{Clock: fakeclock.New(time.Now()), Logger: slog.New(slog.DiscardHandler)})
	lazy := lazyHub{func() *hub.Hub { return built }}
	if lazy.IsOnline(1) {
		t.Error("an agent that never connected is online")
	}
	if err := lazy.Request(t.Context(), 1, "system.info", nil, nil); err == nil {
		t.Error("a request to an offline agent succeeded")
	}
}

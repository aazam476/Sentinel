// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"errors"
	"log/slog"
	"math/rand/v2"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/agent/docker"
	"github.com/aazam476/sentinel/internal/agent/identity"
	"github.com/aazam476/sentinel/internal/agent/logs"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/agent/power"
	"github.com/aazam476/sentinel/internal/agent/processes"
	"github.com/aazam476/sentinel/internal/agent/services"
	"github.com/aazam476/sentinel/internal/agent/terminal"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// Defaults until the server's welcome says otherwise.
const (
	defaultMetricsInterval = 10 * time.Second
	defaultPingInterval    = 15 * time.Second
	// silenceLimit is how long a connection may stay silent before the
	// agent gives up on it: three missed pongs.
	silenceLimit = 45 * time.Second
	// bufferedTime is how much history the agent keeps while disconnected.
	bufferedTime = time.Hour
)

// Collector reads the machine (internal/agent/collectors).
type Collector interface {
	Sample(ctx context.Context, now time.Time) protocol.MetricsSample
	SystemInfo(ctx context.Context, capabilities []protocol.Capability, agentVersion string) (protocol.SystemInfo, error)
}

// Clock tells the time and makes timers.
type Clock interface {
	Now() time.Time
	NewTimer(duration time.Duration) clock.Timer
}

// Options are what New needs.
type Options struct {
	State     identity.State
	Key       ed25519.PrivateKey
	Policy    policy.Policy
	Collector Collector
	Clock     Clock
	Logger    *slog.Logger
	Version   string
	// TLSConfig overrides the TLS settings built from State.TLSFingerprint.
	TLSConfig *tls.Config
	// Random returns a number in [0, 1) for jitter; nil uses math/rand.
	Random func() float64
	// Shells starts the terminal's shells; nil uses this machine's.
	Shells terminal.Starter
	// Services, Logs, Processes and Power manage the machine (§9.6); nil
	// uses this machine's.
	Services  services.Manager
	Logs      logs.Reader
	Processes Processes
	Power     Power
	// Docker manages containers (§9.4); nil uses the engine the local
	// policy names, or the usual sockets.
	Docker Docker
	// Observe, if set, is told when the agent reaches each Event. Tests
	// use it to know when to move their fake clock.
	Observe func(Event)
}

// Processes lists and ends processes (internal/agent/processes).
type Processes interface {
	List(ctx context.Context) (protocol.ProcessListPayload, error)
	Kill(ctx context.Context, request protocol.ProcessKillPayload) error
}

// Docker manages containers, images, volumes and networks
// (internal/agent/docker).
type Docker interface {
	Containers(ctx context.Context) (protocol.ContainerListPayload, error)
	DoContainer(ctx context.Context, action docker.ContainerAction, request protocol.ContainerPayload) error
	Inspect(ctx context.Context, id string) (protocol.ContainerInspectPayload, error)
	Logs(ctx context.Context, request protocol.ContainerLogsPayload) (protocol.ContainerLogsResultPayload, error)
	Images(ctx context.Context) (protocol.ImageListPayload, error)
	Pull(ctx context.Context, request protocol.ImageRefPayload) (protocol.ImagePulledPayload, error)
	RemoveImage(ctx context.Context, request protocol.ImageRefPayload) error
	CheckImage(ctx context.Context, request protocol.ImageRefPayload) (protocol.ImageCheckPayload, error)
	Volumes(ctx context.Context) (protocol.VolumeListPayload, error)
	RemoveVolume(ctx context.Context, name string) error
	Networks(ctx context.Context) (protocol.NetworkListPayload, error)
	RemoveNetwork(ctx context.Context, name string) error
}

// Power reboots and shuts down the machine (internal/agent/power).
type Power interface {
	Schedule(action power.Action) (<-chan error, error)
}

// Event is a point the agent reports reaching, once the timers it then
// waits on are running.
type Event string

// The events.
const (
	// EventConnecting: a connection attempt started; its time limit runs.
	EventConnecting Event = "connecting"
	// EventSampled: a metrics sample was taken and the next is scheduled.
	EventSampled Event = "sampled"
	// EventConnected: the handshake succeeded and the connection is served.
	EventConnected Event = "connected"
	// EventWaiting: a connection ended or failed; the retry is scheduled.
	EventWaiting Event = "waiting"
)

func (a *Agent) observe(event Event) {
	if a.options.Observe != nil {
		a.options.Observe(event)
	}
}

// Agent keeps one machine connected to its server.
type Agent struct {
	options   Options
	tlsConfig *tls.Config
	buffer    *sampleBuffer
	terminals *terminal.Manager
	// metricsInterval is how often to sample, as the server last said.
	metricsInterval atomic.Int64
}

// New returns an Agent; call Run to connect.
func New(options Options) *Agent {
	if options.Random == nil {
		options.Random = rand.Float64
	}
	if options.Shells == nil {
		options.Shells = terminal.NewStarter()
	}
	withMachineDefaults(&options)
	tlsConfig := options.TLSConfig
	if tlsConfig == nil {
		tlsConfig = TLSConfig(options.State.TLSFingerprint)
	}
	agent := &Agent{
		options: options, tlsConfig: tlsConfig,
		buffer:    newSampleBuffer(int(bufferedTime / defaultMetricsInterval)),
		terminals: terminal.NewManager(options.Shells, options.Clock, options.Logger),
	}
	agent.metricsInterval.Store(int64(defaultMetricsInterval))
	return agent
}

// withMachineDefaults fills in the parts that act on the machine itself.
func withMachineDefaults(options *Options) {
	runner := command.Exec{}
	if options.Services == nil {
		options.Services = services.New(runtime.GOOS, runner, options.Clock)
	}
	if options.Logs == nil {
		options.Logs = logs.New(runtime.GOOS, runner, options.Clock)
	}
	if options.Processes == nil {
		options.Processes = processes.New(processes.System{}, options.Clock, int32(os.Getpid())) //nolint:gosec // G115: PIDs fit in 32 bits on every supported system
	}
	if options.Power == nil {
		options.Power = power.New(runtime.GOOS, runner, options.Clock)
	}
	if options.Docker == nil {
		options.Docker = docker.New(options.Policy.Docker.Socket, runtime.GOOS)
	}
}

// Run keeps the agent connected until ctx ends.
func (a *Agent) Run(ctx context.Context) {
	defer a.terminals.CloseAll()
	sampling := make(chan struct{})
	// The timer starts here, not in the goroutine, so the first sample is
	// due a full interval after Run starts however late the goroutine runs.
	timer := a.options.Clock.NewTimer(time.Duration(a.metricsInterval.Load()))
	go func() { a.sampleForever(ctx, timer); close(sampling) }()
	defer func() { <-sampling }()

	retry := newBackoff(a.options.Random)
	for {
		welcomed, err := a.session(ctx)
		if ctx.Err() != nil {
			return
		}
		a.reportSessionEnd(err)
		if welcomed {
			retry.reset()
		}
		if !a.wait(ctx, retry.delay()) {
			return
		}
	}
}

// sampleForever takes a sample every metrics interval, connected or not.
func (a *Agent) sampleForever(ctx context.Context, timer clock.Timer) {
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C():
		}
		a.buffer.add(a.options.Collector.Sample(ctx, a.options.Clock.Now()))
		timer.Reset(time.Duration(a.metricsInterval.Load()))
		a.observe(EventSampled)
	}
}

// wait sleeps on the clock for delay, returning false if ctx ends first.
func (a *Agent) wait(ctx context.Context, delay time.Duration) bool {
	timer := a.options.Clock.NewTimer(delay)
	defer timer.Stop()
	a.observe(EventWaiting)
	select {
	case <-ctx.Done():
		return false
	case <-timer.C():
		return true
	}
}

// reportSessionEnd logs why a connection ended, loudly when retrying won't
// help until a person does something.
func (a *Agent) reportSessionEnd(err error) {
	logger := a.options.Logger
	switch code := websocket.CloseStatus(err); {
	case errors.Is(err, ErrServerIdentity):
		logger.Error("the server failed its identity check; refusing to talk to it. "+
			"If the server was reinstalled, enroll this agent again", "error", err)
	case code == protocol.CloseRevoked:
		logger.Error("an admin revoked this agent; enroll it again to bring it back")
	case code == protocol.CloseUnknownAgent:
		logger.Error("the server doesn't know this agent; enroll it again")
	case code == protocol.CloseUnsupportedVersion:
		logger.Error("the server doesn't support this agent's version; update the agent")
	case code == protocol.CloseHandshakeFailed:
		logger.Error("the server refused this agent's identity; enroll it again", "error", err)
	default:
		logger.Info("disconnected from the server; reconnecting", "error", err)
	}
}

// SPDX-License-Identifier: Apache-2.0

package tray

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/aazam476/sentinel/internal/shared/clock"
)

// retryInterval is how often a tray that can't reach the agent tries again.
const retryInterval = 5 * time.Second

// UI is the tray's native face: the Windows notification area icon or the
// macOS menu bar item (internal/agent/tray/winui, macui).
type UI interface {
	// Update shows the model.
	Update(model Model)
	// Ask puts a yes-or-no question on the screen until it is answered or
	// ctx ends.
	Ask(ctx context.Context, question string) (bool, error)
	// Notify shows a passing notification.
	Notify(text string)
}

// Clock tells the time and makes timers.
type Clock interface {
	Now() time.Time
	NewTimer(duration time.Duration) clock.Timer
}

// ClientOptions configure RunClient.
type ClientOptions struct {
	// Dial connects to the agent's tray socket.
	Dial   func(ctx context.Context) (net.Conn, error)
	UI     UI
	Clock  Clock
	Logger *slog.Logger
}

// RunClient keeps the tray connected to the agent until ctx ends, showing
// its status and carrying the person's picks from actions to it.
func RunClient(ctx context.Context, options ClientOptions, actions <-chan Action) {
	seen := map[string]bool{}
	for {
		conn, err := options.Dial(ctx)
		if err == nil {
			err = serveAgent(ctx, options, conn, actions, seen)
		}
		if ctx.Err() != nil {
			return
		}
		options.Logger.Debug("not connected to the agent", "error", err)
		options.UI.Update(Menu(Status{}, false, options.Clock.Now()))
		if !waitFor(ctx, options.Clock, retryInterval, actions) {
			return
		}
	}
}

// waitFor waits out the retry, discarding picks made while the agent
// can't be reached. It reports false when ctx ends.
func waitFor(ctx context.Context, clock Clock, duration time.Duration, actions <-chan Action) bool {
	timer := clock.NewTimer(duration)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-timer.C():
			return true
		case <-actions:
		}
	}
}

// agentLink is one connection to the agent.
type agentLink struct {
	options ClientOptions
	conn    net.Conn
	writing sync.Mutex
	// asks cancels each question on the screen, by ID.
	asksMutex sync.Mutex
	asks      map[string]context.CancelFunc
}

// serveAgent shows what the agent says and sends it the person's picks
// until the connection ends.
func serveAgent(ctx context.Context, options ClientOptions, conn net.Conn, actions <-chan Action, seen map[string]bool) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	link := &agentLink{options: options, conn: conn, asks: map[string]context.CancelFunc{}}
	defer link.cancelAsks()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() }) // unblocks the reader
	defer stop()
	defer func() { _ = conn.Close() }() // done with it either way

	messages, failed := readMessages(ctx, conn)
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("tray stopping: %w", ctx.Err())
		case err := <-failed:
			if errors.Is(err, io.EOF) {
				return errors.New("the agent closed the connection")
			}
			return err
		case message := <-messages:
			link.receive(ctx, message, seen)
		case action := <-actions:
			if err := link.send(request(action)); err != nil {
				return err
			}
		}
	}
}

// readMessages reads the agent's messages until the connection fails,
// which it then reports on failed.
func readMessages(ctx context.Context, conn net.Conn) (messages <-chan Message, failed <-chan error) {
	out, errs := make(chan Message), make(chan error, 1)
	go func() {
		reader := NewReader(conn)
		for {
			message, err := reader.Read()
			if err != nil {
				errs <- err
				return
			}
			select {
			case out <- message:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, errs
}

// request turns a pick into the message that asks the agent for it.
func request(action Action) Message {
	switch action.Kind {
	case ActionEnd:
		return Message{Type: TypeEnd, ID: action.Session}
	case ActionPause:
		return Message{Type: TypePause, Paused: true}
	default:
		return Message{Type: TypePause, Paused: false}
	}
}

// receive handles one message from the agent.
func (l *agentLink) receive(ctx context.Context, message Message, seen map[string]bool) {
	switch message.Type {
	case TypeStatus:
		now := l.options.Clock.Now()
		l.options.UI.Update(Menu(*message.Status, true, now))
		for _, session := range message.Status.Sessions {
			if !seen[session.ID] {
				seen[session.ID] = true
				l.options.UI.Notify(announcement(session))
			}
		}
	case TypeAsk:
		l.ask(ctx, message)
	case TypeAsked:
		l.asksMutex.Lock()
		if cancel, open := l.asks[message.ID]; open {
			cancel()
		}
		l.asksMutex.Unlock()
	default:
		l.options.Logger.Debug("ignored a message for the agent's side", "type", message.Type)
	}
}

// ask shows the question until the person answers, then sends the answer.
func (l *agentLink) ask(ctx context.Context, message Message) {
	askCtx, cancel := context.WithCancel(ctx)
	l.asksMutex.Lock()
	l.asks[message.ID] = cancel
	l.asksMutex.Unlock()
	go func() {
		defer func() {
			l.asksMutex.Lock()
			delete(l.asks, message.ID)
			l.asksMutex.Unlock()
			cancel()
		}()
		allowed, err := l.options.UI.Ask(askCtx, message.Question)
		if err != nil || askCtx.Err() != nil {
			return // answered elsewhere, timed out or the tray is stopping
		}
		if err := l.send(Message{Type: TypeAnswer, ID: message.ID, Allow: allowed}); err != nil {
			l.options.Logger.Info("couldn't send the answer to the agent", "error", err)
		}
	}()
}

func (l *agentLink) cancelAsks() {
	l.asksMutex.Lock()
	defer l.asksMutex.Unlock()
	for _, cancel := range l.asks {
		cancel()
	}
}

// send writes one message to the agent.
func (l *agentLink) send(message Message) error {
	line, err := Encode(message)
	if err != nil {
		return err
	}
	l.writing.Lock()
	defer l.writing.Unlock()
	if _, err := l.conn.Write(line); err != nil {
		return fmt.Errorf("write to the agent: %w", err)
	}
	return nil
}

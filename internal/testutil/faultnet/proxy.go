// SPDX-License-Identifier: Apache-2.0

package faultnet

import (
	"context"
	"net"
	"sync"
	"testing"
)

// Proxy forwards connections from its own address to a target address.
type Proxy struct {
	listener net.Listener

	mutex    sync.Mutex
	target   string
	links    map[*link]struct{}
	refusing bool
	// flowing is closed while traffic flows, and open while it is stalled.
	flowing chan struct{}
}

// link is one forwarded connection.
type link struct {
	client, server net.Conn
	once           sync.Once
	// closed is closed with the link, to release a copy held by a stall.
	closed chan struct{}
}

func (l *link) close() {
	l.once.Do(func() {
		close(l.closed)
		_ = l.client.Close() // closing a closed connection is harmless
		_ = l.server.Close()
	})
}

// New starts a proxy to target on a free local port. It stops when the
// test ends.
func New(t testing.TB, target string) *Proxy {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	flowing := make(chan struct{})
	close(flowing)
	p := &Proxy{listener: listener, target: target, links: map[*link]struct{}{}, flowing: flowing}
	done := make(chan struct{})
	go func() { p.accept(); close(done) }()
	t.Cleanup(func() {
		_ = listener.Close() // stops accept
		<-done
		p.Drop()
	})
	return p
}

// Addr is the address clients connect to instead of the target.
func (p *Proxy) Addr() string {
	return p.listener.Addr().String()
}

// SetTarget sends new connections to another address, as when a server
// comes back on a different port after a restart.
func (p *Proxy) SetTarget(target string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.target = target
}

// Drop cuts every open connection, as a network drop would.
func (p *Proxy) Drop() {
	p.mutex.Lock()
	links := p.links
	p.links = map[*link]struct{}{}
	p.mutex.Unlock()
	for l := range links {
		l.close()
	}
}

// Refuse makes new connections fail at once, as with a server that is down.
func (p *Proxy) Refuse(refuse bool) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.refusing = refuse
}

// Stall stops or restarts all traffic without closing anything.
func (p *Proxy) Stall(stall bool) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	stalled := p.isStalledLocked()
	switch {
	case stall && !stalled:
		p.flowing = make(chan struct{})
	case !stall && stalled:
		close(p.flowing)
	}
}

// Connections is how many connections are open through the proxy.
func (p *Proxy) Connections() int {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return len(p.links)
}

func (p *Proxy) isStalledLocked() bool {
	select {
	case <-p.flowing:
		return false
	default:
		return true
	}
}

func (p *Proxy) accept() {
	for {
		client, err := p.listener.Accept()
		if err != nil {
			return // the proxy is closed
		}
		go p.forward(client)
	}
}

func (p *Proxy) forward(client net.Conn) {
	p.mutex.Lock()
	target, refusing := p.target, p.refusing
	p.mutex.Unlock()
	if refusing {
		_ = client.Close() // refused, as asked
		return
	}
	// The proxy outlives any one test step, so its dials have no deadline.
	server, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", target)
	if err != nil {
		_ = client.Close() // the target is down: so is this connection
		return
	}
	l := &link{client: client, server: server, closed: make(chan struct{})}
	p.mutex.Lock()
	p.links[l] = struct{}{}
	p.mutex.Unlock()

	var copies sync.WaitGroup
	copies.Go(func() { p.copy(l, server, client); l.close() })
	copies.Go(func() { p.copy(l, client, server); l.close() })
	copies.Wait()

	p.mutex.Lock()
	delete(p.links, l)
	p.mutex.Unlock()
}

// copy moves data from source to destination, holding it back while the
// proxy is stalled.
func (p *Proxy) copy(l *link, destination, source net.Conn) {
	buffer := make([]byte, 32<<10)
	for {
		n, err := source.Read(buffer)
		if n > 0 {
			p.mutex.Lock()
			flowing := p.flowing
			p.mutex.Unlock()
			select {
			case <-flowing:
			case <-l.closed:
				return
			}
			if _, err := destination.Write(buffer[:n]); err != nil {
				return
			}
		}
		if err != nil {
			return // closed by either side, or by Drop
		}
	}
}

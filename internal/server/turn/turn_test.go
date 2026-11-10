// SPDX-License-Identifier: Apache-2.0

package turn_test

import (
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	pionturn "github.com/pion/turn/v5"
	"github.com/pion/webrtc/v4"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/turn"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

func start(t *testing.T, clock *fakeclock.Clock, relayIP string) *turn.Relay {
	t.Helper()
	relay, err := turn.Start(t.Context(), turn.Options{
		Listen: "127.0.0.1:0", RelayIP: netip.MustParseAddr(relayIP), Clock: clock, Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = relay.Close() })
	return relay
}

// allocate asks the relay for a relayed address with the given credentials.
func allocate(t *testing.T, server protocol.ICEServer) error {
	t.Helper()
	_, err := allocateAddress(t, server)
	return err
}

// allocateAddress allocates and returns the relayed address.
func allocateAddress(t *testing.T, server protocol.ICEServer) (net.Addr, error) {
	t.Helper()
	conn, err := (&net.ListenConfig{}).ListenPacket(t.Context(), "udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	turnAddress := strings.TrimSuffix(strings.TrimPrefix(server.URLs[0], "turn:"), "?transport=udp")
	client, err := pionturn.NewClient(&pionturn.ClientConfig{
		TURNServerAddr: turnAddress, Conn: conn, Username: server.Username, Password: server.Credential, Realm: "sentinel",
		RTO: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Listen(); err != nil {
		t.Fatal(err)
	}
	relayed, err := client.Allocate()
	if err != nil {
		return nil, err //nolint:wrapcheck // the test looks at it as it is
	}
	address := relayed.LocalAddr()
	return address, relayed.Close()
}

func TestCredentials(t *testing.T) {
	clock := fakeclock.New(time.Now())
	relay := start(t, clock, "127.0.0.1")
	good := relay.ICEServer(time.Hour)
	if !strings.HasPrefix(good.URLs[0], "turn:127.0.0.1:") || good.Username == "" || good.Credential == "" {
		t.Fatalf("ICE server = %+v", good)
	}

	t.Run("lets in credentials it made", func(t *testing.T) {
		if err := allocate(t, good); err != nil {
			t.Error(err)
		}
	})

	t.Run("refuses a wrong password", func(t *testing.T) {
		bad := good
		bad.Credential = "guess"
		if err := allocate(t, bad); err == nil {
			t.Error("allocated with a wrong password")
		}
	})

	t.Run("refuses a username it didn't make", func(t *testing.T) {
		for _, username := range []string{"admin", "soon:abc"} {
			bad := good
			bad.Username = username
			if err := allocate(t, bad); err == nil {
				t.Errorf("allocated as %q", username)
			}
		}
	})

	t.Run("refuses credentials once they expire", func(t *testing.T) {
		short := relay.ICEServer(time.Minute)
		clock.Advance(2 * time.Minute)
		if err := allocate(t, short); err == nil {
			t.Error("allocated with expired credentials")
		}
	})
}

func TestPermitted(t *testing.T) {
	public := start(t, fakeclock.New(time.Now()), "203.0.113.7")
	loopback := start(t, fakeclock.New(time.Now()), "127.0.0.1")
	tests := []struct {
		name     string
		relay    *turn.Relay
		peer     string
		expected bool
	}{
		{"relays to a machine on the network", public, "192.168.1.20", true},
		{"relays to a public address", public, "198.51.100.4", true},
		{"won't reach the server's own loopback", public, "127.0.0.1", false},
		{"relays on loopback when it is on loopback itself", loopback, "127.0.0.1", true},
		{"won't relay to link-local addresses", public, "169.254.169.254", false},
		{"won't relay to multicast", public, "224.0.0.251", false},
		{"won't relay to an unspecified address", public, "0.0.0.0", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.relay.Permitted(net.ParseIP(test.peer)); got != test.expected {
				t.Errorf("Permitted(%s) = %v", test.peer, got)
			}
		})
	}
	if public.Permitted(net.IP{1, 2, 3}) {
		t.Error("permitted a malformed address")
	}
}

// TestRelayedWebRTC connects two WebRTC peers that may only use the relay,
// as a browser and an agent that can't reach each other do.
func TestRelayedWebRTC(t *testing.T) {
	relay := start(t, fakeclock.New(time.Now()), "127.0.0.1")
	peer := func() *webrtc.PeerConnection {
		t.Helper()
		server := relay.ICEServer(time.Hour)
		connection, err := webrtc.NewPeerConnection(webrtc.Configuration{
			ICEServers:         []webrtc.ICEServer{{URLs: server.URLs, Username: server.Username, Credential: server.Credential}},
			ICETransportPolicy: webrtc.ICETransportPolicyRelay,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = connection.Close() })
		return connection
	}
	browser, agent := peer(), peer()
	received := make(chan string, 1)
	agent.OnDataChannel(func(channel *webrtc.DataChannel) {
		channel.OnMessage(func(message webrtc.DataChannelMessage) { received <- string(message.Data) })
	})
	channel, err := browser.CreateDataChannel("frames", nil)
	if err != nil {
		t.Fatal(err)
	}
	channel.OnOpen(func() { _ = channel.SendText("through the relay") })
	exchange(t, browser, agent)
	select {
	case got := <-received:
		if got != "through the relay" {
			t.Errorf("got %q", got)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("no message came through the relay")
	}
}

// exchange runs the offer and answer, with every candidate gathered first.
func exchange(t *testing.T, offerer, answerer *webrtc.PeerConnection) {
	t.Helper()
	offer, err := offerer.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(offerer)
	if err := offerer.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	if err := answerer.SetRemoteDescription(*offerer.LocalDescription()); err != nil {
		t.Fatal(err)
	}
	answer, err := answerer.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered = webrtc.GatheringCompletePromise(answerer)
	if err := answerer.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	if err := offerer.SetRemoteDescription(*answerer.LocalDescription()); err != nil {
		t.Fatal(err)
	}
}

func TestRelayPorts(t *testing.T) {
	relay, err := turn.Start(t.Context(), turn.Options{
		Listen: "127.0.0.1:0", RelayIP: netip.MustParseAddr("127.0.0.1"), FirstPort: 50210, LastPort: 50219,
		Clock: fakeclock.New(time.Now()), Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = relay.Close() })
	address, err := allocateAddress(t, relay.ICEServer(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	udp, ok := address.(*net.UDPAddr)
	if !ok || udp.Port < 50210 || udp.Port > 50219 || !udp.IP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Errorf("relayed at %v, want 127.0.0.1 in 50210-50219", address)
	}
}

func TestStart(t *testing.T) {
	if _, err := turn.Start(t.Context(), turn.Options{Listen: "not an address", Clock: fakeclock.New(time.Now())}); err == nil {
		t.Error("started on a bad address")
	}
}

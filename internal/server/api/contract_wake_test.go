// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"net/http"
	"testing"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/server/hub"
)

func lan(address string, mac string) protocol.SystemInfo {
	return protocol.SystemInfo{Hostname: "host-" + address, OS: "linux", Arch: "amd64",
		Interfaces: []protocol.NetworkInterface{{Name: "eth0", MAC: mac, Addresses: []string{address}}}}
}

func TestWakeOnLANThroughANeighbour(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	admin := server.setUpAdmin()
	enroll := func(info protocol.SystemInfo) (string, *websocket.Conn) {
		token := createToken(t, admin, api.CreateEnrollmentTokenRequest{ExpiresInHours: 1, MaxUses: 1})
		return server.connectAgentWithInfo(token.Secret, token.ServerFingerprint, info)
	}
	nasID, nas := enroll(lan("192.168.1.20/24", "aa:bb:cc:dd:ee:01"))
	_, pi := enroll(lan("192.168.1.50/24", "aa:bb:cc:dd:ee:02"))

	t.Run("won't wake a machine that is online", func(t *testing.T) {
		expect(t, admin.browser.do(http.MethodPost, "/api/agents/"+nasID+"/wake", nil), http.StatusConflict, "cant_wake")
	})

	events, stop := server.app.Hub.Subscribe()
	defer stop()
	_ = nas.Close(websocket.StatusNormalClosure, "powered off")
	waitForAgentEvent(t, events, nasID, hub.EventOffline)

	t.Run("shows Wake on the offline machine and its interfaces", func(t *testing.T) {
		var details api.AgentDetails
		admin.browser.do(http.MethodGet, "/api/agents/"+nasID, nil).decode(t, &details)
		if !details.CanWake || len(details.Interfaces) != 1 || details.Interfaces[0].MAC != "aa:bb:cc:dd:ee:01" {
			t.Errorf("details = %+v", details)
		}
	})

	t.Run("asks the neighbour to send the packet", func(t *testing.T) {
		answered := make(chan protocol.WakePayload, 1)
		go func() {
			request, err := protocol.ReadMessage(t.Context(), pi)
			if err != nil || request.Type != protocol.TypeRequest {
				t.Errorf("pi read %+v, %v", request, err)
				return
			}
			var payload protocol.WakePayload
			if err := protocol.DecodePayload(request, &payload); err != nil || request.Action != protocol.ActionPowerWake {
				t.Errorf("pi got %s %+v (%v)", request.Action, payload, err)
			}
			answered <- payload
			_ = protocol.WriteMessage(t.Context(), pi, protocol.Message{Type: protocol.TypeResponse, ID: request.ID}, nil)
		}()
		got := admin.browser.do(http.MethodPost, "/api/agents/"+nasID+"/wake", nil)
		expect(t, got, http.StatusAccepted, "")
		var response api.WakeResponse
		got.decode(t, &response)
		if payload := <-answered; payload.MAC != "aa:bb:cc:dd:ee:01" || payload.Network != "192.168.1.20/24" || response.Via != "host-192.168.1.50/24" {
			t.Errorf("payload %+v, via %q", payload, response.Via)
		}
	})

	t.Run("a viewer can't wake machines", func(t *testing.T) {
		viewer := server.addUser(admin, "vic", "viewer")
		expect(t, viewer.do(http.MethodPost, "/api/agents/"+nasID+"/wake", nil), http.StatusForbidden, "forbidden")
	})
}

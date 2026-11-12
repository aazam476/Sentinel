// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"net/http"
	"net/netip"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
)

// errorCodeCantWake: no online machine shares a network with the one to
// wake, or Sentinel doesn't know its address yet.
const errorCodeCantWake = "cant_wake"

// WakeResponse says which machine sent the wake-up.
type WakeResponse struct {
	Via string `json:"via"`
}

// wakePlan is which online machine can wake the target, and what it sends.
type wakePlan struct {
	helper  fleet.Agent
	payload protocol.WakePayload
}

// planWake finds an online machine, other than the target and allowed to
// use power actions, on one of the target's IPv4 networks (§9.6). The
// target's MAC address comes from the interface on that network.
func planWake(target fleet.Agent, candidates []fleet.Agent, online func(int64) bool) (wakePlan, bool) {
	for _, iface := range target.SystemInfo.Interfaces {
		if iface.MAC == "" {
			continue
		}
		for _, address := range iface.Addresses {
			network, err := netip.ParsePrefix(address)
			if err != nil || !network.Addr().Is4() {
				continue
			}
			if helper, ok := onNetwork(network, target, candidates, online); ok {
				return wakePlan{helper: helper, payload: protocol.WakePayload{MAC: iface.MAC, Network: address}}, true
			}
		}
	}
	return wakePlan{}, false
}

// onNetwork is the first online candidate with an address on network.
func onNetwork(network netip.Prefix, target fleet.Agent, candidates []fleet.Agent, online func(int64) bool) (fleet.Agent, bool) {
	for _, candidate := range candidates {
		if candidate.ID == target.ID || !online(candidate.ID) || !allowsCapability(candidate, "power") {
			continue
		}
		for _, iface := range candidate.SystemInfo.Interfaces {
			for _, address := range iface.Addresses {
				if theirs, err := netip.ParsePrefix(address); err == nil && theirs.Masked() == network.Masked() {
					return candidate, true
				}
			}
		}
	}
	return fleet.Agent{}, false
}

// canWake says whether an offline machine could be woken now.
func (h *handlers) canWake(r *http.Request, session auth.Session, agent fleet.Agent) bool {
	if h.hub.IsOnline(agent.ID) {
		return false
	}
	candidates, err := h.fleet.ListAgents(r.Context(), session)
	if err != nil {
		return false
	}
	_, ok := planWake(agent, candidates, h.hub.IsOnline)
	return ok
}

// handleWake is POST /api/agents/{id}/wake: an online machine on the same
// network sends a Wake-on-LAN packet for this one (§9.6).
func (h *handlers) handleWake(w http.ResponseWriter, r *http.Request, session auth.Session) {
	target, err := h.fleet.GetAgent(r.Context(), session, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	if h.hub.IsOnline(target.ID) {
		writeError(w, http.StatusConflict, errorCodeCantWake, "The machine is already online.")
		return
	}
	candidates, err := h.fleet.ListAgents(r.Context(), session)
	if err != nil {
		h.writeError(w, err)
		return
	}
	plan, ok := planWake(target, candidates, h.hub.IsOnline)
	if !ok {
		writeError(w, http.StatusConflict, errorCodeCantWake, "No online machine on its network can send a wake-up.")
		return
	}
	details := map[string]any{"via": plan.helper.PublicID, "mac": plan.payload.MAC}
	if err := h.audited(r, session, target, "agent.wake", details, func(ctx context.Context) error {
		return h.ask(ctx, plan.helper, protocol.ActionPowerWake, plan.payload, nil)
	}); err != nil {
		h.writeAgentError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, WakeResponse{Via: plan.helper.Name()})
}

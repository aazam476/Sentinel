// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"net/http"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
)

// Package updates can be slow: listing refreshes every catalog, and an
// install downloads and installs them.
const (
	listPackagesTimeout  = 10 * time.Minute
	applyPackagesTimeout = time.Hour
	// maxPackageIDs bounds one install request.
	maxPackageIDs = 1000
)

// PackageUpdate is one update a machine's package manager offers.
type PackageUpdate struct {
	// ID is what to send back to install it.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Current and Available are versions, "" when the manager doesn't say.
	Current   string `json:"current"`
	Available string `json:"available"`
	Security  bool   `json:"security"`
}

// PackageSource is one package manager's updates.
type PackageSource struct {
	Manager string          `json:"manager" tstype:"'apt' | 'dnf' | 'zypper' | 'pacman' | 'apk' | 'softwareupdate' | 'homebrew' | 'windows-update' | 'winget'"`
	Updates []PackageUpdate `json:"updates"`
	// Selectable is false when the manager only installs everything at once.
	Selectable bool `json:"selectable"`
	// Error says why the manager couldn't be asked, or "".
	Error string `json:"error"`
}

// PackageUpdatesResponse answers GET /api/agents/{id}/packages.
type PackageUpdatesResponse struct {
	Sources []PackageSource `json:"sources"`
	// RestartRequired is true when installed updates wait for a restart.
	RestartRequired bool `json:"restartRequired"`
}

// ApplyPackagesRequest is the body of POST /api/agents/{id}/packages.
type ApplyPackagesRequest struct {
	Manager string `json:"manager"`
	// IDs are the updates to install; empty installs every one.
	IDs []string `json:"ids"`
}

// ApplyPackagesResponse answers POST /api/agents/{id}/packages.
type ApplyPackagesResponse struct {
	// Output is the end of what the package manager printed.
	Output          string `json:"output"`
	RestartRequired bool   `json:"restartRequired"`
}

// handleListPackages is GET /api/agents/{id}/packages.
func (h *handlers) handleListPackages(w http.ResponseWriter, r *http.Request, session auth.Session) {
	agent, ok := h.onlineMachine(w, r, session)
	if !ok || !packagesAvailable(w, agent) {
		return
	}
	var list protocol.PackageUpdatesPayload
	if err := h.hub.RequestWithin(r.Context(), agent.ID, listPackagesTimeout, protocol.ActionPackageListUpdates, nil, &list); err != nil {
		h.writeAgentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, packageUpdatesResponse(list))
}

func packageUpdatesResponse(list protocol.PackageUpdatesPayload) PackageUpdatesResponse {
	response := PackageUpdatesResponse{Sources: make([]PackageSource, 0, len(list.Sources)), RestartRequired: list.RestartRequired}
	for _, source := range list.Sources {
		updates := make([]PackageUpdate, 0, len(source.Updates))
		for _, update := range source.Updates {
			updates = append(updates, PackageUpdate(update))
		}
		response.Sources = append(response.Sources, PackageSource{
			Manager: source.Manager, Updates: updates, Selectable: source.Selectable, Error: source.Error,
		})
	}
	return response
}

// handleApplyPackages is POST /api/agents/{id}/packages. It installs
// software as root, so it needs a recent second factor, as the terminal
// does (SEC-07), and is audited (SEC-40).
func (h *handlers) handleApplyPackages(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request ApplyPackagesRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.Manager == "" {
		writeInvalidInput(w, "manager", "Choose a package manager.")
		return
	}
	if len(request.IDs) > maxPackageIDs {
		writeInvalidInput(w, "ids", "Choose at most 1,000 updates at a time.")
		return
	}
	if err := h.auth.RequireRecentSecondFactor(session); err != nil {
		h.writeError(w, err)
		return
	}
	agent, ok := h.onlineMachine(w, r, session)
	if !ok || !packagesAvailable(w, agent) {
		return
	}
	var result protocol.PackageApplyResult
	details := map[string]any{"manager": request.Manager, "updates": len(request.IDs), "all": len(request.IDs) == 0}
	err := h.audited(r, session, agent, protocol.ActionPackageApplyUpdates, details, func(ctx context.Context) error {
		payload := protocol.PackageApplyPayload{Manager: request.Manager, IDs: request.IDs}
		return h.hub.RequestWithin(ctx, agent.ID, applyPackagesTimeout, protocol.ActionPackageApplyUpdates, payload, &result)
	})
	if err != nil {
		h.writeAgentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ApplyPackagesResponse(result))
}

// packagesAvailable writes why a machine can't do package updates: its
// local policy switches them off (SEC-22), or it has no package manager
// Sentinel supports.
func packagesAvailable(w http.ResponseWriter, agent fleet.Agent) bool {
	for _, capability := range agent.SystemInfo.Capabilities {
		if capability.Name != "packages" || capability.Enabled {
			continue
		}
		if capability.Reason == protocol.ReasonDisabledByPolicy {
			writeError(w, http.StatusForbidden, errorCodePolicyDenied, "Package updates are disabled by this machine’s local policy.")
		} else {
			writeError(w, http.StatusNotImplemented, errorCodeUnsupported, capability.Reason+".")
		}
		return false
	}
	return true
}
